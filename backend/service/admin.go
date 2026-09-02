package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// maxAdminPageSize ограничивает админские списки, чтобы один запрос не мог
// попросить всю таблицу.
const maxAdminPageSize = 200

// SessionRevoker завершает все сессии пользователя. Удовлетворяется
// *AuthService; AdminService нужно от него ровно столько.
type SessionRevoker interface {
	RevokeAllSessions(ctx context.Context, userID uuid.UUID) error
}

// AdminService управляет административной бизнес-логикой.
type AdminService struct {
	userRepo      repository.UserRepository
	adminRepo     repository.AdminRepository
	settingsRepo  repository.SettingsRepository
	addressRepo   repository.AddressRepository
	ledger        *Ledger
	reconcileRepo repository.ReconciliationRepository
	sessions      SessionRevoker
	mailer        MailSender
	jwtSecret     []byte
	// events, когда подключён, записывает доменные события, порождаемые действием
	// админа, — сегодня только user.verified, которое закрывает заказ верификации
	// и оплачивает выполнившему его модератору.
	events repository.EventRepository
}

// WithEvents подключает outbox доменных событий к тем действиям админа, на
// которые реагируют поведения.
func (s *AdminService) WithEvents(events repository.EventRepository) *AdminService {
	s.events = events
	return s
}

// NewAdminService создаёт новый AdminService.
func NewAdminService(
	userRepo repository.UserRepository,
	adminRepo repository.AdminRepository,
	settingsRepo repository.SettingsRepository,
	jwtSecret string,
	mailer MailSender,
) *AdminService {
	secret := jwtSecret
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	if mailer == nil {
		mailer = NewSmtpMailSender()
	}
	return &AdminService{
		userRepo:     userRepo,
		adminRepo:    adminRepo,
		settingsRepo: settingsRepo,
		mailer:       mailer,
		jwtSecret:    []byte(secret),
	}
}

// WithAddresses присоединяет хранилище сохранённых адресов, используемое профилями.
func (s *AdminService) WithAddresses(addressRepo repository.AddressRepository) *AdminService {
	s.addressRepo = addressRepo
	return s
}

// ListAddresses возвращает сохранённые адреса подачи пользователя.
func (s *AdminService) ListAddresses(ctx context.Context, userID uuid.UUID) ([]repository.Address, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.List(ctx, userID)
}

// AddAddress сохраняет новый адрес подачи.
func (s *AdminService) AddAddress(ctx context.Context, userID uuid.UUID, address Address) ([]repository.Address, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	return s.addressRepo.Add(ctx, userID, address.ToRecord())
}

// DeleteAddress удаляет один из адресов пользователя.
func (s *AdminService) DeleteAddress(ctx context.Context, userID, addressID uuid.UUID) ([]repository.Address, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.Delete(ctx, userID, addressID)
}

// SetDefaultAddress отмечает, с какого адреса должны начинаться новые заказы.
func (s *AdminService) SetDefaultAddress(ctx context.Context, userID, addressID uuid.UUID) ([]repository.Address, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.SetDefault(ctx, userID, addressID)
}

// SetDefaultAddressByValue — то же для клиентов, опознающих адрес по его тексту.
func (s *AdminService) SetDefaultAddressByValue(ctx context.Context, userID uuid.UUID, address string) ([]repository.Address, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.SetDefaultByValue(ctx, userID, strings.TrimSpace(address))
}

// WithLedger присоединяет реестр. Пополнения и выводы двигают деньги, а реестр —
// единственное, что умеет их двигать.
func (s *AdminService) WithLedger(ledger *Ledger) *AdminService {
	s.ledger = ledger
	return s
}

// WithReconciliation включает отчёт о согласованности баланса и реестра.
func (s *AdminService) WithReconciliation(repo repository.ReconciliationRepository) *AdminService {
	s.reconcileRepo = repo
	return s
}

