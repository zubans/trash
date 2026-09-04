package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"healthlogin/backend/repository"
)

type ExecutorGeoService struct {
	geoRepo   repository.ExecutorGeoRepository
	orderRepo repository.OrderRepository
	// Необязательно. Когда подключено, карта применяет тот же предикат видимости,
	// что и список заказов исполнителя (роли, верификация заказчика, только для
	// модераторов), поэтому карта и список никогда не расходятся в том, что показано.
	userRepo     repository.UserRepository
	settingsRepo repository.SettingsRepository
	catalogRepo  repository.ServiceCatalogRepository
	// behaviors применяет скриптовые правила услуги к карте, чтобы карта показывала
	// ровно те заказы, что и список. Необязательно.
	behaviors *Behaviors
	// Кэш в памяти и мьютекс для быстрых проверок паузы
	cooldownMap sync.Map
}

func NewExecutorGeoService(geoRepo repository.ExecutorGeoRepository, orderRepo repository.OrderRepository) *ExecutorGeoService {
	return &ExecutorGeoService{
		geoRepo:   geoRepo,
		orderRepo: orderRepo,
	}
}

// WithEligibility подключает зависимости, нужные карте, чтобы применять тот же
// предикат видимости, что и список заказов исполнителя.
func (s *ExecutorGeoService) WithEligibility(userRepo repository.UserRepository, settingsRepo repository.SettingsRepository, catalogRepo repository.ServiceCatalogRepository) *ExecutorGeoService {
	s.userRepo = userRepo
	s.settingsRepo = settingsRepo
	s.catalogRepo = catalogRepo
	return s
}

// WithBehaviors подключает скрипты поведений к проверке видимости на карте.
func (s *ExecutorGeoService) WithBehaviors(behaviors *Behaviors) *ExecutorGeoService {
	s.behaviors = behaviors
	return s
}

type SetLocationRequest struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	IsManual bool    `json:"is_manual"`
}

type SetLocationResponse struct {
	Success                  bool    `json:"success"`
	Message                  string  `json:"message,omitempty"`
	CooldownRemainingSeconds int     `json:"cooldown_remaining_seconds,omitempty"`
	Lat                      float64 `json:"lat"`
	Lon                      float64 `json:"lon"`
}

func getAcceptRadiusKM() float64 {
	valStr := os.Getenv("ACCEPT_RADIUS_KM")
	if valStr == "" {
		return 0.5
	}
	var val float64
	if _, err := fmt.Sscanf(valStr, "%f", &val); err != nil || val <= 0 {
		return 0.5
	}
	return val
}

func (s *ExecutorGeoService) SetLocation(ctx context.Context, executorID uuid.UUID, req SetLocationRequest) (*SetLocationResponse, error) {
	if err := validateCoordinates(req.Lat, req.Lon); err != nil {
		return nil, err
	}

	oldLat, oldLon, lastManual, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	acceptRadiusKM := getAcceptRadiusKM()

	// Проверяем дистанцию ручного сдвига смены
	var shiftDist float64
	if oldLat != nil && oldLon != nil {
		shiftDist = HaversineDistanceKM(*oldLat, *oldLon, req.Lat, req.Lon)
	}

	// Считается ли перемещение «ручным», решается здесь, по пройденному
	// расстоянию, а не по флагу, который присылает клиент: иначе исполнитель мог
	// бы обойти паузу, выставив is_manual в false.
	isManual := req.IsManual || shiftDist > acceptRadiusKM

	if isManual && oldLat != nil && oldLon != nil {
		// Отвергаем ручные перемещения внутри внутреннего круга
		if shiftDist <= acceptRadiusKM {
			return &SetLocationResponse{
				Success: false,
				Message: fmt.Sprintf("Ручное перемещение разрешено только за пределы разрешенного круга (более %.1f км)", acceptRadiusKM),
				Lat:     *oldLat,
				Lon:     *oldLon,
			}, nil
		}

		// Смена района требует паузы в 10 минут
		var lastManualTime time.Time
		if val, ok := s.cooldownMap.Load(executorID); ok {
			lastManualTime = val.(time.Time)
		} else if lastManual != nil {
			lastManualTime = *lastManual
		}

		if !lastManualTime.IsZero() {
			elapsed := now.Sub(lastManualTime)
			if elapsed < 10*time.Minute {
				remaining := int((10*time.Minute - elapsed).Seconds())
				return &SetLocationResponse{
					Success:                  false,
					Message:                  fmt.Sprintf("Смена района возможна не чаще 1 раза в 10 минут. Осталось: %d сек", remaining),
					CooldownRemainingSeconds: remaining,
					Lat:                      *oldLat,
					Lon:                      *oldLon,
				}, nil
			}
		}
	}

	// Асинхронная горутина: глубокая гео-проверка на подделку скорости
	if oldLat != nil && oldLon != nil && shiftDist > 2.0 {
		go func(exID uuid.UUID, oLat, oLon, nLat, nLon float64, tNow time.Time) {
			var lastTime time.Time
			if lastManual != nil {
				lastTime = *lastManual
			} else {
				lastTime = tNow.Add(-1 * time.Minute)
			}
			hours := tNow.Sub(lastTime).Hours()
			if hours > 0 {
				speed := shiftDist / hours
				if speed > 150.0 {
					// Обнаружена подделка GPS! Пишем GeoAlert для админа
					_ = s.geoRepo.CreateGeoAlert(ctx, &repository.GeoAlert{
						ExecutorID:         exID,
						OldLat:             &oLat,
						OldLon:             &oLon,
						NewLat:             nLat,
						NewLon:             nLon,
						CalculatedSpeedKMH: speed,
						Status:             "PENDING",
					})
				}
			}
		}(executorID, *oldLat, *oldLon, req.Lat, req.Lon, now)
	}

	if err := s.geoRepo.UpdateExecutorLocation(ctx, executorID, req.Lat, req.Lon, isManual); err != nil {
		return nil, err
	}

	if isManual && shiftDist > acceptRadiusKM {
		s.cooldownMap.Store(executorID, now)
	}

	return &SetLocationResponse{
		Success: true,
		Message: "Координаты успешно обновлены",
		Lat:     req.Lat,
		Lon:     req.Lon,
	}, nil
}

