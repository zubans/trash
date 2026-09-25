package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// GeocodeBackfillWorker заполняет координаты подачи у заказов в поиске, которые
// сохранились без них: от старого клиента, не приславшего координат и чей адрес
// не удалось разрешить при создании, или от заказа, появившегося раньше захвата
// координат.
//
// Карта исполнителя рисует только заказы, уже несущие координаты
// (mapOrdersAround остальные пропускает), поэтому отложенное разрешение
// происходит именно в этом воркере: вне пути запроса, ограниченными пачками,
// через тот же разрешатель адресов (DaData), чей кэш поглощает повторы.
type GeocodeBackfillWorker struct {
	orderRepo repository.OrderRepository
	resolver  service.AddressResolver
	batchSize int
	guard     Guard
}

// NewGeocodeBackfillWorker создаёт GeocodeBackfillWorker.
func NewGeocodeBackfillWorker(orderRepo repository.OrderRepository, resolver service.AddressResolver) *GeocodeBackfillWorker {
	return &GeocodeBackfillWorker{orderRepo: orderRepo, resolver: resolver, batchSize: 10}
}

// Start периодически выполняет цикл воркера.
func (w *GeocodeBackfillWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "GeocodeBackfillWorker", metric: "geocode_backfill", guard: w.guard}.
		Start(ctx, interval, w.Run)
}

// Run разрешает одну пачку заказов без координат и сохраняет результаты.
func (w *GeocodeBackfillWorker) Run(ctx context.Context) error {
	if w.resolver == nil {
		return nil
	}

	orders, err := w.orderRepo.GetOrdersMissingCoordinates(ctx, w.batchSize)
	if err != nil {
		return err
	}

	for _, o := range orders {
		if o.Address == nil || *o.Address == "" {
			continue
		}

		// Дедлайн на заказ ограничивает каждое разрешение независимо от пачки.
		resolveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		geo, err := w.resolver.Resolve(resolveCtx, *o.Address)
		cancel()
		if err != nil {
			// Провайдер занят, адрес не найден или ошибка внешнего сервиса: оставляем
			// заказ на следующий тик. Обновляются только координаты, поэтому повтор безопасен.
			log.Printf("[GeocodeBackfillWorker] resolve failed for order %s: %v", o.ID, err)
			continue
		}

		if err := w.orderRepo.SetPickupCoordinates(ctx, o.ID, geo.Lat, geo.Lon); err != nil {
			log.Printf("[GeocodeBackfillWorker] failed to persist coordinates for order %s: %v", o.ID, err)
		}
	}

	return nil
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *GeocodeBackfillWorker) WithLeader(leader *Leader, name string) *GeocodeBackfillWorker {
	w.guard = leader.Guard(name)
	return w
}