// Reconcile сравнивает каждый сохранённый баланс с суммой проводок этого
// пользователя. Только чтение: расхождение сообщается, но не правится молча.
func (s *AdminService) Reconcile(ctx context.Context, tolerance money.Amount) (*repository.ReconciliationReport, error) {
	if s.reconcileRepo == nil {
		return nil, errors.New("reconciliation is not configured")
	}
	if tolerance.IsNegative() {
		tolerance = money.Zero
	}

	started := time.Now()
	report, err := s.reconcileRepo.Reconcile(ctx, tolerance)
	metrics.WorkerRun("reconcile", time.Since(started), err)
	if err != nil {
		metrics.ReconcileFailed()
		return nil, err
	}

	// Проход, запущенный по требованию, публикует результат ровно как ночной.
	// Без этого принудительная сверка из админ-панели или из ops-бота показывала бы
	// зелёный отчёт на экране, пока алерт продолжает срабатывать по вчерашнему
	// датчику: двое расходились бы в оценке одних и тех же денег, и верили бы
	// именно экрану.
	metrics.ReconcileReport(
		report.OK(),
		len(report.Discrepancies),
		len(report.HoldAnomalies),
		len(report.UnknownTypes),
		report.Books.Difference.Rubles(),
		report.Books.EscrowDrift.Rubles(),
	)
	return report, nil
}

// WithSessions позволяет сервису завершать сессии пользователя при изменении
// его доступа. Без этого бан или понижение вступали бы в силу лишь по истечении
// refresh-токена.
func (s *AdminService) WithSessions(sessions SessionRevoker) *AdminService {
	s.sessions = sessions
	return s
}

// revokeSessions завершает все сессии пользователя, логируя ошибку, но не падая
// на ней: само изменение доступа уже сохранено.
func (s *AdminService) revokeSessions(ctx context.Context, userID uuid.UUID, reason string) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.RevokeAllSessions(ctx, userID); err != nil {
		log.Printf("[AUDIT] failed to end sessions of user %s after %s: %v", userID, reason, err)
	}
}

// GetUsers отдаёт список пользователей с фильтрами и поиском.
//
// role и status проверяются здесь, а не передаются прямо в enum-колонки:
// неожиданное значение раньше всплывало ошибкой базы и кодом 500, а это и
// плохой ответ, и способ прощупать схему.
func (s *AdminService) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*repository.User, int, error) {
	if role != "" && role != "CUSTOMER" && role != "EXECUTOR" && role != "ADMIN" {
		return nil, 0, errors.New("invalid role filter")
	}
	if status != "" && status != "ACTIVE" && status != "BANNED" {
		return nil, 0, errors.New("invalid status filter")
	}
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	return s.adminRepo.GetUsers(ctx, page, limit, role, status, search)
}

// UpdateUserStatus обновляет статус пользователя (например, ACTIVE или BANNED).
func (s *AdminService) UpdateUserStatus(ctx context.Context, userID, adminID uuid.UUID, status string) error {
	if status != "ACTIVE" && status != "BANNED" {
		return errors.New("invalid status")
	}
	if status == "BANNED" && userID == adminID {
		return errors.New("нельзя заблокировать самого себя")
	}
	if err := s.userRepo.UpdateStatus(ctx, userID, status); err != nil {
		return err
	}
	if status == "BANNED" {
		// Бан обязан завершить и сессии: RequireAuth отвергнет забаненного на
		// следующем запросе, но его refresh-токен иначе продолжал бы штамповать
		// access-токены.
		s.revokeSessions(ctx, userID, "ban")
	}
	log.Printf("[AUDIT] admin %s set status of user %s to %s", adminID, userID, status)
	return nil
}

