package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Открывать сокет могут только те источники, которым доверяет сам API. Без
	// этой проверки любая веб-страница могла бы открыть аутентифицированный сокет с
	// cookie посетителя и читать или писать его чат (межсайтовый перехват
	// WebSocket) — политика CORS на рукопожатия WebSocket не распространяется.
	CheckOrigin: IsAllowedOrigin,
}

// maxMessageRunes ограничивает одно сообщение чата, чтобы клиент не мог
// заталкивать в базу неограниченный текст.
const maxMessageRunes = 4000

// maxFrameBytes — предел одного входящего кадра сокета. Кадр несёт только тип
// и текст не длиннее maxMessageRunes; вложения по сокету не ходят, они
// загружаются по REST. Худший случай JSON — каждая руна экранирована
// суррогатной парой `\uXXXX\uXXXX`, 12 байт; остаток — на конверт кадра.
// Без предела клиент мог бы заставить сервер буферизовать кадр любого размера.
const maxFrameBytes = maxMessageRunes*12 + 1024

// Дедлайны сокета по стандартной схеме gorilla: сервер сам шлёт ping чуть чаще,
// чем ждёт pong, поэтому живой клиент никогда не упирается в дедлайн чтения, а
// полуоткрытое соединение (ушедший в спячку телефон, оборванная сеть без FIN)
// рвётся через pongWait вместо того, чтобы вечно держать горутину, комнату и
// датчик metrics.ChatConnected.
const (
	defaultWSWriteWait = 10 * time.Second
	defaultWSPongWait  = 60 * time.Second
)

// wsTimeouts — дедлайны одного соединения. Это поля сервиса, а не константы,
// чтобы тест мог уронить их до миллисекунд.
type wsTimeouts struct {
	// writeWait — сколько ждать записи одного кадра клиенту.
	writeWait time.Duration
	// pongWait — сколько ждать от клиента хоть какого-то кадра (pong в том числе).
	pongWait time.Duration
}

// pingPeriod — интервал ping-ов; строго меньше pongWait, чтобы pong успевал.
func (t wsTimeouts) pingPeriod() time.Duration { return t.pongWait * 9 / 10 }

// orDefault подставляет рабочие значения там, где клиента собрали без них.
func (t wsTimeouts) orDefault() wsTimeouts {
	if t.writeWait <= 0 {
		t.writeWait = defaultWSWriteWait
	}
	if t.pongWait <= 0 {
		t.pongWait = defaultWSPongWait
	}
	return t
}

// ChatClient представляет активную клиентскую сессию WebSocket.
type ChatClient struct {
	Conn   *websocket.Conn
	UserID uuid.UUID
	Send   chan []byte

	timeouts wsTimeouts
	// writeMu делает ChatClient единственным писателем в сокет: gorilla
	// допускает одного пишущего одновременно, а писать надо и из насоса записи
	// (сообщения, ping), и из читателя (ошибка «чат закрыт» только этому клиенту).
	writeMu sync.Mutex
}

// write — единственная точка записи в сокет; ставит дедлайн на каждый кадр,
// чтобы клиент, переставший читать, не подвесил писателя навсегда.
func (c *ChatClient) write(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.timeouts.orDefault().writeWait))
	return c.Conn.WriteMessage(messageType, data)
}

// writeJSON шлёт кадр только этому клиенту, минуя комнату.
func (c *ChatClient) writeJSON(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.write(websocket.TextMessage, data)
}

// ChatRoom хранит активные клиентские соединения чата одного заказа.
type ChatRoom struct {
	ChatID     uuid.UUID
	OrderID    uuid.UUID
	Clients    map[*ChatClient]bool
	Register   chan *ChatClient
	Unregister chan *ChatClient
	Broadcast  chan []byte

	// refs считает соединения, держащие эту комнату, и охраняется мьютексом
	// сервиса — тем же, который эту комнату и выдаёт.
	//
	// Раньше комната сама удаляла себя из сервиса, когда отписывался её последний
	// клиент. Соединение, только что взявшее указатель на комнату и ещё не
	// добравшееся до блокирующей отправки в Register, оставалось после этого
	// отправляющим в горутину, которая уже вернулась: обработчик блокировался
	// навсегда, удерживая сокет и горутину, которые никто уже не освободит. Подсчёт
	// держателей под тем же замком, что выдаёт комнату, закрывает это окно, потому
	// что комнату нельзя списать, пока кто-то ещё идёт к тому, чтобы в ней
	// зарегистрироваться.
	refs int
	// done закрывается, когда уходит последний держатель, — именно это
	// останавливает горутину комнаты.
	done chan struct{}
}

