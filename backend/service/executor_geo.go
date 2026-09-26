package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ErrLocationNotSet — у исполнителя нет сохранённой рабочей позиции.
var ErrLocationNotSet = ruleError("местоположение исполнителя не задано")

// NearbyOrdersSource — заказы вокруг точки, собранные под исполнителя. Ему
// удовлетворяет *OrderService: у карты и списка «Заказы поблизости» одна
// реализация, и живёт она у заказов.
type NearbyOrdersSource interface {
	OrdersAround(ctx context.Context, executorID uuid.UUID, lat, lon float64) ([]*MapOrderView, error)
}

// ExecutorGeoService ведёт рабочую позицию исполнителя: где он сейчас, куда
// поставил метку вручную и не «телепортировался» ли.
type ExecutorGeoService struct {
	geoRepo      repository.ExecutorGeoRepository
	settingsRepo repository.SettingsRepository
	// acceptRadiusFallbackKM — запасной радиус взятия из окружения (см.
	// acceptRadiusKM); ноль — умолчание.
	acceptRadiusFallbackKM float64
	// orders собирает заказы вокруг позиции для карты. Необязательно: без
	// него карта пуста.
	orders NearbyOrdersSource
	// track копит отчёты о местоположении в треке исполнителя: сохранённая
	// позиция отвечает на вопрос «где он сейчас», а трек — «где он был тогда».
	// Необязательно.
	track PositionRecorder
	// Кэш в памяти и мьютекс для быстрых проверок паузы
	cooldownMap sync.Map
}

// NewExecutorGeoService создаёт ExecutorGeoService.
func NewExecutorGeoService(geoRepo repository.ExecutorGeoRepository, settingsRepo repository.SettingsRepository) *ExecutorGeoService {
	return &ExecutorGeoService{geoRepo: geoRepo, settingsRepo: settingsRepo}
}

// WithAcceptRadiusFallback задаёт запасной радиус взятия, который действует,
// пока в админке радиус не настроен. main.go читает его из ACCEPT_RADIUS_KM.
func (s *ExecutorGeoService) WithAcceptRadiusFallback(km float64) *ExecutorGeoService {
	s.acceptRadiusFallbackKM = km
	return s
}