// SetUserVerified переключает флаг ручной верификации у пользователя. Это тот
// самый админский чекбокс «верифицирован»: только он делает IsVerified()
// истинным, что, в свою очередь, управляет видимостью заказов для заказчика и
// услугами, требующими верифицированной учётной записи.
func (s *AdminService) SetUserVerified(ctx context.Context, userID, adminID uuid.UUID, verified bool) error {
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return errors.New("user not found")
	}
	if s.events == nil {
		if err := s.userRepo.UpdateVerified(ctx, userID, verified); err != nil {
			return err
		}
	} else if err := s.events.RunInTx(ctx, func(tx *sql.Tx) error {
		// Флаг и событие коммитятся вместе. Поведение, закрывающее заказ верификации
		// по этому событию, не должно ни разу увидеть флаг без события или событие
		// без флага.
		if err := s.userRepo.UpdateVerifiedTx(ctx, tx, userID, verified); err != nil {
			return err
		}
		if !verified {
			// Снятие верификации — это админ, забирающий что-то назад, а не событие,
			// на которое что-то реагирует.
			return nil
		}
		return s.events.Publish(ctx, tx, &repository.DomainEvent{
			Type:        repository.EventUserVerified,
			SubjectType: repository.EventSubjectUser,
			SubjectID:   userID,
			ActorID:     &adminID,
		})
	}); err != nil {
		return err
	}
	log.Printf("[AUDIT] admin %s set verified of user %s to %t", adminID, userID, verified)
	return nil
}

// UpdateUserRole меняет роль пользователя. Смена роли вступает в силу на
// следующем запросе, потому что авторизация читает роль из базы.
func (s *AdminService) UpdateUserRole(ctx context.Context, userID, adminID uuid.UUID, role string) error {
	if role != "CUSTOMER" && role != "EXECUTOR" && role != "ADMIN" {
		return errors.New("invalid role")
	}

	current, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return errors.New("user not found")
	}
	if current.Role == "ADMIN" && role != "ADMIN" {
		if userID == adminID {
			return errors.New("нельзя снять роль администратора с самого себя")
		}
		admins, err := s.adminRepo.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("нельзя снять роль с последнего администратора")
		}
	}

	if err := s.userRepo.UpdateRole(ctx, userID, role); err != nil {
		return err
	}
	// Авторизация читает роль из базы на каждом запросе, поэтому изменение уже
	// действует; завершение сессий заставляет клиента подхватить новую роль вместо
	// отрисовки интерфейса, которым он больше не может пользоваться.
	s.revokeSessions(ctx, userID, "role change")
	log.Printf("[AUDIT] admin %s changed role of user %s: %s -> %s", adminID, userID, current.Role, role)
	return nil
}

// validRoles — закрытый набор ролей, которые админ может назначать.
var validRoles = map[string]struct{}{
	repository.RoleCustomer:  {},
	repository.RoleExecutor:  {},
	repository.RoleModerator: {},
	repository.RoleAdmin:     {},
}

// UpdateUserRoles заменяет полный набор ролей пользователя. Он повторяет
// охранные правила однорольного варианта (пользователь не может лишить себя
// админских прав, а последнего админа нельзя понизить) и завершает сессии
// пользователя, чтобы клиент перечитал свои роли.
func (s *AdminService) UpdateUserRoles(ctx context.Context, userID, adminID uuid.UUID, roles []string) error {
	// Нормализуем: убираем дубли и проверяем.
	seen := map[string]struct{}{}
	clean := make([]string, 0, len(roles))
	for _, role := range roles {
		if _, ok := validRoles[role]; !ok {
			return fmt.Errorf("invalid role: %s", role)
		}
		if _, dup := seen[role]; dup {
			continue
		}
		seen[role] = struct{}{}
		clean = append(clean, role)
	}
	if len(clean) == 0 {
		return errors.New("у пользователя должна быть хотя бы одна роль")
	}

	current, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return errors.New("user not found")
	}

	// Охраняем роль админа так же, как это делают однорольные обновления.
	_, keepsAdmin := seen[repository.RoleAdmin]
	if current.HasRole(repository.RoleAdmin) && !keepsAdmin {
		if userID == adminID {
			return errors.New("нельзя снять роль администратора с самого себя")
		}
		admins, err := s.adminRepo.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("нельзя снять роль с последнего администратора")
		}
	}

	if err := s.userRepo.SetUserRoles(ctx, userID, clean); err != nil {
		return err
	}
	s.revokeSessions(ctx, userID, "roles change")
	log.Printf("[AUDIT] admin %s set roles of user %s: %v -> %v", adminID, userID, current.Roles, clean)
	return nil
}

