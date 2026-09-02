package service

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// defaultAutoMatchRadiusKM ограничивает автоматическое назначение. Он совпадает
// с радиусом, в котором карта исполнителя показывает заказы, поэтому воркер
// может раздавать только те заказы, которые исполнитель увидел бы и мог доехать.
const defaultAutoMatchRadiusKM = 10.0

// MatchingService сопоставляет заказы в поиске с активными исполнителями.
type MatchingService struct {
	orderRepo    repository.OrderRepository
	shiftRepo    repository.ShiftRepository
	userRepo     repository.UserRepository
	catalogRepo  repository.ServiceCatalogRepository
	geoRepo      repository.ExecutorGeoRepository
	settingsRepo repository.SettingsRepository
	// behaviors применяет скриптовые правила услуги, чтобы автоподбор не мог
	// назначить заказ тому, кто не смог бы принять его вручную.
	// Необязательно.
	behaviors *Behaviors
	// leaderGuard, если задан, выполняет цикл только на процессе, держащем
	// блокировку задачи подбора. См. WithLeaderGuard.
	leaderGuard func(func() error) error
}

// NewMatchingService создаёт новый MatchingService.
func NewMatchingService(orderRepo repository.OrderRepository, shiftRepo repository.ShiftRepository, userRepo repository.UserRepository, catalogRepo repository.ServiceCatalogRepository) *MatchingService {
	return &MatchingService{
		orderRepo:   orderRepo,
		shiftRepo:   shiftRepo,
		userRepo:    userRepo,
		catalogRepo: catalogRepo,
	}
}

// WithBehaviors подключает скрипты поведений к проверке кандидатов подборщиком.
func (s *MatchingService) WithBehaviors(behaviors *Behaviors) *MatchingService {
	s.behaviors = behaviors
	return s
}

// WithGeo присоединяет хранилища, нужные автоподбору, чтобы ограничивать
// назначение по расстоянию. Без них воркер не может определить, как далеко
// исполнитель от заказа, и отказывается назначать, а не гадает.
func (s *MatchingService) WithGeo(geoRepo repository.ExecutorGeoRepository, settingsRepo repository.SettingsRepository) *MatchingService {
	s.geoRepo = geoRepo
	s.settingsRepo = settingsRepo
	return s
}

// autoMatchRadiusKM читает настроенную границу, откатываясь к умолчанию.
func (s *MatchingService) autoMatchRadiusKM(ctx context.Context) float64 {
	if s.settingsRepo == nil {
		return defaultAutoMatchRadiusKM
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return defaultAutoMatchRadiusKM
	}
	if v, ok := settings["auto_match_radius_km"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return defaultAutoMatchRadiusKM
}

// SettingAutoMatchingEnabled — ключ system_settings, включающий и выключающий
// автоматическое назначение. По умолчанию ВЫКЛ: пока он выключен, заказы
// берутся только нажатием исполнителем «взять» и никогда не назначаются воркером.
const SettingAutoMatchingEnabled = "auto_matching_enabled"

// autoMatchingEnabled сообщает, можно ли воркеру назначать заказы. Выключено,
// пока админ явно не включит, поэтому по умолчанию всё только вручную.
func (s *MatchingService) autoMatchingEnabled(ctx context.Context) bool {
	if s.settingsRepo == nil {
		return false
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return false
	}
	return settings[SettingAutoMatchingEnabled] == "1"
}

// withinAutoMatchRadius сообщает, достаточно ли заказ близок к исполнителю,
// чтобы быть назначенным автоматически.
//
// Неизвестная позиция — это «нет», а никогда не безусловный пропуск. Пропуск
// исполнителя без координат — то, как заказ достаётся кому-то на другом конце
// страны, кто может только его отменить. Позиции загружаются раз за цикл, а не
// на кандидата, поэтому отсутствующая запись в карте — это ровно та самая
// неизвестная позиция.
func withinAutoMatchRadius(position repository.ExecutorPosition, known bool, order *repository.Order, radiusKM float64) bool {
	if !known {
		return false
	}
	if order.PickupLat == nil || order.PickupLon == nil {
		return false
	}
	return HaversineDistanceKM(*order.PickupLat, *order.PickupLon, position.Lat, position.Lon) <= radiusKM
}

// WithLeaderGuard заставляет воркер подбора выполняться не более одного раза
// среди всех процессов. Защита приходит от вызывающего (worker.Leader), а не
// строится здесь, потому что этот пакет не должен зависеть от пакета worker.
//
// Без неё два процесса назначали бы одни и те же ждущие заказы, а заказ можно
// назначить лишь однажды — проигравший пишет ошибку каждый цикл.
func (s *MatchingService) WithLeaderGuard(guard func(func() error) error) *MatchingService {
	s.leaderGuard = guard
	return s
}

// runGuarded выполняет один цикл подбора, под защитой, если она подключена.
func (s *MatchingService) runGuarded(ctx context.Context) error {
	job := func() error { return s.MatchOrders(ctx) }
	if s.leaderGuard == nil {
		return job()
	}
	return s.leaderGuard(job)
}

// StartMatchingWorker запускает фоновый цикл, периодически выполняющий подбор.
func (s *MatchingService) StartMatchingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("matching", func() error { return s.runGuarded(ctx) }); err != nil {
				log.Printf("[MatchingWorker] Error: %v", err)
			}
		}
	}()
	log.Printf("[MatchingWorker] Started background matching every %v", interval)
}

