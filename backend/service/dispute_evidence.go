package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
)

// Отметки расхождений в карточке доказательств.
const (
	// EvidenceSeal — подпись файла не подтверждена.
	EvidenceSeal = "SEAL"
	// EvidenceMark — метка в изображении не найдена или чужая.
	EvidenceMark = "MARK"
	// EvidenceTime — снимок сделан далеко по времени от отметки «Исполнил».
	EvidenceTime = "TIME"
	// EvidenceExifTime — время в EXIF расходится с временем, названным телефоном.
	EvidenceExifTime = "EXIF_TIME"
	// EvidenceDistance — снимок сделан далеко от адреса заказа.
	EvidenceDistance = "DISTANCE"
	// EvidenceNoTrack — рядом со временем съёмки нет ни одной точки трека.
	EvidenceNoTrack = "NO_TRACK"
	// EvidenceTrackDistance — трек в момент съёмки был далеко от места снимка.
	EvidenceTrackDistance = "TRACK_DISTANCE"
	// EvidenceGeoAlert — вокруг времени съёмки есть аномалия скорости.
	EvidenceGeoAlert = "GEO_ALERT"
)

// geoAlertWindow — насколько вокруг съёмки арбитр смотрит на аномалии скорости.
const geoAlertWindow = time.Hour

// ProofEvidenceSource — то, что карточке доказательств нужно от модуля
// фото-подтверждения. Ему удовлетворяет *photoproof.Service.
type ProofEvidenceSource interface {
	ProofsForOrder(ctx context.Context, q photoproof.Querier, orderID uuid.UUID) ([]photoproof.Proof, error)
	NearestPosition(ctx context.Context, q photoproof.Querier, executorID uuid.UUID, at time.Time) (*photoproof.Position, error)
	Gestures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]photoproof.Symbol, error)
}

// WithEvidence подключает источник доказательств для арбитража.
func (s *OrderService) WithEvidence(source ProofEvidenceSource) *OrderService {
	s.evidence = source
	return s
}

// DisputeEvidence — всё, что арбитру нужно сверить по спору.
type DisputeEvidence struct {
	Dispute *repository.Dispute `json:"dispute"`
	Order   EvidenceOrder       `json:"order"`
	// Gesture — жест, выданный заказу при взятии; nil, если фото не требовалось.
	Gesture *repository.OrderGesture `json:"gesture,omitempty"`
	Proofs  []ProofEvidence          `json:"proofs"`
	Limits  EvidenceLimits           `json:"limits"`
}

// EvidenceOrder — заказ глазами сверки.
type EvidenceOrder struct {
	ID               uuid.UUID  `json:"id"`
	Status           string     `json:"status"`
	Address          string     `json:"address,omitempty"`
	PickupLat        *float64   `json:"pickup_lat,omitempty"`
	PickupLon        *float64   `json:"pickup_lon,omitempty"`
	PhotoRequired    bool       `json:"photo_required"`
	ExecutedAt       *time.Time `json:"executed_at,omitempty"`
	ExecutedAtDevice *time.Time `json:"executed_at_device,omitempty"`
}

// EvidenceLimits — пороги, по которым расставлены отметки.
type EvidenceLimits struct {
	MaxTimeDiffMin  int `json:"max_time_diff_min"`
	MaxDistanceM    int `json:"max_distance_m"`
	MaxTrackGapMin  int `json:"max_track_gap_min"`
	GeoAlertWindowM int `json:"geo_alert_window_min"`
}

