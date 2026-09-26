package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Заказы вокруг исполнителя: одна реализация на список «Заказы поблизости» на
// дашборде и на карту. Раньше их было две, и они расходились в том, что
// показывают, — а показывать обе обязаны ровно то, что исполнитель может
// взять: тот же предикат, тот же радиус, та же позиция, что и путь принятия.

// OrdersAround возвращает заказы в поиске вокруг точки, видимые исполнителю,
// с расстоянием до каждого и флагом can_accept — для карты, которая точку
// берёт из сохранённой позиции исполнителя.
func (s *OrderService) OrdersAround(ctx context.Context, executorID uuid.UUID, lat, lon float64) ([]*MapOrderView, error) {
	return s.ordersAround(ctx, executorID, lat, lon, true)
}

// FindNearbyOrdersForExecutor возвращает обычные/крупные заказы в поиске рядом
// с исполнителем для списка на дашборде.
//
// Каждый заказ несёт can_accept и distance_km — ровно те же поля и с тем же
// смыслом, что и на карте. Раньше список их не отдавал, и дашборд рисовал
// кнопку взятия на каждой строке, не имея, чем её ограничить: радиус списка (5
// км) молча расходился с радиусом взятия, и заказ вне круга брался обычным
// нажатием. Радиус задаёт сервер, а не запрос: это настройка
// map_overview_radius_km, та же, по которой строится карта.
//
// Поиск привязывается к авторитетной сохранённой позиции исполнителя — той же
// точке, что используют карта и проверка радиуса принятия. Координаты клиента
// (GPS устройства, который может отсутствовать или падать в базовую точку) —
// лишь запасной вариант, когда хранилище не подключено, и тогда can_accept
// остаётся выключенным: решает всё равно сервер на пути принятия.
func (s *OrderService) FindNearbyOrdersForExecutor(ctx context.Context, executorID uuid.UUID, lat, lon float64) ([]*MapOrderView, error) {
	positionKnown := false
	if s.executorGeoRepo != nil {
		storedLat, storedLon, _, err := s.executorGeoRepo.GetExecutorLocation(ctx, executorID)
		if err != nil {
			return nil, err
		}
		if storedLat == nil || storedLon == nil {
			// Рабочая позиция ещё не задана: принять нечего, поэтому и в списке ничего нет.
			return []*MapOrderView{}, nil
		}
		lat, lon = *storedLat, *storedLon
		positionKnown = true
	}
	return s.ordersAround(ctx, executorID, lat, lon, positionKnown)
}

// ordersAround — общее тело. positionKnown говорит, доверять ли точке отсчёта:
// от координат, присланных клиентом, расстояние считать нельзя.
func (s *OrderService) ordersAround(ctx context.Context, executorID uuid.UUID, lat, lon float64, positionKnown bool) ([]*MapOrderView, error) {
	settings := loadSettingsMap(ctx, s.settingsRepo)
	acceptKM := acceptRadiusKM(settings, s.acceptRadiusFallbackKM)
	overviewKM := mapOverviewRadiusKM(settings, acceptKM)

	// Ограничиваем поиск в базе, а не в цикле ниже. Чтение каждого заказа в
	// поиске по всей стране с отбрасыванием всех, кроме ближних, делало стоимость
	// этого эндпоинта растущей вместе со всем маркетплейсом — на экране, который
	// каждый исполнитель держит открытым и опрашивает.
	orders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, int(overviewKM*1000))
	if err != nil {
		return nil, err
	}

	// Что смотрящему можно видеть, решают его набор ролей и верификация; роли
	// грузятся вместе с пользователем, поэтому модератор видит и заказы для
	// модераторов. Тихая блокировка читается один раз на смотрящего, а не на
	// каждый заказ списка.
	var viewer *repository.User
	if s.userRepo != nil {
		viewer, _ = s.userRepo.FindByID(ctx, executorID)
	}
	blocked := silentlyBlocked(ctx, s.penalties, viewer, repository.RoleExecutor)

	views, customers := s.viewsOf(ctx, orders)
	presentFor(executorViewer(executorID), views, customers, time.Now())

	result := []*MapOrderView{}
	for _, v := range views {
		// Один предикат и для карты, и для списка, и тот же, что применяет путь
		// принятия: заказы только для модераторов идут модераторам; обычные
		// заказы следуют сегментации по верификации заказчика и стандартным
		// проверкам исполнителя (requires_verification, min_age, бан).
		if s.userRepo != nil && canViewOrTakeOrder(ctx, s.behaviors, blocked, viewer, customers[v.CustomerID], v.ServiceVariant) != nil {
			continue
		}
		item := &MapOrderView{OrderView: *v, CategoryName: categoryName(v.ServiceCategory)}
		// Считать расстояние можно только от известной позиции.
		if positionKnown && v.PickupLat != nil && v.PickupLon != nil {
			item.DistanceKM = HaversineDistanceKM(lat, lon, *v.PickupLat, *v.PickupLon)
			item.CanAccept = item.DistanceKM <= acceptKM
		}
		result = append(result, item)
	}
	return result, nil
}