// deliver передаёт кадр горутине комнаты, которая разошлёт его клиентам.
//
// Это единственное поведение рассылки. Раньше их было два: неблокирующая
// отправка с `default` и отправка с секундным таймаутом. Первая теряла кадр
// всякий раз, когда горутина комнаты была занята чем-то другим (регистрацией
// соседа, предыдущим кадром) — в обычной работе, а не в отказе. Вторая ждала
// секунду на пути REST-запроса ради комнаты, которая уже умерла. Ожидание
// «комната взяла или комната закрылась» точнее обоих: горутина комнаты между
// кадрами ничего не ждёт, поэтому взятие происходит сразу, а done закрывается
// в тот момент, когда уходит последний держатель, поэтому в мёртвую комнату
// никто не пишет.
func (room *ChatRoom) deliver(payload []byte) {
	select {
	case room.Broadcast <- payload:
	case <-room.done:
	}
}

// deliverJSON — deliver для нагрузки, которую ещё надо сериализовать.
func (room *ChatRoom) deliverJSON(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	room.deliver(data)
}

// statusUpdate — кадр «статус этих сообщений изменился», который обе стороны
// получают после подтверждений доставки и прочтения.
func statusUpdate(messageIDs []uuid.UUID, status string) map[string]any {
	return map[string]any{
		"type":        "status_update",
		"message_ids": messageIDs,
		"status":      status,
	}
}

// ChatService управляет группами WebSocket-сессий, обработкой сообщений и получением истории.
type ChatService struct {
	chatRepo  repository.ChatRepository
	orderRepo repository.OrderRepository
	rooms     map[uuid.UUID]*ChatRoom // с ключом OrderID
	mu        sync.RWMutex
	// wsTimeouts выдаётся каждому новому соединению; см. wsTimeouts.
	wsTimeouts wsTimeouts
}

// NewChatService создаёт новый ChatService.
func NewChatService(chatRepo repository.ChatRepository, orderRepo repository.OrderRepository) *ChatService {
	return &ChatService{
		chatRepo:   chatRepo,
		orderRepo:  orderRepo,
		rooms:      make(map[uuid.UUID]*ChatRoom),
		wsTimeouts: wsTimeouts{}.orDefault(),
	}
}

func (s *ChatService) getOrCreateRoom(orderID, chatID uuid.UUID) *ChatRoom {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, exists := s.rooms[orderID]
	if !exists {
		room = &ChatRoom{
			ChatID:     chatID,
			OrderID:    orderID,
			Clients:    make(map[*ChatClient]bool),
			Register:   make(chan *ChatClient),
			Unregister: make(chan *ChatClient),
			Broadcast:  make(chan []byte),
			done:       make(chan struct{}),
		}
		s.rooms[orderID] = room
		go runRoom(room)
	}
	// Вызывающий теперь держит комнату и обязан вызвать releaseRoom, когда его
	// соединение закончится. Взято под тем же замком, что её создал или нашёл,
	// поэтому комнату нельзя списать между этими двумя действиями.
	room.refs++
	return room
}

// releaseRoom отпускает один захват комнаты, списывая её, когда уходит последний.
func (s *ChatService) releaseRoom(room *ChatRoom) {
	s.mu.Lock()
	defer s.mu.Unlock()

	room.refs--
	if room.refs > 0 {
		return
	}
	// Убираем запись, только если это всё ещё та же комната: комната, списанная
	// здесь и заново созданная более поздним соединением, иначе была бы выдернута
	// из-под своих новых держателей.
	if current, ok := s.rooms[room.OrderID]; ok && current == room {
		delete(s.rooms, room.OrderID)
	}
	close(room.done)
}

// runRoom — горутина комнаты. Она живёт, пока комнату держит хоть одно
// соединение, и ни от какого контекста не зависит: комната переживает любой
// отдельный запрос.
func runRoom(room *ChatRoom) {
	for {
		select {
		case client := <-room.Register:
			room.Clients[client] = true
		case client := <-room.Unregister:
			if _, ok := room.Clients[client]; ok {
				delete(room.Clients, client)
				close(client.Send)
			}
		case <-room.done:
			// Последний держатель ушёл, и комната уже вне карты сервиса. Всё, что
			// здесь ещё зарегистрировано, — клиент, чей читатель исчез; закрываем его
			// писателя, чтобы горутина завершилась.
			for client := range room.Clients {
				delete(room.Clients, client)
				close(client.Send)
			}
			return
		case message := <-room.Broadcast:
			for client := range room.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(room.Clients, client)
				}
			}
		}
	}
}