// UpdateUserAddress задаёт адрес, с которого начинаются заказы пользователя (только для админов).
//
// «Обновить» здесь значит «сделать этот адрес адресом по умолчанию»: строка
// вставляется-или-обновляется и повышается, поэтому админ, исправляющий адрес,
// меняет тот, с которого заказчик и правда заказывает, а не добавляет второй.
func (s *AdminService) UpdateUserAddress(ctx context.Context, userID uuid.UUID, address string) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("address is required")
	}
	parsed := ParseAddressLine(address)
	if err := parsed.Validate(); err != nil {
		return err
	}
	if s.addressRepo == nil {
		return errors.New("address storage is not configured")
	}
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return errors.New("user not found")
	}
	record := parsed.ToRecord()
	record.IsDefault = true
	_, err := s.addressRepo.Add(ctx, userID, record)
	return err
}

// UpdateUserName обновляет ФИО пользователя (только для админов).
func (s *AdminService) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	patronymic = strings.TrimSpace(patronymic)
	if lastName == "" || firstName == "" || patronymic == "" {
		return errors.New("last_name, first_name and patronymic are required")
	}
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return errors.New("user not found")
	}
	return s.userRepo.UpdateUserName(ctx, userID, lastName, firstName, patronymic)
}

// UpdateUserBirthDate исправляет дату рождения пользователя (только для
// админов). Он делит parseBirthDate с регистрацией, поэтому админ не может
// сохранить дату, которую отвергла бы форма регистрации.
func (s *AdminService) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate string) error {
	parsed, err := parseBirthDate(birthDate)
	if err != nil {
		return err
	}
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return errors.New("user not found")
	}
	return s.userRepo.UpdateUserBirthDate(ctx, userID, parsed)
}

// TopUpUserBalance зачисляет средства прямо на баланс пользователя.
// Пополнять можно только не-админов, и админ не может зачислить самому себе.
func (s *AdminService) TopUpUserBalance(ctx context.Context, userID, adminID uuid.UUID, amount money.Amount) error {
	if !amount.IsPositive() {
		return errors.New("amount must be greater than zero")
	}

	if userID == adminID {
		return errors.New("admin cannot top up their own balance")
	}

	// Проверяем, что пользователь существует и не является админом
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return errors.New("user not found")
	}
	if user.Role == "ADMIN" {
		return errors.New("cannot top up an admin balance")
	}

	// Через реестр, как и любое другое движение денег. Прежняя реализация
	// зачисляла на баланс и писала строку транзакции сырым SQL, не трогая ни
	// одного системного счёта: собственная история пользователя всё ещё сходилась,
	// поэтому пользовательская сверка продолжала проходить, а книги платформы
	// расходились чуть сильнее с каждым пополнением.
	if s.ledger == nil {
		return errors.New("ledger is not configured")
	}
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.ledger.Deposit(ctx, tx, userID, amount, &adminID)
	}); err != nil {
		return err
	}

	log.Printf("[AUDIT] admin %s credited %s to user %s", adminID, amount, userID)
	return nil
}

