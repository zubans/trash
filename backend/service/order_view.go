package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Карточка заказа в приложении одна на обе роли. Что в ней показать и какие
// кнопки дать, решается здесь, под того, кто смотрит, — приложение только
// рисует пришедшие counterparty и actions.

// OrderView — заказ, каким его отдаёт API: строка таблицы плюс всё, за чем не
// стоит колонки. Репозиторий отдаёт только строку; собирает карточку этот
// файл. Имена JSON-полей — контракт с установленными приложениями, см.
// TestOrderViewJSONKeys.
type OrderView struct {
	repository.Order

	// executor_name и executor_phone читают установленные APK заказчика;
	// новая карточка берёт вторую сторону из Counterparty.
	ExecutorPhone string `json:"executor_phone,omitempty"`
	ExecutorName  string `json:"executor_name,omitempty"`

	ServiceVariant *repository.ServiceNode `json:"service_variant,omitempty"`
	// ServiceCategory — родительская категория варианта. Едет вместе с заказом,
	// потому что клиент подписывает заказ как «категория / услуга», а достать её
	// сам он не может: /service-categories отдаёт только корни, и у вложенного
	// каталога родитель варианта в этот список не попадает.
	ServiceCategory *repository.ServiceNode `json:"service_category,omitempty"`

	// PhotoProof собирается для исполнителя заказа (attachPhotoProof).
	PhotoProof *OrderPhotoProof `json:"photo_proof,omitempty"`
	// SubmitFields называет данные, которые исполнитель обязан отправить на
	// проверку до завершения этого заказа, — поля личности в заказе верификации.
	// Берётся из поведения услуги и никогда не несёт сами значения.
	SubmitFields []string `json:"submit_fields,omitempty"`
	// RequirePassport — исполнитель вносит паспорт заказчика с фото до сверки
	// (require_passport в манифесте).
	RequirePassport bool `json:"require_passport,omitempty"`
	// ScriptExecuted — заказ закрывает скрипт услуги, а не отметка исполнителя
	// (manual_execute = false в манифесте). Наружу выходит как actions.execute.
	ScriptExecuted bool `json:"-"`

	// Counterparty и Actions собираются под того, кто смотрит на заказ.
	Counterparty *OrderParty   `json:"counterparty,omitempty"`
	Actions      *OrderActions `json:"actions,omitempty"`
}

// MapOrderView — заказ на карте и в списке «Заказы поблизости»: карточка плюс
// расстояние до него и можно ли его взять с текущей позиции.
type MapOrderView struct {
	OrderView
	CanAccept  bool    `json:"can_accept"`
	DistanceKM float64 `json:"distance_km"`
	// CategoryName — родительская категория варианта услуги, разрешённая, чтобы
	// карта показывала «категория · услуга», не заставляя клиента обходить дерево каталога.
	CategoryName string `json:"category_name,omitempty"`
}

// OrderPhotoProof — что исполнитель должен знать о фото-подтверждении заказа,
// чтобы снять его без сети: какой жест показать и служебные данные проверки.
type OrderPhotoProof struct {
	Required bool          `json:"required"`
	Gesture  *OrderGesture `json:"gesture,omitempty"`
	Nonce    string        `json:"nonce,omitempty"`
}

