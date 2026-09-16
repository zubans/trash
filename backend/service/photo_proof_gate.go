package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log"

	"github.com/google/uuid"

	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
)

// ErrPhotoProofRequired — заказ требует фото-подтверждения, а снимка места
// заказа ещё нет.
var ErrPhotoProofRequired = errors.New("заказ требует фото-подтверждения: сначала загрузите снимок места заказа с жестом")

// PhotoProofGate — то, что заказам нужно от модуля фото-подтверждения. Ему
// удовлетворяет *photoproof.Service.
type PhotoProofGate interface {
	// RequireForOrderTx выдаёт заказу жест и служебные данные проверки.
	RequireForOrderTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error
	// HasRequiredPhotoTx сообщает, загружен ли обязательный снимок.
	HasRequiredPhotoTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) (bool, error)
	// Gestures отдаёт жесты по id.
	Gestures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]photoproof.Symbol, error)
}

// WithPhotoProof подключает фото-подтверждение к заказам.
func (s *OrderService) WithPhotoProof(gate PhotoProofGate) *OrderService {
	s.photoProof = gate
	return s
}

// requirePhotoProofTx решает при взятии заказа, требует ли он фото: да, если
// период доп. задания идёт у исполнителя или у заказчика. Решение фиксируется
// в заказе в момент взятия, поэтому период, начавшийся позже, взятый заказ не
// затрагивает, а закончившийся — не отменяет требования.
func (s *OrderService) requirePhotoProofTx(ctx context.Context, tx *sql.Tx, order *repository.Order, executorID uuid.UUID) error {
	if s.photoProof == nil || s.penalties == nil {
		return nil
	}
	if !s.penalties.PhotoRequired(ctx, executorID, repository.RoleExecutor) &&
		!s.penalties.PhotoRequired(ctx, order.CustomerID, repository.RoleCustomer) {
		return nil
	}
	return s.photoProof.RequireForOrderTx(ctx, tx, order.ID)
}

// attachPhotoProof собирает для исполнителя заказа то, что нужно снять
// фото-подтверждение без сети: жест и служебные данные проверки. Заказчику не
// отдаётся ничего сверх флага photo_required: жест — задание исполнителю, а
// служебные данные — не его дело вовсе.
func (s *OrderService) attachPhotoProof(ctx context.Context, viewer orderViewer, orders []*repository.Order) {
	if s.photoProof == nil || viewer.role != repository.RoleExecutor {
		return
	}
	var ids []uuid.UUID
	for _, o := range orders {
		if o != nil && o.PhotoRequired && executorOf(o, viewer.userID) && o.WatermarkSymbolID != nil {
			ids = append(ids, *o.WatermarkSymbolID)
		}
	}
	if len(ids) == 0 {
		return
	}
	gestures, err := s.photoProof.Gestures(ctx, ids)
	if err != nil {
		log.Printf("[OrderService] cannot load photo proof gestures: %v", err)
		return
	}
	for _, o := range orders {
		if o == nil || !o.PhotoRequired || !executorOf(o, viewer.userID) || o.WatermarkSymbolID == nil {
			continue
		}
		proof := &repository.OrderPhotoProof{Required: true, Nonce: base64.StdEncoding.EncodeToString(o.ProofKey)}
		if g, ok := gestures[*o.WatermarkSymbolID]; ok {
			proof.Gesture = &repository.OrderGesture{
				Code: g.Code, Number: g.Number, Title: g.Title, Description: g.Description,
				HintImageURL: g.HintImageURL, FitsInSelfie: g.FitsInSelfie,
			}
		}
		o.PhotoProof = proof
	}
}
