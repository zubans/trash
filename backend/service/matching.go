package service

import (
	"context"
	"log"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// defaultAutoMatchRadiusKM ограничивает автоматическое назначение. Он совпадает
// с радиусом, в котором карта исполнителя показывает заказы, поэтому воркер
// может раздавать только те заказы, которые исполнитель увидел бы и мог доехать.
const defaultAutoMatchRadiusKM = 10.0

// SettingAutoMatchingEnabled — ключ system_settings, включающий и выключающий
// автоматическое назначение. По умолчанию ВЫКЛ: пока он выключен, заказы
// берутся только нажатием исполнителем «взять» и никогда не назначаются воркером.
const SettingAutoMatchingEnabled = "auto_matching_enabled"

// SettingAutoMatchRadiusKM — радиус автоподбора.
const SettingAutoMatchRadiusKM = "auto_match_radius_km"

// MatchingService сопоставляет заказы в поиске с активными исполнителями.
type MatchingService struct {
	// penalties — тихая блокировка ролей; nil означает «механизм штрафов не подключён».
	penalties    *PenaltyService
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

// WithPenalties подключает тихую блокировку: заблокированный исполнитель не
// видит заказов и не может их брать.
func (s *MatchingService) WithPenalties(penalties *PenaltyService) *MatchingService {
	s.penalties = penalties
	return s
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

// matchingRound держит всё, что нужно одному циклу подбора, загруженное заранее.
//
// Воркер сравнивает каждый ждущий заказ с каждым исполнителем на смене. Когда
// каждое такое сравнение ходило в базу за исполнителем, вариантом услуги,
// позицией, числом назначенных заказов и тихой блокировкой, цикл стоил
// примерно пяти запросов на пару — а это растёт как произведение заказов и
// исполнителей, на таймере в пять секунд. Однократная загрузка каждого из этих
// наборов превращает сравнение в арифметику.
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
	// blocked — исполнители в тихой блокировке, одним запросом на раунд.
	blocked map[uuid.UUID]bool
}

// loadRound достаёт входные данные раунда фиксированным числом запросов.
func (s *MatchingService) loadRound(ctx context.Context, orders []*repository.Order, executorIDs []uuid.UUID) (*matchingRound, error) {
	round := &matchingRound{
		users:        map[uuid.UUID]*repository.User{},
		variants:     map[uuid.UUID]*repository.ServiceNode{},
		positions:    map[uuid.UUID]repository.ExecutorPosition{},
		activeOrders: map[uuid.UUID]int{},
		blocked:      map[uuid.UUID]bool{},
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
	if s.penalties != nil {
		blocked, err := s.penalties.SilentlyBlockedAmong(ctx, executorIDs, repository.RoleExecutor)
		if err != nil {
			return nil, err
		}
		round.blocked = blocked
	}

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
	return canViewOrTakeOrder(ctx, s.behaviors, round.blocked[executorID], executor, round.users[order.CustomerID], variant) == nil
}

// MatchOrders выполняет один цикл подбора. По таймеру его запускает
// worker.MatchingWorker — под защитой лидера, как и остальные периодические задачи.
func (s *MatchingService) MatchOrders(ctx context.Context) error {
	settings := loadSettingsMap(ctx, s.settingsRepo)
	// Автоматическое назначение включается явно. Пока оно выключено (по
	// умолчанию), воркер ничего не делает, и заказы берутся только вручную.
	if s.settingsRepo == nil || !settings.bool(SettingAutoMatchingEnabled, false) {
		return nil
	}

	// 1. Получаем заказы в поиске — страницей, старые первыми: очередь длиннее
	// страницы дойдёт до следующих тиков.
	orders, err := s.orderRepo.GetPendingOrders(ctx, 0)
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
	radiusKM := settings.positiveFloat(SettingAutoMatchRadiusKM, defaultAutoMatchRadiusKM)
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
