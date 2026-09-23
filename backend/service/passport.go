package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/passport"
	"healthlogin/backend/repository"
)

// SettingPDConsentVersion — текущая редакция согласия на обработку
// персональных данных.
const SettingPDConsentVersion = "pd_consent_version"

// maxPassportPhotoBytes — потолок фото документа.
const maxPassportPhotoBytes = 10 << 20

// PassportError — отказ с кодом, который клиент переводит, и полями формы.
type PassportError struct {
	Status  int               `json:"-"`
	Code    string            `json:"error"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *PassportError) Error() string { return e.Code + ": " + e.Message }

func passportErr(status int, code, message string) *PassportError {
	return &PassportError{Status: status, Code: code, Message: message}
}

// Коды отказов.
const (
	PassportErrNoKey           = "passport_unavailable"
	PassportErrConsentRequired = "pd_consent_required"
	PassportErrLocked          = "passport_locked"
	PassportErrRequired        = "passport_required"
	PassportErrNotFound        = "not_found"
	PassportErrValidation      = "validation"
	PassportErrBadPhoto        = "bad_photo"
)

// ErrPassportRequired — сверку по услуге с require_passport нельзя принять,
// пока паспорт заказчика с фото не на сервере.
var ErrPassportRequired = errors.New("сначала отправьте паспорт заказчика с фото")

// PassportData — то, что записано в паспорте. Обязательны серия, номер и дата
// выдачи; «кем выдан» и код подразделения — по желанию
// (implementation_plan_delivery_passport.md §3.1).
type PassportData struct {
	Series       string `json:"series"`
	Number       string `json:"number"`
	IssuedAt     string `json:"issued_at"`
	IssuedBy     string `json:"issued_by,omitempty"`
	DivisionCode string `json:"division_code,omitempty"`
}

var (
	passportSeries   = regexp.MustCompile(`^\d{4}$`)
	passportNumber   = regexp.MustCompile(`^\d{6}$`)
	passportDivision = regexp.MustCompile(`^\d{3}-\d{3}$`)
)

func digitsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// normalize убирает пробелы, которыми люди разбивают серию и номер, и
// проверяет поля. Ошибки — по полям, чтобы форма показала их у поля.
func (d *PassportData) normalize(now time.Time) map[string]string {
	d.Series = digitsOnly(d.Series)
	d.Number = digitsOnly(d.Number)
	d.IssuedAt = strings.TrimSpace(d.IssuedAt)
	d.IssuedBy = strings.TrimSpace(d.IssuedBy)
	d.DivisionCode = strings.TrimSpace(d.DivisionCode)
	fields := map[string]string{}
	if !passportSeries.MatchString(d.Series) {
		fields["series"] = "Серия — 4 цифры"
	}
	if !passportNumber.MatchString(d.Number) {
		fields["number"] = "Номер — 6 цифр"
	}
	if issued, err := time.Parse("2006-01-02", d.IssuedAt); err != nil {
		fields["issued_at"] = "Дата выдачи обязательна"
	} else if issued.After(now) {
		fields["issued_at"] = "Дата выдачи не может быть в будущем"
	}
	if d.DivisionCode != "" && !passportDivision.MatchString(d.DivisionCode) {
		fields["division_code"] = "Код подразделения — в виде 123-456"
	}
	return fields
}

// PassportMask — паспорт, каким его видит владелец: без полных цифр.
type PassportMask struct {
	Exists    bool       `json:"exists"`
	Series    string     `json:"series,omitempty"`
	Number    string     `json:"number,omitempty"`
	IssuedYr  string     `json:"issued_year,omitempty"`
	HasPhoto  bool       `json:"has_photo"`
	Source    string     `json:"source,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// Locked — пользователь «проверенный»: правка паспорта — через поддержку,
	// иначе проверенный документ можно было бы подменить.
	Locked bool `json:"locked"`
}