// OrderGesture — жест заказа в том виде, в каком его показывает попап.
type OrderGesture struct {
	Code         string `json:"code"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	HintImageURL string `json:"hint_image_url,omitempty"`
	FitsInSelfie bool   `json:"fits_in_selfie"`
}

// OrderParty — вторая сторона заказа глазами смотрящего: заказчику —
// исполнитель, исполнителю — заказчик.
type OrderParty struct {
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	// Hidden — сторона известна, но этому смотрящему её не показывают.
	Hidden bool `json:"hidden,omitempty"`
}

// OrderActions — что смотрящий может сделать с заказом сейчас. Решает сервер
// по роли и статусу; приложение только рисует кнопки.
type OrderActions struct {
	Cancel bool `json:"cancel"`
	Reject bool `json:"reject"`
	Review bool `json:"review"`
	// Dispute — заказчик может заявить, что заказ не выполнен.
	Dispute bool `json:"dispute"`
	// Concede — исполнитель может признать, что оспоренный заказ не выполнен.
	Concede bool `json:"concede"`
	// Execute — исполнитель может отметить заказ исполненным («Исполнил»).
	Execute bool `json:"execute"`
}

// ReviewWindow — сколько после завершения заказа можно оставить отзыв.
const ReviewWindow = 7 * 24 * time.Hour

// customerCancelStatuses — статусы, в которых заказчик может отменить заказ.
var customerCancelStatuses = []repository.OrderStatus{
	repository.OrderStatusSearching,
	repository.OrderStatusAssigned,
}

// orderViewer — тот, для кого собирается ответ.
type orderViewer struct {
	role   string
	userID uuid.UUID
}

func customerViewer(id uuid.UUID) orderViewer {
	return orderViewer{role: repository.RoleCustomer, userID: id}
}

func executorViewer(id uuid.UUID) orderViewer {
	return orderViewer{role: repository.RoleExecutor, userID: id}
}

// viewOf собирает карточку одного заказа. Это однозаказная форма viewsOf,
// которую используют списковые эндпоинты; обе делят одну реализацию, чтобы
// отрисованный заказ выглядел одинаково, каким бы путём его ни получили.
func (s *OrderService) viewOf(ctx context.Context, order *repository.Order) *OrderView {
	if order == nil {
		return nil
	}
	views, _ := s.viewsOf(ctx, []*repository.Order{order})
	return views[0]
}

// viewsOf собирает карточки целой страницы заказов тремя запросами: варианты,
// их категории и участники.
//
// Делать это по одному заказу стоило двух запросов на строку — вариант и, для
// назначенных заказов, исполнитель — на каждом списковом эндпоинте, который
// опрашивают приложения. Возвращает и участников заказов — заказчиков и
// исполнителей: по ним собирается вторая сторона в карточке и фильтруются ленты.
func (s *OrderService) viewsOf(ctx context.Context, orders []*repository.Order) ([]*OrderView, map[uuid.UUID]*repository.User) {
	users := map[uuid.UUID]*repository.User{}
	views := make([]*OrderView, 0, len(orders))
	if len(orders) == 0 {
		return views, users
	}

	variantIDs := make([]uuid.UUID, 0, len(orders))
	userIDs := make([]uuid.UUID, 0, 2*len(orders))
	for _, o := range orders {
		if o == nil {
			continue
		}
		variantIDs = append(variantIDs, o.ServiceVariantID)
		userIDs = append(userIDs, o.CustomerID)
		if o.ExecutorID != nil {
			userIDs = append(userIDs, *o.ExecutorID)
		}
	}

	variants := map[uuid.UUID]*repository.ServiceNode{}
	if s.catalogRepo != nil {
		if loaded, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs); err == nil {
			variants = loaded
		}
	}
	categories := loadOrderCategories(ctx, s.catalogRepo, variants)
	if s.userRepo != nil && len(userIDs) > 0 {
		if loaded, err := s.userRepo.FindByIDs(ctx, userIDs); err == nil {
			users = loaded
		}
	}

	for _, o := range orders {
		if o == nil {
			continue
		}
		v := &OrderView{Order: *o}
		if variant := variants[o.ServiceVariantID]; variant != nil {
			v.ServiceVariant = variant
			v.ServiceCategory = categoryOf(variant, categories)
			// Что исполнитель обязан отправить, прежде чем этот заказ можно завершить.
			// Только имена полей: их значения — то, с чем идёт сверка, и исполнителю
			// их показывать нельзя.
			if manifest, ok := s.behaviors.Manifest(variant); ok {
				v.SubmitFields = manifest.CheckFields
				v.RequirePassport = manifest.RequirePassport
				v.ScriptExecuted = !manifest.ExecutableByHand()
			}
		}
		if o.ExecutorID != nil {
			if execUser := users[*o.ExecutorID]; execUser != nil {
				v.ExecutorPhone = execUser.Phone
				v.ExecutorName = shortDisplayName(execUser)
			}
		}
		views = append(views, v)
	}
	return views, users
}

// presentOrders собирает карточки заказов под смотрящего.
func (s *OrderService) presentOrders(ctx context.Context, viewer orderViewer, orders []*repository.Order) []*OrderView {
	views, users := s.viewsOf(ctx, orders)
	presentFor(viewer, views, users, time.Now())
	s.attachPhotoProof(ctx, viewer, views)
	return views
}

// presentFor дособирает уже заполненные карточки под смотрящего. users —
// участники заказов, загруженные viewsOf.
func presentFor(viewer orderViewer, views []*OrderView, users map[uuid.UUID]*repository.User, now time.Time) {
	for _, v := range views {
		if v == nil {
			continue
		}
		v.Counterparty = counterpartyFor(viewer, v, users)
		v.Actions = actionsFor(viewer, v, now)
	}
}

func counterpartyFor(viewer orderViewer, o *OrderView, users map[uuid.UUID]*repository.User) *OrderParty {
	switch viewer.role {
	case repository.RoleCustomer:
		party := &OrderParty{Role: repository.RoleExecutor}
		if o.ExecutorID != nil {
			if u := users[*o.ExecutorID]; u != nil {
				party.Name = shortDisplayName(u)
				party.Phone = u.Phone
			}
		}
		return party
	case repository.RoleExecutor:
		party := &OrderParty{Role: repository.RoleCustomer}
		// В заказе со сверкой личности имя заказчика — то, что исполнитель должен
		// прочитать с документа, а не получить готовым.
		if len(o.SubmitFields) > 0 {
			party.Hidden = true
			return party
		}
		if u := users[o.CustomerID]; u != nil {
			party.Name = shortDisplayName(u)
		}
		return party
	}
	return nil
}

func actionsFor(viewer orderViewer, o *OrderView, now time.Time) *OrderActions {
	isCustomer := viewer.role == repository.RoleCustomer && o.CustomerID == viewer.userID
	isExecutor := viewer.role == repository.RoleExecutor && executorOf(&o.Order, viewer.userID)
	return &OrderActions{
		Cancel: isCustomer && slices.Contains(customerCancelStatuses, o.Status),
		Reject: isExecutor && executorCanReject(&o.Order, viewer.userID),
		Review: (isCustomer || isExecutor) && o.ExecutorID != nil && reviewOpen(&o.Order, now),
		// Скриптовые услуги закрываются сами, их не оспаривают (см. OpenDispute).
		Dispute: isCustomer && o.Status == repository.OrderStatusExecuted && o.ExecutorID != nil &&
			(o.ServiceVariant == nil || !o.ServiceVariant.HasBehavior()),
		Concede: isExecutor && o.Status == repository.OrderStatusDisputed,
		Execute: isExecutor && o.Status == repository.OrderStatusAssigned && !o.ScriptExecuted,
	}
}

func executorOf(o *repository.Order, executorID uuid.UUID) bool {
	return o.ExecutorID != nil && *o.ExecutorID == executorID
}

// executorCanReject — исполнитель может отказаться только от своего
// назначенного, ещё не исполненного заказа.
func executorCanReject(o *repository.Order, executorID uuid.UUID) bool {
	return o.Status == repository.OrderStatusAssigned && executorOf(o, executorID)
}

func reviewOpen(o *repository.Order, now time.Time) bool {
	if o.Status != repository.OrderStatusCompleted {
		return false
	}
	return o.CompletedAt == nil || now.Sub(*o.CompletedAt) <= ReviewWindow
}

// shortDisplayName отдаёт «Имя Отчество Ф.» — форму, в которой приложения
// показывают участников заказа: исполнителя заказчику и заказчика исполнителю.
func shortDisplayName(u *repository.User) string {
	var nameParts []string
	if u.FirstName != "" {
		nameParts = append(nameParts, u.FirstName)
	}
	if u.Patronymic != "" {
		nameParts = append(nameParts, u.Patronymic)
	}
	if u.LastName != "" {
		runes := []rune(strings.TrimSpace(u.LastName))
		if len(runes) > 0 {
			nameParts = append(nameParts, string(runes[0])+".")
		}
	}
	return strings.Join(nameParts, " ")
}

// loadOrderCategories возвращает родительские категории вариантов одним
// запросом на список, а не одним на заказ. Подпись «категория / услуга» нужна
// на каждом экране заказов, поэтому загрузка живёт в одном месте.
func loadOrderCategories(
	ctx context.Context,
	catalogRepo repository.ServiceCatalogRepository,
	variants map[uuid.UUID]*repository.ServiceNode,
) map[uuid.UUID]*repository.ServiceNode {
	if catalogRepo == nil || len(variants) == 0 {
		return nil
	}
	parentIDs := make([]uuid.UUID, 0, len(variants))
	for _, v := range variants {
		if v != nil && v.ParentID != nil {
			parentIDs = append(parentIDs, *v.ParentID)
		}
	}
	if len(parentIDs) == 0 {
		return nil
	}
	loaded, err := catalogRepo.GetNodesByIDs(ctx, parentIDs)
	if err != nil {
		return nil
	}
	return loaded
}

// categoryOf находит категорию варианта в уже загруженной пачке.
func categoryOf(variant *repository.ServiceNode, categories map[uuid.UUID]*repository.ServiceNode) *repository.ServiceNode {
	if variant == nil || variant.ParentID == nil || categories == nil {
		return nil
	}
	return categories[*variant.ParentID]
}

// categoryName — подпись категории для карты: русское имя, иначе английское.
func categoryName(category *repository.ServiceNode) string {
	if category == nil {
		return ""
	}
	if name := category.Name["ru"]; name != "" {
		return name
	}
	return category.Name["en"]
}