// WritePump проталкивает сообщения из канала отправки клиенту WebSocket и
// пингует его, чтобы дедлайн чтения на той стороне и здесь не срабатывал у
// живого соединения. Выходит, когда комната закрыла Send или сокет умер; в
// обоих случаях закрывает сокет, чем выбивает ReadPump из чтения — а тот уже
// отписывает клиента и отпускает комнату.
func (c *ChatClient) WritePump() {
	ticker := time.NewTicker(c.timeouts.orDefault().pingPeriod())
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				_ = c.write(websocket.CloseMessage, nil)
				return
			}
			if err := c.write(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// inboundFrame — всё, что клиент может прислать по сокету. У обычного
// сообщения тип пуст: {"text": "..."}; подтверждения и ping несут только тип.
type inboundFrame struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReadPump слушает сообщения от клиента WebSocket и рассылает их.
//
// ctx — контекст соединения, а не запроса: net/http отменяет контекст запроса
// сразу после возврата обработчика, ещё до проверки, что соединение перехвачено,
// а этот цикл живёт много дольше. Здесь у него собственная отмена, чтобы всё,
// что от него породили, отпустилось вместе с соединением.
func (s *ChatService) ReadPump(ctx context.Context, client *ChatClient, room *ChatRoom) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		// Сначала отписка, потом отпускание: пока это соединение всё ещё держит
		// комнату, её горутина гарантированно работает и это примет.
		room.Unregister <- client
		s.releaseRoom(room)
		client.Conn.Close()
	}()

	conn := client.Conn
	pongWait := client.timeouts.orDefault().pongWait
	conn.SetReadLimit(maxFrameBytes)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	// Pong-и приходят в этой же горутине, изнутри ReadMessage, поэтому сдвигать
	// дедлайн отсюда безопасно.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Сначала разбираем кадр, потом решаем, нужна ли база: подтверждения и
		// ping не пишут сообщений, и статус заказа им ни к чему. Раньше каждый
		// кадр стоил двух запросов ещё до взгляда на его тип.
		var frame inboundFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		switch frame.Type {
		case "delivery_ack":
			s.acknowledge(ctx, client, room, "delivered", s.chatRepo.MarkMessagesAsDelivered)
		case "read_ack":
			s.acknowledge(ctx, client, room, "read", s.chatRepo.MarkMessagesAsRead)
		case "ping":
			// Диагностический round-trip: доказательство, что кадр, отправленный
			// клиентом, реально дошёл до сервера. Pong рассылается через писателя
			// комнаты. Клиенты игнорируют pong в интерфейсе и только логируют.
			room.deliverJSON(map[string]any{
				"type": "pong",
				"ts":   time.Now().UnixMilli(),
			})
		default:
			if !s.receiveMessage(ctx, client, room, frame.Text) {
				return
			}
		}
	}
}

// acknowledge помечает сообщения собеседника доставленными или прочитанными и
// сообщает об этом комнате, если что-то действительно изменилось.
func (s *ChatService) acknowledge(ctx context.Context, client *ChatClient, room *ChatRoom, status string,
	mark func(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error)) {
	updatedIDs, err := mark(ctx, room.ChatID, client.UserID)
	if err == nil && len(updatedIDs) > 0 {
		room.deliverJSON(statusUpdate(updatedIDs, status))
	}
}

// receiveMessage — путь обычного сообщения: единственный вид кадра, которому
// нужны статус заказа и активность чата. Возвращает false, когда заказ уже
// закрыт и соединение пора рвать.
func (s *ChatService) receiveMessage(ctx context.Context, client *ChatClient, room *ChatRoom, text string) bool {
	if text == "" {
		return true
	}

	// Читаем текущий статус заказа, чтобы проверить состояние чата.
	order, err := s.orderRepo.FindByID(ctx, room.OrderID)
	if err != nil {
		log.Printf("[ChatService] Failed to check order status: %v", err)
		return true
	}

	// Если заказ уже COMPLETED или CANCELED, гасим чат.
	if orderChatClosed(order.Status) {
		_ = s.chatRepo.DeactivateChat(ctx, room.ChatID)
		room.deliverJSON(map[string]string{
			"type":   "system",
			"action": "lock",
		})
		return false
	}

	// Проверяем, активна ли чат-комната в базе.
	chat, err := s.chatRepo.GetChatByOrderID(ctx, room.OrderID)
	if err != nil || chat == nil || !chat.IsActive {
		_ = client.writeJSON(map[string]string{
			"type":    "error",
			"message": "Chat is locked (read-only).",
		})
		return true
	}

	if len([]rune(text)) > maxMessageRunes {
		return true
	}

	savedMsg, err := s.chatRepo.SaveMessage(ctx, room.ChatID, client.UserID, text)
	if err != nil {
		log.Printf("[ChatService] Failed to save message: %v", err)
		return true
	}
	room.deliverJSON(savedMsg)
	return true
}