// page нормализует запрошенный размер страницы. Админские списки раньше
// возвращали целые таблицы — это и медленный ответ, и лёгкий способ нагрузить базу.
func page(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// GetTopUpRequests перечисляет заявки на пополнение баланса, сначала новые.
func (s *AdminService) GetTopUpRequests(ctx context.Context, limit, offset int) ([]*repository.TopUpRequest, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetTopUpRequests(ctx, limit, offset)
}

// CreateTopUpRequest создаёт ожидающую заявку на пополнение баланса.
func (s *AdminService) CreateTopUpRequest(ctx context.Context, userID uuid.UUID, amount money.Amount) (*repository.TopUpRequest, error) {
	if !amount.IsPositive() {
		return nil, errors.New("amount must be greater than zero")
	}

	// Проверяем, что пользователь существует
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status == "BANNED" {
		return nil, errors.New("cannot request top-up for a banned user")
	}

	return s.adminRepo.CreateTopUpRequest(ctx, nil, userID, amount)
}

// ApproveTopUpRequest зачисляет запрошенную сумму пользователю.
//
// Деньги приходят со счёта DEPOSITS, представляющего внешний мир: раньше
// пополнение растило баланс, не имея ничего по другую сторону.
func (s *AdminService) ApproveTopUpRequest(ctx context.Context, requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideTopUp(ctx, requestID, adminID, "APPROVED")
}

// RejectTopUpRequest закрывает заявку, не двигая денег.
func (s *AdminService) RejectTopUpRequest(ctx context.Context, requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideTopUp(ctx, requestID, adminID, "REJECTED")
}

func (s *AdminService) decideTopUp(ctx context.Context, requestID, adminID uuid.UUID, status string) error {
	if s.ledger == nil {
		return errors.New("ledger is not configured")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		req, err := s.adminRepo.LockTopUpRequest(ctx, tx, requestID)
		if err != nil {
			return errors.New("request not found")
		}
		if req.Status != "PENDING" {
			return errors.New("request is not in PENDING status")
		}
		if err := s.adminRepo.SetTopUpStatus(ctx, tx, requestID, adminID, status); err != nil {
			return err
		}
		if status != "APPROVED" {
			return nil
		}
		return s.ledger.Deposit(ctx, tx, req.UserID, req.Amount, &adminID)
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("request is not in PENDING status")
		}
		return err
	}
	log.Printf("[AUDIT] admin %s set top-up request %s to %s", adminID, requestID, status)
	return nil
}

// GetWithdrawalRequests перечисляет все заявки на вывод средств.
func (s *AdminService) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*repository.WithdrawalRequest, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetWithdrawalRequests(ctx, limit, offset)
}

// CreateWithdrawalRequest резервирует запрошенную сумму и записывает ожидающую
// заявку на неё.
//
// Деньги уходят с баланса немедленно, ровно как удержание по заказу. Раньше
// заявка лишь проверяла баланс и оставляла средства тратимыми, поэтому
// пользователь мог поставить в очередь несколько заявок на одни и те же деньги
// и потратить их за время ожидания, а очередь выплат тогда содержала суммы,
// которые нельзя было выполнить все.
func (s *AdminService) CreateWithdrawalRequest(ctx context.Context, userID uuid.UUID, amount money.Amount) (*repository.WithdrawalRequest, error) {
	if !amount.IsPositive() {
		return nil, errors.New("amount must be greater than zero")
	}
	if s.ledger == nil {
		return nil, errors.New("ledger is not configured")
	}

	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status == "BANNED" {
		return nil, errors.New("cannot request withdrawal for a banned user")
	}

	pending, err := s.adminRepo.HasPendingWithdrawal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, errors.New("у вас уже есть заявка на вывод в обработке")
	}

	var created *repository.WithdrawalRequest
	err = s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		// Охраняемое списание: баланс обязан покрыть заявку в этот момент,
		// а не при каком-то более раннем чтении.
		// Деньги уходят с баланса на счёт выплат, где они ждут
		// решения администратора.
		if err := s.ledger.Reserve(ctx, tx, userID, repository.AccountPayouts, amount, repository.TransactionTypeWithdrawalHold, nil); err != nil {
			return err
		}
		req, err := s.adminRepo.CreateWithdrawalRequest(ctx, tx, userID, amount)
		if err != nil {
			return err
		}
		created = req
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, errors.New("insufficient balance for withdrawal")
		}
		return nil, err
	}
	return created, nil
}

// ApproveWithdrawalRequest помечает зарезервированный вывод выплаченным.
// Никакого движения баланса тут не происходит: деньги ушли с баланса при
// создании заявки, а это фиксирует расход того резерва.
func (s *AdminService) ApproveWithdrawalRequest(ctx context.Context, requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideWithdrawal(ctx, requestID, adminID, "APPROVED")
}

// RejectWithdrawalRequest возвращает зарезервированные деньги пользователю.
func (s *AdminService) RejectWithdrawalRequest(ctx context.Context, requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideWithdrawal(ctx, requestID, adminID, "REJECTED")
}