// WithNearbyOrders подключает источник заказов для карты.
func (s *ExecutorGeoService) WithNearbyOrders(orders NearbyOrdersSource) *ExecutorGeoService {
	s.orders = orders
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

// Гео-радиусы — настройки, а не константы сборки. Оба заводит миграция 049,
// оба правятся в админке: это рабочие параметры рынка, они зависят от плотности
// исполнителей в городе и меняются чаще, чем выкатывается образ.
const (
	// SettingAcceptRadiusKM — насколько далеко исполнитель может взять заказ.
	SettingAcceptRadiusKM = "accept_radius_km"
	// SettingMapOverviewRadiusKM — что показывать вокруг: радиус, в котором
	// заказы попадают на карту и в список «Заказы поблизости».
	SettingMapOverviewRadiusKM = "map_overview_radius_km"
)

// Умолчания — последний рубеж, если строки настройки нет (база старше миграции
// 049) и окружение молчит. Они повторяют значения, действовавшие до переноса.
const (
	defaultAcceptRadiusKM      = 0.5
	defaultMapOverviewRadiusKM = 10.0
	// maxMapOverviewRadiusKM ограничивает обзор сверху: запрос читает заказы в
	// круге, и настройка в тысячу километров превратила бы экран, открытый у
	// каждого исполнителя, в чтение всей таблицы заказов.
	maxMapOverviewRadiusKM = 50.0
)

// speedAlertThresholdKMH — скорость, выше которой перемещение считается
// подделкой GPS и оставляет GeoAlert; speedAlertMinShiftKM — перемещение
// короче этого не проверяется вовсе.
const (
	speedAlertThresholdKMH = 150.0
	speedAlertMinShiftKM   = 2.0
)

// acceptRadiusKM возвращает действующий радиус взятия заказа.
//
// Источник один на всех: и флаг can_accept на карте, и такой же флаг в списке
// на дашборде, и проверка на сервере при взятии заказа читают эту функцию.
// Разойтись они не могут — а разойдясь, давали бы ровно ту картину, с которой
// всё началось: карта пишет «нельзя взять», а сервер заказ отдаёт.
//
// Порядок источников: настройка из админки, затем fallbackKM — запасной радиус,
// который main.go один раз читает из ACCEPT_RADIUS_KM (ради установок, поднятых
// до появления настройки), а без него — умолчание defaultAcceptRadiusKM.
func acceptRadiusKM(settings settingsMap, fallbackKM float64) float64 {
	return settings.positiveFloat(SettingAcceptRadiusKM, acceptRadiusFallback(fallbackKM))
}

// acceptRadiusFallback подставляет умолчание вместо незаданного запасного
// радиуса: сервис, собранный без WithAcceptRadiusFallback, ведёт себя так же,
// как с пустой ACCEPT_RADIUS_KM.
func acceptRadiusFallback(km float64) float64 {
	if km > 0 {
		return km
	}
	return defaultAcceptRadiusKM
}

// mapOverviewRadiusKM возвращает радиус обзора — тот, в котором заказы
// показываются. Он всегда не меньше радиуса взятия: обзор уже зоны взятия
// означал бы, что исполнителю не показывают то, что ему разрешено брать.
func mapOverviewRadiusKM(settings settingsMap, acceptKM float64) float64 {
	radius := settings.positiveFloat(SettingMapOverviewRadiusKM, defaultMapOverviewRadiusKM)
	if radius > maxMapOverviewRadiusKM {
		radius = maxMapOverviewRadiusKM
	}
	if radius < acceptKM {
		radius = acceptKM
	}
	return radius
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
	radiusKM := acceptRadiusKM(loadSettingsMap(ctx, s.settingsRepo), s.acceptRadiusFallbackKM)

	// Проверяем дистанцию ручного сдвига смены
	var shiftDist float64
	if oldLat != nil && oldLon != nil {
		shiftDist = HaversineDistanceKM(*oldLat, *oldLon, req.Lat, req.Lon)
	}

	// Считается ли перемещение «ручным», решается здесь, по пройденному
	// расстоянию, а не по флагу, который присылает клиент: иначе исполнитель мог
	// бы обойти паузу, выставив is_manual в false.
	isManual := req.IsManual || shiftDist > radiusKM

	if isManual && oldLat != nil && oldLon != nil {
		// Отвергаем ручные перемещения внутри внутреннего круга
		if shiftDist <= radiusKM {
			return &SetLocationResponse{
				Success: false,
				Message: fmt.Sprintf("Ручное перемещение разрешено только за пределы разрешенного круга (более %.1f км)", radiusKM),
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

	// Проверка скорости — синхронно, до записи позиции. Это один INSERT, и
	// выигрыш от горутины был нулевым, а цена — ненулевой: горутина держала
	// контекст запроса, который net/http отменяет сразу после ответа, и INSERT
	// падал с «context canceled». GeoAlert — след подделки GPS для карточки
	// доказательств арбитража, и терять его молча нельзя. Сбой записи
	// логируется, но позицию не блокирует: алерт — наблюдение, а не запрет.
	if oldLat != nil && oldLon != nil && shiftDist > speedAlertMinShiftKM {
		if alert := speedAlert(executorID, *oldLat, *oldLon, req.Lat, req.Lon, shiftDist, lastManual, now); alert != nil {
			if err := s.geoRepo.CreateGeoAlert(ctx, alert); err != nil {
				log.Printf("[ExecutorGeoService] cannot record geo alert for %s: %v", executorID, err)
			}
		}
	}

	if err := s.geoRepo.UpdateExecutorLocation(ctx, executorID, req.Lat, req.Lon, isManual); err != nil {
		return nil, err
	}

	if isManual && shiftDist > radiusKM {
		s.cooldownMap.Store(executorID, now)
	}

	return &SetLocationResponse{
		Success: true,
		Message: "Координаты успешно обновлены",
		Lat:     req.Lat,
		Lon:     req.Lon,
	}, nil
}

// speedAlert решает, похоже ли перемещение на подделку GPS: расстояние
// shiftDist, пройденное с момента lastManual (или за последнюю минуту, если
// ручных перемещений не было), даёт скорость выше speedAlertThresholdKMH.
// Возвращает алерт для записи или nil.
func speedAlert(executorID uuid.UUID, oldLat, oldLon, newLat, newLon, shiftDist float64, lastManual *time.Time, now time.Time) *repository.GeoAlert {
	lastTime := now.Add(-1 * time.Minute)
	if lastManual != nil {
		lastTime = *lastManual
	}
	hours := now.Sub(lastTime).Hours()
	if hours <= 0 {
		return nil
	}
	speed := shiftDist / hours
	if speed <= speedAlertThresholdKMH {
		return nil
	}
	return &repository.GeoAlert{
		ExecutorID:         executorID,
		OldLat:             &oldLat,
		OldLon:             &oldLon,
		NewLat:             newLat,
		NewLon:             newLon,
		CalculatedSpeedKMH: speed,
		Status:             "PENDING",
	}
}

// PositionRecorder дописывает точку в трек исполнителя. Ему удовлетворяет
// *photoproof.Service; сервису местоположений нужно ровно столько.
type PositionRecorder interface {
	RecordLive(ctx context.Context, executorID uuid.UUID, lat, lon float64, at time.Time) error
}

// WithTrack подключает трек исполнителя к отчётам о местоположении.
func (s *ExecutorGeoService) WithTrack(track PositionRecorder) *ExecutorGeoService {
	s.track = track
	return s
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
	// Та же точка уходит в трек. Сбой записи трека не отменяет отчёта: трек —
	// доказательная история, а не условие работы карты и подбора.
	if s.track != nil {
		if err := s.track.RecordLive(ctx, executorID, lat, lon, time.Now()); err != nil {
			log.Printf("[ExecutorGeoService] cannot append the track of %s: %v", executorID, err)
		}
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
		return validationError("invalid coordinates")
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
// собрать адреса заказчиков по всей стране. Сами заказы собирает OrderService —
// тем же путём, что и список «Заказы поблизости».
func (s *ExecutorGeoService) GetMapOrders(ctx context.Context, executorID uuid.UUID) ([]*MapOrderView, error) {
	if s.orders == nil {
		return nil, ErrNotConfigured
	}
	lat, lon, _, err := s.geoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if lat == nil || lon == nil {
		return nil, ErrLocationNotSet
	}
	return s.orders.OrdersAround(ctx, executorID, *lat, *lon)
}

func (s *ExecutorGeoService) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return s.geoRepo.GetGeoAlerts(ctx, status, limit, offset)
}