// MarkMessagesAsRead помечает сообщения заказа прочитанными.
func (s *ChatService) MarkMessagesAsRead(ctx context.Context, orderID, userID uuid.UUID) ([]uuid.UUID, error) {
	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil || chat == nil {
		return nil, errors.New("chat room not found")
	}
	updatedIDs, err := s.chatRepo.MarkMessagesAsRead(ctx, chat.ID, userID)
	if err == nil && len(updatedIDs) > 0 {
		s.broadcast(orderID, statusUpdate(updatedIDs, "read"))
	}
	return updatedIDs, err
}

// GetUnreadOrderIDs возвращает ID заказов с непрочитанными сообщениями для пользователя.
func (s *ChatService) GetUnreadOrderIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.chatRepo.GetUnreadOrderIDs(ctx, userID)
}

// GetMessages отдаёт окно истории чата заказа, проверяя, что вызывающий
// участвует в переписке. Окно выбирает вызывающий (см. repository.MessageQuery);
// пустой запрос даёт самую свежую страницу.
func (s *ChatService) GetMessages(ctx context.Context, orderID, userID uuid.UUID, q repository.MessageQuery) ([]*repository.Message, error) {
	if _, err := s.participantOrder(ctx, orderID, userID); err != nil {
		return nil, err
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(ctx, orderID)
		if err != nil {
			return nil, err
		}
	}

	return s.chatRepo.GetMessages(ctx, chat.ID, q)
}

// SendMessage сохраняет сообщение чата через REST и рассылает его активным
// WS-клиентам. Это классический запасной путь по HTTP, используемый, когда путь
// отправки по WebSocket недоступен (например, в WebView, где мост глотает ws.send()).
func (s *ChatService) SendMessage(ctx context.Context, orderID, userID uuid.UUID, text string) (*repository.Message, error) {
	chat, err := s.writableChat(ctx, orderID, userID)
	if err != nil {
		return nil, err
	}

	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	savedMsg, err := s.chatRepo.SaveMessage(ctx, chat.ID, userID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("order")

	s.broadcast(orderID, savedMsg)
	return savedMsg, nil
}

// SendMessageWithAttachment сохраняет сообщение чата с вложением через REST и рассылает его.
func (s *ChatService) SendMessageWithAttachment(ctx context.Context, orderID, userID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	chat, err := s.writableChat(ctx, orderID, userID)
	if err != nil {
		return nil, err
	}

	savedMsg, err := s.chatRepo.SaveMessageWithAttachment(ctx, chat.ID, userID, text, fileURL, fileName, fileType, fileSize)
	if err != nil {
		return nil, err
	}

	s.broadcast(orderID, savedMsg)
	return savedMsg, nil
}

// orderChatClosed сообщает, что заказ завершён и его чат больше не принимает
// сообщений.
func orderChatClosed(status repository.OrderStatus) bool {
	return status == "COMPLETED" || status == "CANCELED"
}

// participantOrder возвращает заказ, если пользователь — его заказчик или
// исполнитель, и ErrForbidden — если нет.
func (s *ChatService) participantOrder(ctx context.Context, orderID, userID uuid.UUID) (*repository.Order, error) {
	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}
	return order, nil
}

// writableChat — общий пролог записи в чат заказа: вызывающий должен быть
// участником, заказ — ещё открытым, а чат — активным. Возвращает чат, в
// который можно писать.
func (s *ChatService) writableChat(ctx context.Context, orderID, userID uuid.UUID) (*repository.Chat, error) {
	order, err := s.participantOrder(ctx, orderID, userID)
	if err != nil {
		return nil, err
	}
	if orderChatClosed(order.Status) {
		return nil, fmt.Errorf("%w: order completed or canceled", ErrChatLocked)
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, ErrChatLocked
	}
	return chat, nil
}

