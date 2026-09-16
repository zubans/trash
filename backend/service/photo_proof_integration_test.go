package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
)

// photoPeriod ставит роли период фото-подтверждения напрямую.
func photoPeriod(t *testing.T, db *sql.DB, userID uuid.UUID, role string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO user_penalty_status (user_id, role, active_points, photo_required_until)
		VALUES ($1, $2, 2, now() + interval '30 days')
		ON CONFLICT (user_id, role) DO UPDATE SET photo_required_until = EXCLUDED.photo_required_until`,
		userID, role); err != nil {
		t.Fatalf("photo period: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM user_penalty_status WHERE user_id = $1`, userID) })
}

// assignedOrder — заказ в поиске, назначенный исполнителю тем же шагом, что и
// при взятии: назначение и решение о фото в одной транзакции.
func assignedOrder(t *testing.T, srv *OrderService, db *sql.DB, customerID, variantID, executorID uuid.UUID) *repository.Order {
	t.Helper()
	ctx := context.Background()
	lat, lon := 55.7558, 37.6173
	order, err := srv.CreateOrder(ctx, customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := srv.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := srv.orderRepo.Assign(ctx, tx, order.ID, executorID); err != nil {
			return err
		}
		return srv.requirePhotoProofTx(ctx, tx, order, executorID)
	}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	loaded, err := srv.orderRepo.GetOrderByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return loaded
}

func TestPhotoProofRequirementIntegration(t *testing.T) {
	db := openTestDB(t)
	penalties := NewPenaltyService(repository.NewPenaltyRepository(db), repository.NewSettingsRepository(db), nil)
	proof := photoproof.NewService(photoproof.NewSymbolRepository(db))
	srv := newIntegrationOrderService(db).WithPenalties(penalties).WithPhotoProof(proof)
	ctx := context.Background()

	customerID, variantID := seedCustomer(t, db, money.FromRubles(10000))

	t.Run("no period, no requirement", func(t *testing.T) {
		executorID := seedExecutor(t, db)
		order := assignedOrder(t, srv, db, customerID, variantID, executorID)
		if order.PhotoRequired || order.WatermarkSymbolID != nil || order.ProofKey != nil {
			t.Fatalf("order without a period requires a photo: %+v", order)
		}
		if err := srv.ExecuteOrderAt(ctx, order.ID, executorID, nil); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})

	t.Run("executor period", func(t *testing.T) {
		executorID := seedExecutor(t, db)
		photoPeriod(t, db, executorID, repository.RoleExecutor)
		order := assignedOrder(t, srv, db, customerID, variantID, executorID)
		if !order.PhotoRequired || order.WatermarkSymbolID == nil || len(order.ProofKey) != 32 {
			t.Fatalf("requirement not fixed on the order: %+v", order)
		}

		// Исполнитель получает жест и служебные данные, заказчик — ничего.
		asExecutor := *order
		srv.presentOrders(ctx, executorViewer(executorID), []*repository.Order{&asExecutor})
		if asExecutor.PhotoProof == nil || asExecutor.PhotoProof.Gesture == nil || asExecutor.PhotoProof.Gesture.Title == "" {
			t.Fatalf("executor view: %+v", asExecutor.PhotoProof)
		}
		if key, err := base64.StdEncoding.DecodeString(asExecutor.PhotoProof.Nonce); err != nil || len(key) != 32 {
			t.Fatalf("nonce: %q %v", asExecutor.PhotoProof.Nonce, err)
		}
		asCustomer := *order
		srv.presentOrders(ctx, customerViewer(customerID), []*repository.Order{&asCustomer})
		if asCustomer.PhotoProof != nil {
			t.Fatalf("the customer sees the photo proof data: %+v", asCustomer.PhotoProof)
		}

		// Без снимка места заказа отметка «Исполнил» не проходит.
		deviceAt := time.Now().Add(-20 * time.Minute).Truncate(time.Second)
		if err := srv.ExecuteOrderAt(ctx, order.ID, executorID, &deviceAt); !errors.Is(err, ErrPhotoProofRequired) {
			t.Fatalf("execute without a photo: %v", err)
		}
		// Селфи обязательного снимка не заменяет.
		insertProof(t, db, order, executorID, photoproof.KindSelfie)
		if err := srv.ExecuteOrderAt(ctx, order.ID, executorID, &deviceAt); !errors.Is(err, ErrPhotoProofRequired) {
			t.Fatalf("execute with a selfie only: %v", err)
		}
		insertProof(t, db, order, executorID, photoproof.KindArea)
		if err := srv.ExecuteOrderAt(ctx, order.ID, executorID, &deviceAt); err != nil {
			t.Fatalf("execute with the area photo: %v", err)
		}
		executed, err := srv.orderRepo.GetOrderByID(ctx, order.ID)
		if err != nil {
			t.Fatal(err)
		}
		if executed.ExecutedAt == nil || executed.ExecutedAtDevice == nil || !executed.ExecutedAtDevice.Equal(deviceAt) {
			t.Fatalf("execution times: server %v device %v", executed.ExecutedAt, executed.ExecutedAtDevice)
		}
	})

	t.Run("customer period", func(t *testing.T) {
		periodCustomer, periodVariant := seedCustomer(t, db, money.FromRubles(10000))
		photoPeriod(t, db, periodCustomer, repository.RoleCustomer)
		executorID := seedExecutor(t, db)
		order := assignedOrder(t, srv, db, periodCustomer, periodVariant, executorID)
		if !order.PhotoRequired {
			t.Fatal("the customer's period did not require a photo")
		}
	})
}

func insertProof(t *testing.T, db *sql.DB, order *repository.Order, executorID uuid.UUID, kind string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO order_photo_proofs (order_id, executor_id, kind, camera, symbol_id, client_key, file_url,
			file_sha256, file_size, device_taken_at, seal_status, mark_status)
		VALUES ($1, $2, $3, 'REAR', $4, $5, '/uploads/proof/test.jpg', repeat('0', 64), 1, now(), 'VALID', 'FOUND')`,
		order.ID, executorID, kind, *order.WatermarkSymbolID, uuid.NewString()); err != nil {
		t.Fatalf("insert proof: %v", err)
	}
}