// matchingRound держит всё, что нужно одному циклу подбора, загруженное заранее.
//
// Воркер сравнивает каждый ждущий заказ с каждым исполнителем на смене. Когда
// каждое такое сравнение ходило в базу за исполнителем, вариантом услуги,
// позицией и числом назначенных заказов, цикл стоил примерно четырёх запросов
// на пару — а это растёт как произведение заказов и исполнителей, на таймере в
// пять секунд. Однократная загрузка каждого из этих наборов превращает
// сравнение в арифметику.
type matchingRound struct {
	users    map[uuid.UUID]*repository.User
	variants map[uuid.UUID]*repository.ServiceNode
	// positions содержит только исполнителей с сохранённой позицией; отсутствие
	// означает «неизвестно», а это никогда не допускается.
	positions map[uuid.UUID]repository.ExecutorPosition
	// activeOrders считает назначенные заказы по исполнителям. Он обновляется по
	// ходу назначения в цикле, поэтому исполнителю нельзя вручить второй заказ на
	// более поздней итерации того же цикла.
	activeOrders map[uuid.UUID]int
}

// loadRound достаёт входные данные раунда фиксированным числом запросов.
func (s *MatchingService) loadRound(ctx context.Context, orders []*repository.Order, executorIDs []uuid.UUID) (*matchingRound, error) {
	round := &matchingRound{
		users:        map[uuid.UUID]*repository.User{},
		variants:     map[uuid.UUID]*repository.ServiceNode{},
		positions:    map[uuid.UUID]repository.ExecutorPosition{},
		activeOrders: map[uuid.UUID]int{},
	}

	// Заказчики и исполнители лежат в одной таблице, поэтому это одно чтение.
	userIDs := make([]uuid.UUID, 0, len(orders)+len(executorIDs))
	variantIDs := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		userIDs = append(userIDs, o.CustomerID)
		variantIDs = append(variantIDs, o.ServiceVariantID)
	}
	userIDs = append(userIDs, executorIDs...)

	if s.userRepo != nil {
		users, err := s.userRepo.FindByIDs(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		round.users = users
	}
	if s.catalogRepo != nil {
		variants, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs)
		if err != nil {
			return nil, err
		}
		round.variants = variants
	}
	if s.geoRepo != nil {
		positions, err := s.geoRepo.GetExecutorLocations(ctx, executorIDs)
		if err != nil {
			return nil, err
		}
		round.positions = positions
	}
	counts, err := s.orderRepo.CountActiveOrdersByExecutors(ctx, executorIDs)
	if err != nil {
		return nil, err
	}
	round.activeOrders = counts

	return round, nil
}

