package photoproof

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// proofKeyBytes — длина служебного ключа проверки снимка, выдаваемого заказу.
const proofKeyBytes = 32

// ErrOrderNotAwaitingProof — заказ не требует фото или уже получил требование.
var ErrOrderNotAwaitingProof = errors.New("заказ уже получил требование фото-подтверждения")

// RequireForOrderTx помечает заказ требующим фото-подтверждения: выбирает
// случайный действующий жест и выдаёт служебный ключ проверки. Вызывается при
// взятии заказа, в транзакции назначения: заказ без жеста при требовании фото
// закрыть было бы нечем.
func (s *Service) RequireForOrderTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	symbol, err := s.PickSymbol(ctx, tx)
	if err != nil {
		return err
	}
	key := make([]byte, proofKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
        UPDATE orders SET photo_required = TRUE, watermark_symbol_id = $2, proof_key = $3
        WHERE id = $1 AND photo_required = FALSE
    `, orderID, symbol.ID, key)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrOrderNotAwaitingProof
	}
	return nil
}

// HasRequiredPhotoTx сообщает, загружен ли обязательный снимок заказа — место
// заказа с жестом. Селфи с заказчиком необязательно и на ответ не влияет.
func (s *Service) HasRequiredPhotoTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) (bool, error) {
	var has bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM order_photo_proofs WHERE order_id = $1 AND kind = $2)`,
		orderID, KindArea).Scan(&has)
	return has, err
}

// Gestures отдаёт жесты по id — в том числе удалённые: заказ, получивший жест
// до удаления, показывает его по-прежнему.
func (s *Service) Gestures(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]Symbol, error) {
	out := make(map[uuid.UUID]Symbol, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	return out, s.symbols.ByIDs(ctx, ids, out)
}

// Виды снимков (order_photo_proofs.kind).
const (
	// KindArea — место заказа с жестом; обязательный.
	KindArea = "AREA"
	// KindSelfie — селфи с заказчиком, если он согласен; необязательный.
	KindSelfie = "SELFIE"
)

func (r *symbolRepo) ByIDs(ctx context.Context, ids []uuid.UUID, into map[uuid.UUID]Symbol) error {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+symbolColumns+` FROM watermark_symbols WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		symbol, err := scanSymbol(rows)
		if err != nil {
			return err
		}
		into[symbol.ID] = *symbol
	}
	return rows.Err()
}
