package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
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
	guard func(func() error) error
}

// NewGeocodeBackfillWorker создаёт GeocodeBackfillWorker.
func NewGeocodeBackfillWorker(orderRepo repository.OrderRepository, resolver service.AddressResolver) *GeocodeBackfillWorker {
	return &GeocodeBackfillWorker{orderRepo: orderRepo, resolver: resolver, batchSize: 10}
}

// Start периодически выполняет цикл воркера.
func (w *GeocodeBackfillWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("geocode_backfill", func() error { return w.runGuarded(w.Run) }); err != nil {
				log.Printf("[GeocodeBackfillWorker] Error backfilling coordinates: %v", err)
			}
		}
	}()
	log.Printf("[GeocodeBackfillWorker] Background worker started every %v", interval)
}

// Run разрешает одну пачку заказов без координат и сохраняет результаты.
func (w *GeocodeBackfillWorker) Run() error {
	if w.resolver == nil {
		return nil
	}

	orders, err := w.orderRepo.GetOrdersMissingCoordinates(context.Background(), w.batchSize)
	if err != nil {
		return err
	}

	for _, o := range orders {
		if o.Address == nil || *o.Address == "" {
			continue
		}

		// Фоновая работа, поэтому корень — context.Background(); дедлайн на заказ
		// всё равно ограничивает каждое разрешение независимо от пачки.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		geo, err := w.resolver.Resolve(ctx, *o.Address)
		cancel()
		if err != nil {
			// Провайдер занят, адрес не найден или ошибка внешнего сервиса: оставляем
			// заказ на следующий тик. Обновляются только координаты, поэтому повтор безопасен.
			log.Printf("[GeocodeBackfillWorker] resolve failed for order %s: %v", o.ID, err)
			continue
		}

		if err := w.orderRepo.SetPickupCoordinates(context.Background(), o.ID, geo.Lat, geo.Lon); err != nil {
			log.Printf("[GeocodeBackfillWorker] failed to persist coordinates for order %s: %v", o.ID, err)
		}
	}

	return nil
}

// guard выполняет один тик под advisory-блокировкой задачи, когда подключён
// Leader, чтобы вторая реплика пропустила тик, а не продублировала его. Без
// него выполняется напрямую — так и делают однопроцессный деплой и тесты.
func (w *GeocodeBackfillWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *GeocodeBackfillWorker) WithLeader(leader *Leader, name string) *GeocodeBackfillWorker {
	w.guard = leader.Guard(name)
	return w
}