func (s *AdminService) decideWithdrawal(ctx context.Context, requestID, adminID uuid.UUID, status string) error {
	if s.ledger == nil {
		return errors.New("ledger is not configured")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		req, err := s.adminRepo.LockWithdrawalRequest(ctx, tx, requestID)
		if err != nil {
			return errors.New("request not found")
		}
		if req.Status != "PENDING" {
			return errors.New("request is not in PENDING status")
		}

		if err := s.adminRepo.SetWithdrawalStatus(ctx, tx, requestID, adminID, status); err != nil {
			return err
		}

		if status == "REJECTED" {
			// Возвращаем зарезервированные деньги.
			return s.ledger.Release(ctx, tx, repository.AccountPayouts, req.UserID, req.Amount, repository.TransactionTypeRefund, nil, &adminID)
		}

		// Выплачено: резерв покидает систему через счёт, представляющий
		// внешний мир.
		return s.ledger.Settle(ctx, tx, repository.AccountPayouts, repository.AccountDeposits, req.UserID, req.Amount, repository.TransactionTypeWithdrawalPaid, &adminID)
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("request is not in PENDING status")
		}
		return err
	}
	log.Printf("[AUDIT] admin %s set withdrawal request %s to %s", adminID, requestID, status)
	return nil
}

// GetTransactions отдаёт историю транзакций.
func (s *AdminService) GetTransactions(ctx context.Context, limit, offset int) ([]*repository.Transaction, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetTransactions(ctx, limit, offset)
}

// GetActiveShifts возвращает все активные сейчас смены исполнителей.
func (s *AdminService) GetActiveShifts(ctx context.Context) ([]*repository.AdminShift, error) {
	return s.adminRepo.GetActiveShifts(ctx)
}

// GetActiveOrders возвращает заказы клиентов, которые ещё активны (в поиске или назначены).
func (s *AdminService) GetActiveOrders(ctx context.Context, limit, offset int) ([]*repository.AdminOrder, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetActiveOrders(ctx, limit, offset)
}

// GetCompletedOrders возвращает одну страницу завершённых заказов клиентов
// вместе с общим числом подходящих под фильтр, чтобы клиент мог листать и
// выгружать, не гадая, сколько стоит за имеющейся у него страницей.
func (s *AdminService) GetCompletedOrders(ctx context.Context, f repository.CompletedOrdersFilter) ([]*repository.AdminOrder, int, error) {
	f.Limit, f.Offset = page(f.Limit, f.Offset)
	return s.adminRepo.GetCompletedOrders(ctx, f)
}

// CompletedOrderFacets возвращает значения, которые предлагают фильтры завершённых заказов.
func (s *AdminService) CompletedOrderFacets(ctx context.Context) (repository.CompletedOrderFacets, error) {
	return s.adminRepo.CompletedOrderFacets(ctx)
}

// GetProfile возвращает профиль аутентифицированного пользователя, включая адрес заказчика.
func (s *AdminService) GetProfile(ctx context.Context, userID uuid.UUID) (map[string]interface{}, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	user.Password = ""

	profile := map[string]interface{}{
		"id":         user.ID,
		"role":       user.Role,
		"roles":      user.Roles,
		"phone":      user.Phone,
		"email":      user.Email,
		"balance":    user.Balance,
		"status":     user.Status,
		"created_at": user.CreatedAt,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"patronymic": user.Patronymic,
		"birth_date": user.BirthDateString(),
		"age":        user.GetAge(),
		"address":    "",
	}

	profile["addresses"] = []repository.Address{}
	if s.addressRepo != nil {
		addresses, err := s.addressRepo.List(ctx, userID)
		if err != nil {
			log.Printf("[GetProfile] failed to load addresses for %s: %v", userID, err)
		} else {
			profile["addresses"] = addresses
			for _, a := range addresses {
				if a.IsDefault {
					profile["default_address"] = a.Address
					profile["address"] = a.Address
					break
				}
			}
		}
	}
	if _, ok := profile["default_address"]; !ok {
		profile["default_address"] = profile["address"]
	}

	return profile, nil
}