// PassportFull — паспорт целиком, для права passports.view.
type PassportFull struct {
	PassportData
	HasPhoto  bool       `json:"has_photo"`
	Source    string     `json:"source"`
	EnteredBy *uuid.UUID `json:"entered_by,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// PassportService — паспорт, статус «проверенный» и согласие на обработку
// персональных данных (implementation_plan_delivery_passport.md §2–§4).
type PassportService struct {
	repo     repository.PassportRepository
	users    repository.UserRepository
	cipher   *passport.Cipher
	photos   passport.Photos
	settings repository.SettingsRepository
	// orders сверяет, что паспорт вносит исполнитель заказа верификации.
	orders verificationOrders
	now    func() time.Time
}

// verificationOrders — то, что нужно паспорту от диспетчера поведений:
// заказчик заказа, по которому исполнитель вносит паспорт.
type verificationOrders interface {
	PassportCustomer(ctx context.Context, orderID, executorID uuid.UUID) (uuid.UUID, error)
}

// NewPassportService собирает сервис. cipher может быть nil: сервер стартует,
// а приём паспортов отвечает 503.
func NewPassportService(repo repository.PassportRepository, users repository.UserRepository,
	cipher *passport.Cipher, photosDir string, settings repository.SettingsRepository) *PassportService {
	return &PassportService{repo: repo, users: users, cipher: cipher,
		photos: passport.Photos{Dir: photosDir, Cipher: cipher}, settings: settings, now: time.Now}
}

// WithVerification подключает заказы верификации: без них модератор паспорт
// не вносит.
func (s *PassportService) WithVerification(orders verificationOrders) *PassportService {
	s.orders = orders
	return s
}

// ConsentVersion — текущая редакция согласия.
func (s *PassportService) ConsentVersion(ctx context.Context) int {
	return settingInt(ctx, s.settings, SettingPDConsentVersion, 1)
}

// ConsentRequired — пользователь не принимал текущую редакцию согласия.
func (s *PassportService) ConsentRequired(ctx context.Context, user *repository.User) bool {
	return user.PDConsentVersion == nil || *user.PDConsentVersion < s.ConsentVersion(ctx)
}

// AcceptConsent записывает согласие на текущую редакцию.
func (s *PassportService) AcceptConsent(ctx context.Context, user *repository.User) error {
	version := s.ConsentVersion(ctx)
	if err := s.repo.AcceptPDConsent(ctx, user.ID, version); err != nil {
		return err
	}
	log.Printf("[AUDIT] user %s accepted personal data consent v%d", user.ID, version)
	return nil
}

func (s *PassportService) requireKey() error {
	if s.cipher == nil {
		return passportErr(http.StatusServiceUnavailable, PassportErrNoKey, "Приём паспортов сейчас недоступен")
	}
	return nil
}

// requireConsent — паспорт без согласия его владельца не хранится.
func (s *PassportService) requireConsent(ctx context.Context, owner *repository.User, self bool) error {
	if !s.ConsentRequired(ctx, owner) {
		return nil
	}
	if self {
		return passportErr(http.StatusForbidden, PassportErrConsentRequired,
			"Примите согласие на обработку персональных данных")
	}
	return passportErr(http.StatusConflict, PassportErrConsentRequired,
		"Пользователь не дал согласия на обработку персональных данных — попросите его принять согласие в приложении")
}

func (s *PassportService) owner(ctx context.Context, userID uuid.UUID) (*repository.User, error) {
	u, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, passportErr(http.StatusNotFound, PassportErrNotFound, "Пользователь не найден")
	}
	return u, err
}

// save — общая запись данных: проверка, шифрование, журнал.
func (s *PassportService) save(ctx context.Context, owner *repository.User, actor uuid.UUID, source string, data PassportData) error {
	if err := s.requireKey(); err != nil {
		return err
	}
	if err := s.validate(&data); err != nil {
		return err
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return err
	}
	sealed, err := s.cipher.Seal(plain)
	if err != nil {
		return err
	}
	var enteredBy *uuid.UUID
	if actor != owner.ID {
		enteredBy = &actor
	}
	return s.repo.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.repo.Save(ctx, tx, &repository.PassportRecord{
			UserID: owner.ID, DataEnc: sealed, Source: source, EnteredBy: enteredBy, KeyVersion: s.cipher.Version(),
		}); err != nil {
			return err
		}
		return s.repo.LogAccess(ctx, tx, owner.ID, actor, repository.PassportActionWrite)
	})
}

// savePhoto — общая запись фото: тип по содержимому, шифрованный файл, старый
// файл удаляется после коммита.
func (s *PassportService) savePhoto(ctx context.Context, owner *repository.User, actor uuid.UUID, photo []byte) error {
	if err := s.requireKey(); err != nil {
		return err
	}
	if len(photo) == 0 || len(photo) > maxPassportPhotoBytes {
		return passportErr(http.StatusBadRequest, PassportErrBadPhoto, "Фото — до 10 МБ")
	}
	switch http.DetectContentType(photo) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return passportErr(http.StatusBadRequest, PassportErrBadPhoto, "Фото — JPEG, PNG или WebP")
	}
	if _, err := s.repo.Get(ctx, nil, owner.ID); errors.Is(err, repository.ErrPassportNotFound) {
		return passportErr(http.StatusConflict, PassportErrRequired, "Сначала заполните данные паспорта")
	} else if err != nil {
		return err
	}
	name := owner.ID.String() + "-" + uuid.New().String() + ".bin"
	if err := s.photos.Save(name, photo); err != nil {
		return err
	}
	var previous *string
	err := s.repo.RunInTx(ctx, func(tx *sql.Tx) error {
		var err error
		if previous, err = s.repo.SetPhoto(ctx, tx, owner.ID, name); err != nil {
			return err
		}
		return s.repo.LogAccess(ctx, tx, owner.ID, actor, repository.PassportActionWrite)
	})
	if err != nil {
		_ = s.photos.Remove(name)
		return err
	}
	if previous != nil {
		if err := s.photos.Remove(*previous); err != nil {
			log.Printf("[passport] cannot remove the previous photo of %s: %v", owner.ID, err)
		}
	}
	return nil
}

// Mine — паспорт владельца маской.
func (s *PassportService) Mine(ctx context.Context, user *repository.User) (*PassportMask, error) {
	mask := &PassportMask{Locked: user.Checked}
	rec, err := s.repo.Get(ctx, nil, user.ID)
	if errors.Is(err, repository.ErrPassportNotFound) {
		return mask, nil
	}
	if err != nil {
		return nil, err
	}
	mask.Exists, mask.HasPhoto, mask.Source, mask.UpdatedAt = true, rec.PhotoPath != nil, rec.Source, &rec.UpdatedAt
	if data, err := s.open(rec); err == nil {
		mask.Series = data.Series[:2] + " **"
		mask.Number = "****" + data.Number[len(data.Number)-2:]
		if len(data.IssuedAt) >= 4 {
			mask.IssuedYr = data.IssuedAt[:4]
		}
	}
	return mask, nil
}

func (s *PassportService) open(rec *repository.PassportRecord) (*PassportData, error) {
	plain, err := s.cipher.Open(rec.DataEnc)
	if err != nil {
		return nil, err
	}
	var data PassportData
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, err
	}
	if len(data.Series) < 2 || len(data.Number) < 2 {
		return nil, errors.New("passport data is malformed")
	}
	return &data, nil
}

// ownerMayEdit — владелец правит паспорт, пока не «проверенный».
func (s *PassportService) ownerMayEdit(ctx context.Context, user *repository.User) error {
	if err := s.requireConsent(ctx, user, true); err != nil {
		return err
	}
	if user.Checked {
		return passportErr(http.StatusConflict, PassportErrLocked,
			"Паспорт проверен — изменить его можно через поддержку")
	}
	return nil
}

// SaveMine — владелец заполняет паспорт сам: при регистрации или в профиле.
func (s *PassportService) SaveMine(ctx context.Context, user *repository.User, data PassportData) error {
	if err := s.ownerMayEdit(ctx, user); err != nil {
		return err
	}
	return s.save(ctx, user, user.ID, repository.PassportSourceOwner, data)
}

// ValidateData проверяет паспорт, ничего не сохраняя: регистрация проверяет
// его до создания учётной записи, чтобы не создать её с отказом в паспорте.
func (s *PassportService) ValidateData(data PassportData) error {
	return s.validate(&data)
}

// validate приводит поля к виду, в котором они хранятся, и проверяет их.
func (s *PassportService) validate(data *PassportData) error {
	if fields := data.normalize(s.now()); len(fields) > 0 {
		return &PassportError{Status: http.StatusUnprocessableEntity, Code: PassportErrValidation,
			Message: "Проверьте поля паспорта", Fields: fields}
	}
	return nil
}

// SaveAtRegistration — паспорт из формы регистрации, сразу после создания
// учётной записи.
func (s *PassportService) SaveAtRegistration(ctx context.Context, userID uuid.UUID, data PassportData) error {
	owner, err := s.owner(ctx, userID)
	if err != nil {
		return err
	}
	return s.SaveMine(ctx, owner, data)
}

// SaveMinePhoto — владелец прикладывает фото документа.
func (s *PassportService) SaveMinePhoto(ctx context.Context, user *repository.User, photo []byte) error {
	if err := s.ownerMayEdit(ctx, user); err != nil {
		return err
	}
	return s.savePhoto(ctx, user, user.ID, photo)
}

// PassportStatus — есть ли паспорт и фото и стоит ли «проверенный». Без
// паспортных данных, поэтому в журнал доступа не пишется.
type PassportStatus struct {
	Exists    bool       `json:"exists"`
	HasPhoto  bool       `json:"has_photo"`
	Source    string     `json:"source,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Checked   bool       `json:"is_checked"`
	// ConsentGiven — пользователь принял согласие на обработку персональных
	// данных: без него паспорт не вносится.
	ConsentGiven bool `json:"consent_given"`
}