// HandleWS обрабатывает апгрейды, авторизацию и циклы.
func (s *ChatService) HandleWS(ctx context.Context, w http.ResponseWriter, r *http.Request, orderID, userID uuid.UUID) {
	if _, err := s.participantOrder(ctx, orderID, userID); err != nil {
		if errors.Is(err, ErrForbidden) {
			http.Error(w, "forbidden: you are not a participant in this order", http.StatusForbidden)
			return
		}
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(ctx, orderID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ChatService] Upgrade error: %v", err)
		return
	}

	room := s.getOrCreateRoom(orderID, chat.ID)
	client := &ChatClient{
		Conn:     conn,
		UserID:   userID,
		Send:     make(chan []byte, 256),
		timeouts: s.wsTimeouts,
	}
	room.Register <- client

	go client.WritePump()

	// Автоматически помечаем сообщения прочитанными, когда пользователь подключается к комнате, и уведомляем собеседника
	if updatedIDs, err := s.chatRepo.MarkMessagesAsRead(ctx, chat.ID, userID); err == nil && len(updatedIDs) > 0 {
		room.deliverJSON(statusUpdate(updatedIDs, "read"))
	}

	// Контекст соединения. Обработчик возвращается сразу после запуска насосов,
	// а net/http отменяет контекст запроса сразу после возврата обработчика —
	// раньше, чем проверяет, что соединение перехвачено. С контекстом запроса
	// каждый запрос к базе из читателя падал бы с context canceled. WithoutCancel
	// сохраняет значения промежуточных слоёв (пользователя и прочее) и снимает
	// только отмену; свою отмену читатель заводит сам.
	connCtx := context.WithoutCancel(ctx)

	// Датчик парно ставится здесь, а не внутри ReadPump, чтобы соединение никогда
	// не было посчитано без своего парного уменьшения.
	metrics.ChatConnected("order")
	go func() {
		defer metrics.ChatDisconnected("order")
		s.ReadPump(connCtx, client, room)
	}()
}

// EditMessage меняет текст сообщения, если оно принадлежит отправителю, и рассылает событие message_edited.
func (s *ChatService) EditMessage(ctx context.Context, messageID, senderID, orderID uuid.UUID, newText string) (*repository.Message, error) {
	if len([]rune(newText)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	msg, err := s.chatRepo.UpdateMessage(ctx, messageID, senderID, newText)
	if err != nil {
		return nil, err
	}
	// Комната, которую надо уведомить, выводится из самого сообщения: взятие id
	// заказа из запроса позволило бы отправителю протолкнуть событие правки в чат,
	// участником которого он не является.
	if err := s.assertMessageInOrder(ctx, msg, orderID); err != nil {
		return nil, err
	}

	// Рассылаем событие правки в комнату, если она активна
	s.broadcast(orderID, map[string]any{
		"type":       "message_edited",
		"message_id": messageID,
		"order_id":   orderID,
		"text":       msg.Text,
		"updated_at": msg.UpdatedAt,
		"message":    msg,
	})

	return msg, nil
}

// DeleteMessage удаляет сообщение, если оно принадлежит отправителю, и рассылает событие message_deleted.
func (s *ChatService) DeleteMessage(ctx context.Context, messageID, senderID, orderID uuid.UUID) error {
	if err := s.chatRepo.DeleteMessage(ctx, messageID, senderID); err != nil {
		return err
	}

	// Рассылаем событие удаления в комнату, если она активна
	s.broadcast(orderID, map[string]any{
		"type":       "message_deleted",
		"message_id": messageID,
		"order_id":   orderID,
	})

	return nil
}

// assertMessageInOrder проверяет, что сообщение действительно принадлежит чату
// указанного заказа.
func (s *ChatService) assertMessageInOrder(ctx context.Context, msg *repository.Message, orderID uuid.UUID) error {
	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil || chat == nil || chat.ID != msg.ChatID {
		return ErrForbidden
	}
	return nil
}

// BroadcastSystemMessage отправляет произвольную нагрузку всем активным
// соединениям заказа. Комнаты без соединений нет, и тогда сообщение просто
// некому вручить.
func (s *ChatService) BroadcastSystemMessage(ctx context.Context, orderID uuid.UUID, msg interface{}) {
	s.broadcast(orderID, msg)
}

// broadcast сериализует нагрузку и вручает её комнате заказа, если та открыта.
// Семантика ожидания описана у ChatRoom.deliver.
func (s *ChatService) broadcast(orderID uuid.UUID, payload any) {
	s.mu.RLock()
	room, exists := s.rooms[orderID]
	s.mu.RUnlock()
	if !exists {
		return
	}
	room.deliverJSON(payload)
}

// GetOrCreateSupportChat возвращает чат поддержки пользователя.
func (s *ChatService) GetOrCreateSupportChat(ctx context.Context, userID uuid.UUID) (*repository.SupportChat, error) {
	return s.chatRepo.GetOrCreateSupportChat(ctx, userID)
}

// Ошибки, которые возвращает сервис чата. Обработчики сопоставляют их с кодами
// статуса по тождеству: сопоставление по тексту ошибки — способ молча
// превратить 403 в 500 при переименовании.
var (
	// ErrChatLocked сообщает, что переписка больше не принимает сообщений.
	ErrChatLocked = errors.New("chat is locked (read-only)")
)

// authorizeSupportChat пропускает владельца чата и любого админа. Переписки
// поддержки адресуются по id чата, поэтому без этой проверки любой
// аутентифицированный пользователь мог бы читать или писать в чужой чат.
func (s *ChatService) authorizeSupportChat(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	if role == "ADMIN" {
		return nil
	}
	owner, err := s.chatRepo.SupportChatOwner(ctx, chatID)
	if err != nil {
		return ErrForbidden
	}
	if owner != userID {
		return ErrForbidden
	}
	return nil
}

// SupportChatOwner — пользователь, которому принадлежит чат поддержки.
func (s *ChatService) SupportChatOwner(ctx context.Context, chatID uuid.UUID) (uuid.UUID, error) {
	return s.chatRepo.SupportChatOwner(ctx, chatID)
}

// GetSupportMessages возвращает окно чата поддержки, которым владеет вызывающий.
func (s *ChatService) GetSupportMessages(ctx context.Context, chatID, userID uuid.UUID, role string, q repository.MessageQuery) ([]*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, userID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.GetSupportMessages(ctx, chatID, q)
}

// SaveSupportMessage сохраняет новое текстовое сообщение поддержки.
func (s *ChatService) SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, role, text string) (*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, senderID, role); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("text is required")
	}
	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}
	msg, err := s.chatRepo.SaveSupportMessage(ctx, chatID, senderID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("support")
	return msg, nil
}