// GetSettings отдаёт глобальные настройки.
func (s *AdminService) GetSettings(ctx context.Context) (map[string]string, error) {
	return s.settingsRepo.GetSettings(ctx)
}

// UpdateSettings обновляет глобальные настройки.
func (s *AdminService) UpdateSettings(ctx context.Context, settings map[string]string) error {
	// Числовые настройки, где это применимо, обязаны быть неотрицательными.
	numericKeys := map[string]bool{
		"standard_tariff_coeff":  true,
		"increased_tariff_coeff": true,
		"urgent_tariff_coeff":    true,
		"asap_tariff_coeff":      true,
		"min_balance_limit":      true,
		// Как далеко может дотянуться автоматический подбор при назначении заказа.
		"auto_match_radius_km": true,
	}
	numericKeys["shift_early_exit_penalty"] = true
	// Включение этого заставляет приложения исполнителей сообщать своё положение,
	// что держит сохранённую позицию свежей для карты и автоподбора. Принимаются
	// только «1» и «0», чтобы его нельзя было включить опечаткой.
	if v, ok := settings["geofence_tracking_enabled"]; ok && v != "0" && v != "1" {
		return errors.New("setting geofence_tracking_enabled must be 0 or 1")
	}
	// Назначает ли фоновый воркер заказы автоматически. По умолчанию выключено;
	// только «1» или «0», чтобы его нельзя было включить опечаткой.
	if v, ok := settings["auto_matching_enabled"]; ok && v != "0" && v != "1" {
		return errors.New("setting auto_matching_enabled must be 0 or 1")
	}
	numericKeys["reject_penalty_share"] = true
	// Доля платформы с завершённого заказа. Снизу ограничена вместе с прочими
	// числовыми настройками, а сверху — прямо здесь, потому что доля выше 100%
	// платила бы исполнителю отрицательное вознаграждение.
	numericKeys[SettingOrderCommissionPercent] = true
	positiveIntKeys := map[string]bool{
		"executor_location_send_interval_seconds": true,
		"max_active_orders":                       true,
		"max_executed_unconfirmed_orders":         true,
	}
	for key, value := range settings {
		if numericKeys[key] {
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return errors.New("setting " + key + " must be numeric")
			}
			if v < 0 {
				return errors.New("setting " + key + " value cannot be negative")
			}
			if key == "reject_penalty_share" && v > 1 {
				return errors.New("setting reject_penalty_share must be between 0 and 1")
			}
			if key == SettingOrderCommissionPercent && v > 100 {
				return errors.New("setting " + SettingOrderCommissionPercent + " must be between 0 and 100")
			}
		}
		if positiveIntKeys[key] {
			v, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("setting " + key + " must be an integer")
			}
			if v < 1 {
				return errors.New("setting " + key + " must be at least 1 second")
			}
		}
	}
	return s.settingsRepo.UpdateSettings(ctx, settings)
}

// Commission — то, что админский экран показывает про долю платформы: сколько
// собрано и всё ещё лежит на счёте комиссии и по какой ставке она берётся
// сейчас.
type Commission struct {
	Balance money.Amount `json:"balance"`
	Percent float64      `json:"percent"`
}

// GetCommission сообщает баланс счёта комиссии и текущую ставку.
func (s *AdminService) GetCommission(ctx context.Context) (*Commission, error) {
	if s.ledger == nil {
		return nil, errors.New("ledger is not configured")
	}
	account, err := s.ledger.AccountBalance(ctx, repository.AccountCommission)
	if err != nil {
		return nil, err
	}
	return &Commission{Balance: account.Balance, Percent: s.commissionPercent(ctx)}, nil
}

// commissionPercent читает настроенную ставку, откатываясь к нулю, когда
// настройка отсутствует или нечитаема: не брать ничего — безопасное направление
// отказа.
func (s *AdminService) commissionPercent(ctx context.Context) float64 {
	if s.settingsRepo == nil {
		return 0
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return 0
	}
	percent, err := strconv.ParseFloat(settings[SettingOrderCommissionPercent], 64)
	if err != nil {
		return 0
	}
	return percent
}