// ProofEvidence — один снимок и его сверка.
type ProofEvidence struct {
	photoproof.Proof
	// FileURL — откуда арбитру взять файл: через админку, по праву на споры.
	FileURL string `json:"file_url"`

	// TakenVsExecutedMin — минуты от съёмки до отметки «Исполнил» (по часам
	// телефона, если они есть, иначе по серверу); отрицательное — снято после.
	TakenVsExecutedMin *float64 `json:"taken_vs_executed_min,omitempty"`
	// ExifVsDeviceMin — расхождение времени EXIF и времени, названного телефоном.
	ExifVsDeviceMin *float64 `json:"exif_vs_device_min,omitempty"`
	// DistanceToOrderM — от места съёмки (телефон, иначе EXIF) до адреса.
	DistanceToOrderM     *float64 `json:"distance_to_order_m,omitempty"`
	ExifDistanceToOrderM *float64 `json:"exif_distance_to_order_m,omitempty"`

	Track     *TrackEvidence        `json:"track,omitempty"`
	GeoAlerts []repository.GeoAlert `json:"geo_alerts"`
	Flags     []string              `json:"flags"`
}

// TrackEvidence — ближайшая к съёмке точка трека.
type TrackEvidence struct {
	photoproof.Position
	// AgeMin — насколько точка раньше (−) или позже (+) съёмки.
	AgeMin           float64  `json:"age_min"`
	DistanceToPhotoM *float64 `json:"distance_to_photo_m,omitempty"`
	DistanceToOrderM *float64 `json:"distance_to_order_m,omitempty"`
}

func metersBetween(lat1, lon1 *float64, lat2, lon2 *float64) *float64 {
	if lat1 == nil || lon1 == nil || lat2 == nil || lon2 == nil {
		return nil
	}
	m := HaversineDistanceKM(*lat1, *lon1, *lat2, *lon2) * 1000
	return &m
}

func minutesBetween(a, b time.Time) float64 {
	return b.Sub(a).Minutes()
}

func (s *OrderService) evidenceLimits(ctx context.Context) EvidenceLimits {
	limits := EvidenceLimits{
		MaxTimeDiffMin:  defaultPhotoProofMaxTimeDiffMin,
		MaxDistanceM:    defaultPhotoProofMaxDistanceM,
		MaxTrackGapMin:  15,
		GeoAlertWindowM: int(geoAlertWindow.Minutes()),
	}
	if s.settingsRepo == nil {
		return limits
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return limits
	}
	read := func(key string, into *int) {
		if v, err := strconv.Atoi(settings[key]); err == nil && v > 0 {
			*into = v
		}
	}
	read(SettingPhotoProofMaxTimeDiffMin, &limits.MaxTimeDiffMin)
	read(SettingPhotoProofMaxDistanceM, &limits.MaxDistanceM)
	read(photoproof.SettingMaxTrackGapMin, &limits.MaxTrackGapMin)
	return limits
}

// DisputeEvidence собирает карточку доказательств спора: снимки, жест, времена,
// расстояния, ближайшую точку трека и аномалии скорости — с отметками там, где
// значения расходятся больше порогов. Отметка — повод присмотреться, а не
// решение: решает арбитр.
func (s *OrderService) DisputeEvidence(ctx context.Context, disputeID uuid.UUID) (*DisputeEvidence, error) {
	if s.disputes == nil {
		return nil, ErrDisputeNotFound
	}
	dispute, err := s.disputes.FindByID(ctx, nil, disputeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDisputeNotFound
	}
	if err != nil {
		return nil, err
	}
	order, err := s.orderRepo.GetOrderByID(ctx, dispute.OrderID)
	if err != nil {
		return nil, err
	}

	ev := &DisputeEvidence{
		Dispute: dispute,
		Order: EvidenceOrder{
			ID: order.ID, Status: string(order.Status), PickupLat: order.PickupLat, PickupLon: order.PickupLon,
			PhotoRequired: order.PhotoRequired, ExecutedAt: order.ExecutedAt, ExecutedAtDevice: order.ExecutedAtDevice,
		},
		Proofs: []ProofEvidence{},
		Limits: s.evidenceLimits(ctx),
	}
	if order.Address != nil {
		ev.Order.Address = *order.Address
	}
	if s.evidence == nil {
		return ev, nil
	}

	if order.WatermarkSymbolID != nil {
		gestures, err := s.evidence.Gestures(ctx, []uuid.UUID{*order.WatermarkSymbolID})
		if err != nil {
			return nil, err
		}
		if g, ok := gestures[*order.WatermarkSymbolID]; ok {
			ev.Gesture = &repository.OrderGesture{
				Code: g.Code, Number: g.Number, Title: g.Title, Description: g.Description,
				HintImageURL: g.HintImageURL, FitsInSelfie: g.FitsInSelfie,
			}
		}
	}

	proofs, err := s.evidence.ProofsForOrder(ctx, nil, order.ID)
	if err != nil {
		return nil, err
	}
	executedAt := order.ExecutedAtDevice
	if executedAt == nil {
		executedAt = order.ExecutedAt
	}
	for _, p := range proofs {
		item, err := s.proofEvidence(ctx, ev.Limits, order, executedAt, p)
		if err != nil {
			return nil, err
		}
		ev.Proofs = append(ev.Proofs, item)
	}
	return ev, nil
}