// Status — состояние паспорта для карточки пользователя в админке.
func (s *PassportService) Status(ctx context.Context, userID uuid.UUID) (*PassportStatus, error) {
	owner, err := s.owner(ctx, userID)
	if err != nil {
		return nil, err
	}
	status := &PassportStatus{Checked: owner.Checked, ConsentGiven: !s.ConsentRequired(ctx, owner)}
	rec, err := s.repo.Get(ctx, nil, userID)
	if errors.Is(err, repository.ErrPassportNotFound) {
		return status, nil
	}
	if err != nil {
		return nil, err
	}
	status.Exists, status.HasPhoto, status.Source, status.UpdatedAt = true, rec.PhotoPath != nil, rec.Source, &rec.UpdatedAt
	return status, nil
}

// AdminView — паспорт целиком; каждый показ пишется в журнал.
func (s *PassportService) AdminView(ctx context.Context, adminID, userID uuid.UUID) (*PassportFull, error) {
	if err := s.requireKey(); err != nil {
		return nil, err
	}
	rec, err := s.repo.Get(ctx, nil, userID)
	if errors.Is(err, repository.ErrPassportNotFound) {
		return nil, passportErr(http.StatusNotFound, PassportErrNotFound, "Паспорта нет")
	}
	if err != nil {
		return nil, err
	}
	data, err := s.open(rec)
	if err != nil {
		return nil, fmt.Errorf("open passport of %s: %w", userID, err)
	}
	if err := s.repo.LogAccess(ctx, nil, userID, adminID, repository.PassportActionView); err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] %s viewed the passport of %s", adminID, userID)
	return &PassportFull{PassportData: *data, HasPhoto: rec.PhotoPath != nil, Source: rec.Source,
		EnteredBy: rec.EnteredBy, UpdatedAt: rec.UpdatedAt}, nil
}

