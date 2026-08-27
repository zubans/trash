package worker

import (
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// GeocodeBackfillWorker fills pickup coordinates for searching orders that were
// stored without them — because the OSM/Nominatim lookup failed when the order
// was created (busy slot, upstream error, address not found), or because the
// order predates coordinate capture.
//
// The executor map only plots orders that already carry coordinates
// (mapOrdersAround skips the rest), and geocoding was deliberately removed from
// that request path because it hammered a shared 1/s upstream. This worker is
// where the deferred geocoding now happens: off the request path, in bounded
// batches, using the same geocoder — which serialises at 1/s and caches hits.
type GeocodeBackfillWorker struct {
	orderRepo repository.OrderRepository
	geocoder  *service.Geocoder
	batchSize int
}

// NewGeocodeBackfillWorker creates a GeocodeBackfillWorker.
func NewGeocodeBackfillWorker(orderRepo repository.OrderRepository, geocoder *service.Geocoder) *GeocodeBackfillWorker {
	return &GeocodeBackfillWorker{orderRepo: orderRepo, geocoder: geocoder, batchSize: 10}
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

// Run geocodes one batch of coordinate-less orders and persists the results.
func (w *GeocodeBackfillWorker) Run() error {
	if w.geocoder == nil {
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

		// Geocode blocks on the shared once-per-second Nominatim slot and gives
		// up with ErrGeocoderBusy rather than queueing forever, so a full batch
		// costs at most batchSize seconds and never stalls order creation.
		geo, err := w.geocoder.Geocode(*o.Address)
		if err != nil {
			// Busy / not found / upstream error: leave the order for a later
			// tick. Only coordinates are updated, so a retry is harmless.
			log.Printf("[GeocodeBackfillWorker] geocode failed for order %s: %v", o.ID, err)
			continue
		}

		if err := w.orderRepo.SetPickupCoordinates(o.ID, geo.Lat, geo.Lon); err != nil {
			log.Printf("[GeocodeBackfillWorker] failed to persist coordinates for order %s: %v", o.ID, err)
		}
	}

	return nil
}