func (s *OrderService) proofEvidence(ctx context.Context, limits EvidenceLimits, order *repository.Order, executedAt *time.Time, p photoproof.Proof) (ProofEvidence, error) {
	item := ProofEvidence{
		Proof:     p,
		FileURL:   "/api/admin/photo-proofs/" + p.ID.String() + "/file",
		GeoAlerts: []repository.GeoAlert{},
		Flags:     []string{},
	}
	flag := func(f string) { item.Flags = append(item.Flags, f) }
	maxDiff := float64(limits.MaxTimeDiffMin)
	maxDist := float64(limits.MaxDistanceM)

	if p.SealStatus != photoproof.SealValid {
		flag(EvidenceSeal)
	}
	if p.MarkStatus != photoproof.MarkFound {
		flag(EvidenceMark)
	}

	if executedAt != nil {
		diff := minutesBetween(p.DeviceTakenAt, *executedAt)
		item.TakenVsExecutedMin = &diff
		if math.Abs(diff) > maxDiff {
			flag(EvidenceTime)
		}
	}
	if p.ExifTakenAt != nil {
		diff := minutesBetween(p.DeviceTakenAt, *p.ExifTakenAt)
		item.ExifVsDeviceMin = &diff
		if math.Abs(diff) > maxDiff {
			flag(EvidenceExifTime)
		}
	}

	photoLat, photoLon := p.DeviceLat, p.DeviceLon
	if photoLat == nil || photoLon == nil {
		photoLat, photoLon = p.ExifLat, p.ExifLon
	}
	item.DistanceToOrderM = metersBetween(photoLat, photoLon, order.PickupLat, order.PickupLon)
	item.ExifDistanceToOrderM = metersBetween(p.ExifLat, p.ExifLon, order.PickupLat, order.PickupLon)
	if item.DistanceToOrderM != nil && *item.DistanceToOrderM > maxDist {
		flag(EvidenceDistance)
	}

	nearest, err := s.evidence.NearestPosition(ctx, nil, p.ExecutorID, p.DeviceTakenAt)
	if err != nil {
		return item, err
	}
	if nearest == nil {
		flag(EvidenceNoTrack)
	} else {
		track := &TrackEvidence{Position: *nearest, AgeMin: minutesBetween(p.DeviceTakenAt, nearest.DeviceAt)}
		track.DistanceToPhotoM = metersBetween(&nearest.Lat, &nearest.Lon, photoLat, photoLon)
		track.DistanceToOrderM = metersBetween(&nearest.Lat, &nearest.Lon, order.PickupLat, order.PickupLon)
		if track.DistanceToPhotoM != nil && *track.DistanceToPhotoM > maxDist {
			flag(EvidenceTrackDistance)
		}
		item.Track = track
	}

	if s.executorGeoRepo != nil {
		alerts, err := s.executorGeoRepo.GeoAlertsBetween(ctx, p.ExecutorID,
			p.DeviceTakenAt.Add(-geoAlertWindow), p.DeviceTakenAt.Add(geoAlertWindow))
		if err != nil {
			return item, err
		}
		if len(alerts) > 0 {
			item.GeoAlerts = alerts
			flag(EvidenceGeoAlert)
		}
	}
	return item, nil
}