// executorEligible переиспользует общий предикат видимости/принятия, чтобы
// автоподбор не мог выдать заказ, который исполнителю брать нельзя, — включая
// заказы только для модераторов (только модераторам) и сегментацию по
// верификации заказчика.
//
// Входные данные приходят из предзагруженного раунда; предикат — тот же, что
// используют карта, список заказов и путь принятия.
func (s *MatchingService) executorEligible(ctx context.Context, round *matchingRound, executorID uuid.UUID, order *repository.Order) bool {
	if s.userRepo == nil || s.catalogRepo == nil {
		return true
	}
	executor, ok := round.users[executorID]
	if !ok {
		return false
	}
	variant, ok := round.variants[order.ServiceVariantID]
	if !ok {
		return false
	}
	return canViewOrTakeOrder(ctx, s.behaviors, executor, round.users[order.CustomerID], variant) == nil
}

// MatchOrders выполняет цикл подбора.
func (s *MatchingService) MatchOrders(ctx context.Context) error {
	// Автоматическое назначение включается явно. Пока оно выключено (по
	// умолчанию), воркер ничего не делает, и заказы берутся только вручную.
	if !s.autoMatchingEnabled(ctx) {
		return nil
	}

	// 1. Получаем все заказы в поиске
	orders, err := s.orderRepo.GetPendingOrders(ctx)
	if err != nil {
		return err
	}
	if len(orders) == 0 {
		metrics.SetMarketplaceDepth(0, 0)
		return nil
	}

	// 2. Достаём все активные смены
	activeShifts, err := s.shiftRepo.GetActiveShifts(ctx)
	if err != nil {
		return err
	}
	metrics.SetMarketplaceDepth(len(orders), len(activeShifts))
	if len(activeShifts) == 0 {
		return nil
	}

	// Кандидаты-исполнители: по записи на каждую активную смену.
	executorIDs := make([]uuid.UUID, 0, len(activeShifts))
	for _, shift := range activeShifts {
		executorIDs = append(executorIDs, shift.ExecutorID)
	}

	// Всё, что нужно сопоставлению ниже, загруженное один раз на весь цикл.
	round, err := s.loadRound(ctx, orders, executorIDs)
	if err != nil {
		return err
	}

	// 3. Подбираем каждому заказу
	radiusKM := s.autoMatchRadiusKM(ctx)
	for _, order := range orders {
		var matchedExecutorID uuid.UUID
		for _, execID := range executorIDs {
			if execID == order.CustomerID {
				continue
			}
			// По одному назначенному заказу за раз: исполнитель, у которого уже есть
			// заказ, не кандидат. Проверяется первым, потому что это самая дешёвая
			// проверка и она учитывает заказы, назначенные раньше в этом же цикле.
			if round.activeOrders[execID] > 0 {
				continue
			}
			if !s.executorEligible(ctx, round, execID, order) {
				continue
			}
			position, known := round.positions[execID]
			if !withinAutoMatchRadius(position, known, order, radiusKM) {
				continue
			}

			matchedExecutorID = execID
			break
		}

		if matchedExecutorID != uuid.Nil {
			err = s.orderRepo.Assign(ctx, nil, order.ID, matchedExecutorID)
			if err != nil {
				metrics.MatchingAssignment("error")
				log.Printf("[MatchingWorker] Error assigning order %s to executor %s: %v", order.ID, matchedExecutorID, err)
			} else {
				// Из гонки исполнителя выводит только успешное назначение: неудачное
				// оставляет его свободным для следующего заказа.
				round.activeOrders[matchedExecutorID]++
				metrics.MatchingAssignment("assigned")
				metrics.OrderEvent("assigned")
				log.Printf("[MatchingWorker] Matched order %s with executor %s", order.ID, matchedExecutorID)
			}
		}
	}

	return nil
}
