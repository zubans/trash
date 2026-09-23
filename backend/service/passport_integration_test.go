package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/passport"
	"healthlogin/backend/repository"
)

// jpegBytes — начало JPEG: по нему http.DetectContentType узнаёт фото.
var jpegBytes = append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), bytes.Repeat([]byte{0x42}, 64)...)

type fixedVerificationOrders struct {
	customer, executor uuid.UUID
}

func (o *fixedVerificationOrders) PassportCustomer(ctx context.Context, orderID, executorID uuid.UUID) (uuid.UUID, error) {
	if executorID != o.executor {
		return uuid.Nil, errors.New("заказ назначен не вам")
	}
	return o.customer, nil
}

func passportCode(err error) string {
	var pe *PassportError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func newPassportService(t *testing.T, withKey bool) (*PassportService, repository.PassportRepository, string, func(consent bool) *repository.User) {
	t.Helper()
	db := openTestDB(t)
	var cipher *passport.Cipher
	if withKey {
		key := make([]byte, 32)
		_, _ = rand.Read(key)
		var err error
		if cipher, err = passport.NewCipher(base64.StdEncoding.EncodeToString(key), 1); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	users := repository.New(db)
	repo := repository.NewPassportRepository(db)
	srv := NewPassportService(repo, users, cipher, dir, &orderMockSettingsRepo{settings: map[string]string{SettingPDConsentVersion: "1"}})
	newUser := func(consent bool) *repository.User {
		t.Helper()
		id := uuid.New()
		var version interface{}
		if consent {
			version = 1
		}
		if _, err := db.Exec(
			`INSERT INTO users (id, role, phone, password, balance, status, pd_consent_version) VALUES ($1, 'CUSTOMER', $2, 'x', 0, 'ACTIVE', $3)`,
			id, "+7995"+id.String()[:7], version); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		t.Cleanup(func() {
			_, _ = db.Exec(`DELETE FROM passport_access_log WHERE user_id = $1 OR viewer_id = $1`, id)
			_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id)
		})
		u, err := users.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	return srv, repo, dir, newUser
}

// Владелец: без согласия паспорт не хранится; с ним — хранится зашифрованным,
// показывается маской, а после отметки «проверенный» правится только через
// поддержку.
func TestPassportOwnerFlowIntegration(t *testing.T) {
	ctx := context.Background()
	srv, repo, dir, newUser := newPassportService(t, true)
	data := PassportData{Series: "45 10", Number: "123 456", IssuedAt: "2015-06-01", DivisionCode: "770-001"}

	noConsent := newUser(false)
	if err := srv.SaveMine(ctx, noConsent, data); passportCode(err) != PassportErrConsentRequired {
		t.Fatalf("save without consent: %v", err)
	}

	owner := newUser(true)
	if err := srv.SaveMine(ctx, owner, PassportData{Series: "45", Number: "1", IssuedAt: "2999-01-01"}); passportCode(err) != PassportErrValidation {
		t.Fatalf("bad passport: %v", err)
	}
	if err := srv.SaveMinePhoto(ctx, owner, jpegBytes); passportCode(err) != PassportErrRequired {
		t.Fatalf("photo before the data: %v", err)
	}
	if err := srv.SaveMine(ctx, owner, data); err != nil {
		t.Fatalf("save: %v", err)
	}
	rec, err := repo.Get(ctx, nil, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rec.DataEnc, []byte("123456")) || rec.Source != repository.PassportSourceOwner {
		t.Fatalf("stored passport: source %s, the number readable = %v", rec.Source, bytes.Contains(rec.DataEnc, []byte("123456")))
	}
	mask, err := srv.Mine(ctx, owner)
	if err != nil || mask.Series != "45 **" || mask.Number != "****56" || mask.IssuedYr != "2015" || mask.HasPhoto {
		t.Fatalf("mask: %+v, %v", mask, err)
	}

	if err := srv.SaveMinePhoto(ctx, owner, []byte("<html>not a photo</html>")); passportCode(err) != PassportErrBadPhoto {
		t.Fatalf("a non-image photo: %v", err)
	}
	if err := srv.SaveMinePhoto(ctx, owner, jpegBytes); err != nil {
		t.Fatalf("photo: %v", err)
	}
	if err := srv.SaveMinePhoto(ctx, owner, jpegBytes); err != nil {
		t.Fatalf("second photo: %v", err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("the replaced photo was not removed: %d files", len(files))
	}
	raw, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if bytes.Contains(raw, []byte("JFIF")) {
		t.Fatal("the photo is stored in the clear")
	}

	// «Проверенный»: только с фото, причина обязательна; после отметки владелец
	// паспорт не правит.
	admin := newUser(true)
	if err := srv.SetChecked(ctx, admin.ID, owner.ID, true, ""); passportCode(err) != PassportErrValidation {
		t.Fatalf("check without a reason: %v", err)
	}
	bare := newUser(true)
	if err := srv.SaveMine(ctx, bare, data); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetChecked(ctx, admin.ID, bare.ID, true, "обратился в поддержку"); passportCode(err) != PassportErrRequired {
		t.Fatalf("check without a photo: %v", err)
	}
	if err := srv.SetChecked(ctx, admin.ID, owner.ID, true, "обратился в поддержку"); err != nil {
		t.Fatalf("check: %v", err)
	}
	checked, _ := repository.New(openTestDB(t)).FindByID(ctx, owner.ID)
	if !checked.Checked {
		t.Fatal("the user is not checked")
	}
	if err := srv.SaveMine(ctx, checked, data); passportCode(err) != PassportErrLocked {
		t.Fatalf("a checked user edited the passport: %v", err)
	}
}

// Админка: каждый просмотр — строка журнала; удаление снимает «проверенный» и
// файл фото.
func TestPassportAdminFlowIntegration(t *testing.T) {
	ctx := context.Background()
	srv, _, dir, newUser := newPassportService(t, true)
	db := openTestDB(t)
	owner, admin := newUser(true), newUser(true)

	if err := srv.AdminSave(ctx, admin.ID, owner.ID, PassportData{Series: "4510", Number: "654321", IssuedAt: "2020-02-02", IssuedBy: "ОВД"}); err != nil {
		t.Fatalf("admin save: %v", err)
	}
	if err := srv.AdminSavePhoto(ctx, admin.ID, owner.ID, jpegBytes); err != nil {
		t.Fatalf("admin photo: %v", err)
	}
	full, err := srv.AdminView(ctx, admin.ID, owner.ID)
	if err != nil || full.Number != "654321" || full.IssuedBy != "ОВД" || full.Source != repository.PassportSourceAdmin || !full.HasPhoto {
		t.Fatalf("admin view: %+v, %v", full, err)
	}
	photo, err := srv.AdminPhoto(ctx, admin.ID, owner.ID)
	if err != nil || !bytes.Equal(photo, jpegBytes) {
		t.Fatalf("admin photo: %v", err)
	}
	var views int
	if err := db.QueryRow(`SELECT COUNT(*) FROM passport_access_log WHERE user_id = $1 AND viewer_id = $2 AND action IN ('VIEW', 'VIEW_PHOTO')`,
		owner.ID, admin.ID).Scan(&views); err != nil || views != 2 {
		t.Fatalf("access log has %d views, want 2 (%v)", views, err)
	}

	if err := srv.SetChecked(ctx, admin.ID, owner.ID, true, "проверен"); err != nil {
		t.Fatal(err)
	}
	if err := srv.AdminDelete(ctx, admin.ID, owner.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, _ := repository.New(db).FindByID(ctx, owner.ID)
	if after.Checked {
		t.Error("deleting the passport left the user checked")
	}
	if files, _ := os.ReadDir(dir); len(files) != 0 {
		t.Errorf("the photo file survived the deletion: %d files", len(files))
	}
	if err := srv.AdminDelete(ctx, admin.ID, owner.ID); passportCode(err) != PassportErrNotFound {
		t.Errorf("second delete: %v", err)
	}

	// Паспорт пользователя без согласия админ не вносит.
	stranger := newUser(false)
	if err := srv.AdminSave(ctx, admin.ID, stranger.ID, PassportData{Series: "4510", Number: "654321", IssuedAt: "2020-02-02"}); passportCode(err) != PassportErrConsentRequired {
		t.Errorf("admin saved a passport without consent: %v", err)
	}
}

// Модератор вносит паспорт заказчика по своему заказу верификации.
func TestPassportFromVerificationIntegration(t *testing.T) {
	ctx := context.Background()
	srv, repo, _, newUser := newPassportService(t, true)
	customer, moderator, stranger := newUser(true), newUser(true), newUser(true)
	srv.WithVerification(&fixedVerificationOrders{customer: customer.ID, executor: moderator.ID})
	order := uuid.New()

	data := PassportData{Series: "4510", Number: "111222", IssuedAt: "2018-03-03"}
	if err := srv.SaveFromVerification(ctx, order, stranger.ID, data); passportCode(err) != PassportErrValidation {
		t.Fatalf("a stranger entered the passport: %v", err)
	}
	if err := srv.SaveFromVerification(ctx, order, moderator.ID, data); err != nil {
		t.Fatalf("verification save: %v", err)
	}
	if err := srv.SavePhotoFromVerification(ctx, order, moderator.ID, jpegBytes); err != nil {
		t.Fatalf("verification photo: %v", err)
	}
	rec, err := repo.Get(ctx, nil, customer.ID)
	if err != nil || rec.Source != repository.PassportSourceVerification || rec.EnteredBy == nil || *rec.EnteredBy != moderator.ID || rec.PhotoPath == nil {
		t.Fatalf("verification passport: %+v, %v", rec, err)
	}
}

// Без ключа паспорта не принимаются вовсе — а не хранятся открытыми.
func TestPassportWithoutKeyIntegration(t *testing.T) {
	srv, _, _, newUser := newPassportService(t, false)
	owner := newUser(true)
	if err := srv.SaveMine(context.Background(), owner, PassportData{Series: "4510", Number: "123456", IssuedAt: "2015-06-01"}); passportCode(err) != PassportErrNoKey {
		t.Fatalf("save without a key: %v", err)
	}
}