// RecordLiveLocation сохраняет позицию, о которой приложение исполнителя
// сообщает само во время смены.
//
// Отчёт — это телеметрия, а не команда. Он записывается всегда, но двигает
// рабочий якорь только пока исполнитель не выбрал район вручную: иначе телефон
// тихо утаскивал бы рабочую зону с выбранного района через несколько секунд
// после выбора. Нажатие «моё местоположение» — то, что возвращает якорь под
// управление устройства, см. FollowDevice.
//
// Это заодно закрывает лазейку старой схемы: раз якорь больше не двигается
// по пассивному отчёту, отчётом больше нельзя обойти паузу при смене
// района.
func (s *ExecutorGeoService) RecordLiveLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	if err := validateCoordinates(lat, lon); err != nil {
		return false, err
	}
	if err := s.geoRepo.RecordDevicePosition(ctx, executorID, lat, lon); err != nil {
		return false, err
	}
	return true, nil
}

// FollowDevice переносит рабочий якорь на позицию, о которой сообщает телефон
// исполнителя, и возвращает управление устройству.
//
// Это то, что делает кнопка «моё местоположение». Это не смена района:
// исполнитель возвращается туда, где он на самом деле есть, поэтому паузы это
// не несёт и ручное переопределение снимает, а не ставит.
func (s *ExecutorGeoService) FollowDevice(ctx context.Context, executorID uuid.UUID, lat, lon float64) (*SetLocationResponse, error) {
	if err := validateCoordinates(lat, lon); err != nil {
		return nil, err
	}
	if err := s.geoRepo.FollowDevicePosition(ctx, executorID, lat, lon); err != nil {
		return nil, err
	}
	// Пауза привязана к ручным перемещениям, а это завершает ручное
	// переопределение, поэтому копия в памяти должна уйти вместе с ним.
	s.cooldownMap.Delete(executorID)
	return &SetLocationResponse{
		Success: true,
		Message: "Метка возвращена к вашему местоположению",
		Lat:     lat,
		Lon:     lon,
	}, nil
}

// validateCoordinates отвергает точки, которых нет на глобусе.
func validateCoordinates(lat, lon float64) error {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return fmt.Errorf("invalid coordinates")
	}
	return nil
}