// SaveSupportMessageWithAttachment сохраняет новое сообщение поддержки с вложением.
func (s *ChatService) SaveSupportMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, role, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, senderID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.SaveSupportMessageWithAttachment(ctx, chatID, senderID, text, fileURL, fileName, fileType, fileSize)
}

// CanAccessAttachment сообщает, может ли пользователь скачать сохранённый файл.
func (s *ChatService) CanAccessAttachment(ctx context.Context, userID uuid.UUID, role, fileURL string) (bool, error) {
	if role == "ADMIN" {
		return true, nil
	}
	return s.chatRepo.CanAccessAttachment(ctx, userID, fileURL)
}

// GetAdminSupportChatList возвращает недавно активные чаты поддержки для
// админского интерфейса в стиле Telegram, ограниченные размером страницы репозитория.
func (s *ChatService) GetAdminSupportChatList(ctx context.Context) ([]*repository.SupportChatListItem, error) {
	return s.chatRepo.GetAdminSupportChatList(ctx, 0)
}

// MarkSupportMessagesAsRead помечает непрочитанные сообщения чата поддержки прочитанными.
func (s *ChatService) MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID, role string) error {
	if err := s.authorizeSupportChat(ctx, chatID, readerID, role); err != nil {
		return err
	}
	return s.chatRepo.MarkSupportMessagesAsRead(ctx, chatID, readerID)
}

// BanSupportChat банит чат поддержки на указанный срок («10m», «1h», «forever»).
func (s *ChatService) BanSupportChat(ctx context.Context, chatID uuid.UUID, duration string) error {
	return s.chatRepo.BanSupportChat(ctx, chatID, duration)
}

// UnbanSupportChat снимает бан с чата поддержки.
func (s *ChatService) UnbanSupportChat(ctx context.Context, chatID uuid.UUID) error {
	return s.chatRepo.UnbanSupportChat(ctx, chatID)
}

// IsSupportChatBanned проверяет, забанен ли чат поддержки.
func (s *ChatService) IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error) {
	return s.chatRepo.IsSupportChatBanned(ctx, chatID)
}

// GetAdminSupportUnreadCount возвращает общее число непрочитанных сообщений для админа.
func (s *ChatService) GetAdminSupportUnreadCount(ctx context.Context) (int, error) {
	return s.chatRepo.GetAdminSupportUnreadCount(ctx)
}