// AdminPhoto — фото документа; показ пишется в журнал.
func (s *PassportService) AdminPhoto(ctx context.Context, adminID, userID uuid.UUID) ([]byte, error) {
	if err := s.requireKey(); err != nil {
		return nil, err
	}
	rec, err := s.repo.Get(ctx, nil, userID)
	if errors.Is(err, repository.ErrPassportNotFound) || (err == nil && rec.PhotoPath == nil) {
		return nil, passportErr(http.StatusNotFound, PassportErrNotFound, "Фото паспорта нет")
	}
	if err != nil {
		return nil, err
	}
	photo, err := s.photos.Read(*rec.PhotoPath)
	if err != nil {
		return nil, fmt.Errorf("read passport photo of %s: %w", userID, err)
	}
	if err := s.repo.LogAccess(ctx, nil, userID, adminID, repository.PassportActionViewPhoto); err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] %s viewed the passport photo of %s", adminID, userID)
	return photo, nil
}

// AdminSave — администратор вносит или исправляет паспорт.
func (s *PassportService) AdminSave(ctx context.Context, adminID, userID uuid.UUID, data PassportData) error {
	owner, err := s.owner(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.requireConsent(ctx, owner, false); err != nil {
		return err
	}
	if err := s.save(ctx, owner, adminID, repository.PassportSourceAdmin, data); err != nil {
		return err
	}
	log.Printf("[AUDIT] admin %s saved the passport of %s", adminID, userID)
	return nil
}

// AdminSavePhoto — администратор прикладывает фото документа.
func (s *PassportService) AdminSavePhoto(ctx context.Context, adminID, userID uuid.UUID, photo []byte) error {
	owner, err := s.owner(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.requireConsent(ctx, owner, false); err != nil {
		return err
	}
	if err := s.savePhoto(ctx, owner, adminID, photo); err != nil {
		return err
	}
	log.Printf("[AUDIT] admin %s saved the passport photo of %s", adminID, userID)
	return nil
}

// AdminDelete удаляет паспорт. Без паспорта «проверенным» быть нельзя, поэтому
// отметка снимается в той же транзакции.
func (s *PassportService) AdminDelete(ctx context.Context, adminID, userID uuid.UUID) error {
	var deleted *repository.PassportRecord
	err := s.repo.RunInTx(ctx, func(tx *sql.Tx) error {
		var err error
		if deleted, err = s.repo.Delete(ctx, tx, userID); err != nil {
			return err
		}
		if err := s.repo.SetChecked(ctx, tx, userID, false, adminID); err != nil {
			return err
		}
		return s.repo.LogAccess(ctx, tx, userID, adminID, repository.PassportActionDelete)
	})
	if errors.Is(err, repository.ErrPassportNotFound) {
		return passportErr(http.StatusNotFound, PassportErrNotFound, "Паспорта нет")
	}
	if err != nil {
		return err
	}
	if deleted.PhotoPath != nil {
		if err := s.photos.Remove(*deleted.PhotoPath); err != nil {
			log.Printf("[passport] cannot remove the photo of %s: %v", userID, err)
		}
	}
	log.Printf("[AUDIT] admin %s deleted the passport of %s", adminID, userID)
	return nil
}

// SetChecked ставит или снимает «проверенный». Поставить можно только при
// паспорте с фото; причина обязательна в обе стороны.
func (s *PassportService) SetChecked(ctx context.Context, adminID, userID uuid.UUID, checked bool, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &PassportError{Status: http.StatusUnprocessableEntity, Code: PassportErrValidation,
			Message: "Укажите причину", Fields: map[string]string{"reason": "Укажите причину"}}
	}
	if _, err := s.owner(ctx, userID); err != nil {
		return err
	}
	if checked {
		rec, err := s.repo.Get(ctx, nil, userID)
		if errors.Is(err, repository.ErrPassportNotFound) || (err == nil && rec.PhotoPath == nil) {
			return passportErr(http.StatusConflict, PassportErrRequired,
				"Отметить проверенным можно только при паспорте с фото")
		}
		if err != nil {
			return err
		}
	}
	if err := s.repo.SetChecked(ctx, nil, userID, checked, adminID); err != nil {
		return err
	}
	log.Printf("[AUDIT] %s set checked of %s to %t: %s", adminID, userID, checked, reason)
	return nil
}

// verificationOwner — заказчик заказа верификации, по которому исполнитель
// вносит паспорт.
func (s *PassportService) verificationOwner(ctx context.Context, orderID, executorID uuid.UUID) (*repository.User, error) {
	if s.orders == nil {
		return nil, passportErr(http.StatusBadRequest, PassportErrValidation, "Эта услуга не принимает паспорт")
	}
	customerID, err := s.orders.PassportCustomer(ctx, orderID, executorID)
	if err != nil {
		return nil, passportErr(http.StatusBadRequest, PassportErrValidation, err.Error())
	}
	owner, err := s.owner(ctx, customerID)
	if err != nil {
		return nil, err
	}
	return owner, s.requireConsent(ctx, owner, false)
}

// SaveFromVerification — модератор вносит паспорт заказчика по заказу
// верификации (implementation_plan_delivery_passport.md §3.2).
func (s *PassportService) SaveFromVerification(ctx context.Context, orderID, executorID uuid.UUID, data PassportData) error {
	owner, err := s.verificationOwner(ctx, orderID, executorID)
	if err != nil {
		return err
	}
	return s.save(ctx, owner, executorID, repository.PassportSourceVerification, data)
}

// SavePhotoFromVerification — модератор фотографирует документ заказчика.
func (s *PassportService) SavePhotoFromVerification(ctx context.Context, orderID, executorID uuid.UUID, photo []byte) error {
	owner, err := s.verificationOwner(ctx, orderID, executorID)
	if err != nil {
		return err
	}
	return s.savePhoto(ctx, owner, executorID, photo)
}