// PayoutCommission выводит собранную комиссию из системы. Сюда дотягивается
// только админ — маршрут требует роли, — и админ записывается в проводку,
// поэтому у выплаты всегда есть имя.
//
// Списание охраняется балансом счёта, поэтому два админа, выплачивающих
// одновременно, не могут вместе вывести больше, чем собрано.
func (s *AdminService) PayoutCommission(ctx context.Context, adminID uuid.UUID, amount money.Amount) (*Commission, error) {
	if !amount.IsPositive() {
		return nil, errors.New("amount must be greater than zero")
	}
	if s.ledger == nil {
		return nil, errors.New("ledger is not configured")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.ledger.Payout(ctx, tx, repository.AccountCommission, adminID, amount,
			repository.TransactionTypeCommissionPayout, &adminID)
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, errors.New("commission account holds less than the requested amount")
		}
		return nil, err
	}
	log.Printf("[AUDIT] admin %s withdrew %s from the commission account", adminID, amount)

	return s.GetCommission(ctx)
}

// BroadcastEmailRequest описывает полезную нагрузку рассылки писем.
type BroadcastEmailRequest struct {
	TargetGroup  string   `json:"target_group"` // CUSTOMERS, EXECUTORS, CUSTOM_EMAILS
	CustomEmails []string `json:"custom_emails,omitempty"`
	Subject      string   `json:"subject"`
	BodyHTML     string   `json:"body_html"`
}

// BroadcastEmailResult содержит сводку об отправленных письмах.
type BroadcastEmailResult struct {
	Total      int      `json:"total"`
	Successful int      `json:"successful"`
	Failed     int      `json:"failed"`
	Failures   []string `json:"failures,omitempty"`
}

// SendBroadcastEmail рассылает письма выбранным группам пользователей или произвольному списку получателей.
func (s *AdminService) SendBroadcastEmail(ctx context.Context, req BroadcastEmailRequest) (*BroadcastEmailResult, error) {
	req.Subject = strings.TrimSpace(req.Subject)
	req.BodyHTML = strings.TrimSpace(req.BodyHTML)
	if req.Subject == "" || req.BodyHTML == "" {
		return nil, errors.New("subject and body_html are required")
	}

	var recipientEmails []string
	switch strings.ToUpper(req.TargetGroup) {
	case "CUSTOMERS":
		users, _, err := s.adminRepo.GetUsers(ctx, 1, 10000, "CUSTOMER", "", "")
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.Email != "" && u.EmailVerified {
				recipientEmails = append(recipientEmails, u.Email)
			}
		}
	case "EXECUTORS":
		users, _, err := s.adminRepo.GetUsers(ctx, 1, 10000, "EXECUTOR", "", "")
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.Email != "" && u.EmailVerified {
				recipientEmails = append(recipientEmails, u.Email)
			}
		}
	case "CUSTOM_EMAILS":
		for _, email := range req.CustomEmails {
			trimmed := strings.TrimSpace(email)
			if trimmed == "" {
				continue
			}
			// Отвергаем всё, что не является обычным адресом: заголовки письма
			// собираются конкатенацией, поэтому CR/LF здесь — инъекция заголовков.
			if !validRecipient.MatchString(trimmed) {
				return nil, fmt.Errorf("invalid recipient address: %s", trimmed)
			}
			recipientEmails = append(recipientEmails, trimmed)
		}
	default:
		return nil, errors.New("invalid target_group: must be CUSTOMERS, EXECUTORS, or CUSTOM_EMAILS")
	}

	if len(recipientEmails) == 0 {
		return nil, errors.New("no valid recipient emails found for specified target group")
	}

	result := &BroadcastEmailResult{
		Total: len(recipientEmails),
	}

	smtpSender, ok := s.mailer.(*SmtpMailSender)
	if !ok {
		// Без настоящего транспорта ничего не отправляется; отчёт об успехе был бы ложью.
		return nil, errors.New("email transport is not available")
	}
	for _, email := range recipientEmails {
		err := smtpSender.SendEmail(email, req.Subject, req.BodyHTML)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", email, err))
		} else {
			result.Successful++
		}
	}

	return result, nil
}