// LocationResponse сообщает авторитетную сохранённую позицию исполнителя. Это
// единственный источник истины, по которому центрируется карта, поэтому клиенту
// никогда не приходится гадать по возможно устаревшей координате устройства.
type LocationResponse struct {
	HasLocation bool     `json:"has_location"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

// GetLocation возвращает собственные сохранённые координаты исполнителя. Как и
// в GetMapOrders, позиция берётся из базы и ограничена вызывающим, поэтому ею
// нельзя узнать, где находится другой исполнитель.
func (s *ExecutorGeoService) GetLocation(ctx context.Context, executorID uuid.UUID) (*LocationResponse, error) {
	lat, lon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if lat == nil || lon == nil {
		return &LocationResponse{HasLocation: false}, nil
	}
	return &LocationResponse{HasLocation: true, Lat: lat, Lon: lon}, nil
}

// GetMapOrders возвращает заказы в поиске вокруг собственной сохранённой
// позиции исполнителя. Позиция намеренно берётся из базы, а не из параметров
// запроса: с координатами от клиента любая учётка могла бы прочесать карту и
// собрать адреса заказчиков по всей стране.
func (s *ExecutorGeoService) GetMapOrders(ctx context.Context, executorID uuid.UUID) ([]repository.MapOrder, error) {
	lat, lon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if lat == nil || lon == nil {
		return nil, errors.New("местоположение исполнителя не задано")
	}
	return s.mapOrdersAround(ctx, executorID, *lat, *lon)
}

func (s *ExecutorGeoService) mapOrdersAround(ctx context.Context, executorID uuid.UUID, lat, lon float64) ([]repository.MapOrder, error) {
	// Ищем ожидающие заказы в радиусе 10 км
	const overviewRadiusKM = 10.0
	acceptRadiusKM := getAcceptRadiusKM()

	// Ограничиваем поиск в базе, а не в этом цикле. Чтение каждого заказа в
	// поиске по всей стране с отбрасыванием всех, кроме ближних, делало стоимость
	// этого эндпоинта растущей вместе со всем маркетплейсом — на экране, который
	// каждый исполнитель держит открытым и опрашивает.
	pendingOrders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, int(overviewRadiusKM*1000))
	if err != nil {
		return nil, err
	}
	if len(pendingOrders) == 0 {
		return nil, nil
	}

	// Видимость определяют роли и верификация смотрящего, ровно как в списке
	// заказов исполнителя, поэтому карта и список никогда не расходятся.
	var viewer *repository.User
	if s.userRepo != nil {
		viewer, _ = s.userRepo.FindByID(ctx, executorID)
	}

	// Всё, что нужно предикату, за два запроса вместо двух на заказ. Сам предикат
	// не изменился и по-прежнему единственный решает видимость: отличается лишь
	// то, как загружаются его входные данные.
	customers, variants := s.eligibilityInputs(ctx, pendingOrders)

	// Категории (родители вариантов), чтобы карта могла подписать каждый заказ
	// как «категория · услуга». Пакетно, поэтому это один лишний запрос, а не N.
	categories := loadOrderCategories(ctx, s.catalogRepo, variants)

	var mapOrders []repository.MapOrder

	for _, o := range pendingOrders {
		// FindNearbyOrders возвращает только заказы с координатами, поэтому
		// разыменование ниже безопасно; проверка остаётся на всякий случай, если
		// контракт запроса когда-нибудь изменится.
		if o.PickupLat == nil || o.PickupLon == nil {
			continue
		}

		// Тот же предикат, что в FindNearbyOrdersForExecutor и на пути принятия:
		// заказы только для модераторов → модераторам; обычные заказы → сегментация
		// по верификации заказчика плюс стандартные проверки исполнителя.
		if s.userRepo != nil {
			if canViewOrTakeOrder(ctx, s.behaviors, viewer, customers[o.CustomerID], variants[o.ServiceVariantID]) != nil {
				continue
			}
		}

		oLat, oLon := *o.PickupLat, *o.PickupLon

		dist := HaversineDistanceKM(lat, lon, oLat, oLon)
		if dist <= overviewRadiusKM {
			// Прикрепляем вариант (название услуги) и разрешаем название категории,
			// чтобы клиент рисовал «категория · услуга» без лишних запросов.
			oc := *o
			categoryName := ""
			if v := variants[o.ServiceVariantID]; v != nil {
				oc.ServiceVariant = v
				oc.ServiceCategory = categoryOf(v, categories)
				if v.ParentID != nil {
					if cat := categories[*v.ParentID]; cat != nil {
						categoryName = cat.Name["ru"]
						if categoryName == "" {
							categoryName = cat.Name["en"]
						}
					}
				}
			}
			mapOrders = append(mapOrders, repository.MapOrder{
				Order:        oc,
				CanAccept:    dist <= acceptRadiusKM,
				DistanceKM:   dist,
				CategoryName: categoryName,
			})
		}
	}

	return mapOrders, nil
}

// eligibilityInputs пакетно загружает заказчиков и варианты услуг, которые
// нужны canViewOrTakeOrder для страницы заказов.
//
// Неудача чтения даёт отсутствующую запись в карте, а не ошибку, — так же, как
// вели себя вызовы по одному заказу: предикат уже трактует nil-заказчика или
// nil-вариант как «нет дополнительных ограничений», и одна нечитаемая строка не
// должна обнулять всю карту.
func (s *ExecutorGeoService) eligibilityInputs(ctx context.Context, orders []*repository.Order) (map[uuid.UUID]*repository.User, map[uuid.UUID]*repository.ServiceNode) {
	customerIDs := make([]uuid.UUID, 0, len(orders))
	variantIDs := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		customerIDs = append(customerIDs, o.CustomerID)
		variantIDs = append(variantIDs, o.ServiceVariantID)
	}

	customers := map[uuid.UUID]*repository.User{}
	if s.userRepo != nil {
		if loaded, err := s.userRepo.FindByIDs(ctx, customerIDs); err == nil {
			customers = loaded
		}
	}
	variants := map[uuid.UUID]*repository.ServiceNode{}
	if s.catalogRepo != nil {
		if loaded, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs); err == nil {
			variants = loaded
		}
	}
	return customers, variants
}

func (s *ExecutorGeoService) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return s.geoRepo.GetGeoAlerts(ctx, status, limit, offset)
}
