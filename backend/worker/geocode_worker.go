package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// GeocodeBackfillWorker fills pickup coordinates for searching orders that were
// stored without them — an older client that sent no coordinates and whose
// address could not be resolved at creation, or an order that predates
// coordinate capture.
//
// The executor map only plots orders that already carry coordinates
// (mapOrdersAround skips the rest), so this worker is where the deferred
// resolution happens: off the request path, in bounded batches, through the same
// address resolver (DaData) as everything else, with its cache absorbing repeats.
type GeocodeBackfillWorker struct {
	orderRepo repository.OrderRepository
	resolver  service.AddressResolver
	batchSize int
}

// NewGeocodeBackfillWorker creates a GeocodeBackfillWorker.
func NewGeocodeBackfillWorker(orderRepo repository.OrderRepository, resolver service.AddressResolver) *GeocodeBackfillWorker {
	return &GeocodeBackfillWorker{orderRepo: orderRepo, resolver: resolver, batchSize: 10}
}

// Start runs the worker loop periodically.
func (w *GeocodeBackfillWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("geocode_backfill", w.Run); err != nil {
				log.Printf("[GeocodeBackfillWorker] Error backfilling coordinates: %v", err)
			}
		}
	}()
	log.Printf("[GeocodeBackfillWorker] Background worker started every %v", interval)
}

// Run resolves one batch of coordinate-less orders and persists the results.
func (w *GeocodeBackfillWorker) Run() error {
	if w.resolver == nil {
		return nil
	}

	orders, err := w.orderRepo.GetOrdersMissingCoordinates(w.batchSize)
	if err != nil {
		return err
	}

	for _, o := range orders {
		if o.Address == nil || *o.Address == "" {
			continue
		}

		// Background work, so the root is context.Background(); a per-order
		// deadline still bounds each resolve independently of the batch.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		geo, err := w.resolver.Resolve(ctx, *o.Address)
		cancel()
		if err != nil {
			// Provider busy, address not found or upstream error: leave the order
			// for a later tick. Only coordinates are updated, so a retry is safe.
			log.Printf("[GeocodeBackfillWorker] resolve failed for order %s: %v", o.ID, err)
			continue
		}

		if err := w.orderRepo.SetPickupCoordinates(o.ID, geo.Lat, geo.Lon); err != nil {
			log.Printf("[GeocodeBackfillWorker] failed to persist coordinates for order %s: %v", o.ID, err)
		}
	}

	return nil
}
