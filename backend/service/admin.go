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

// Ошибки административных действий. Класс каждой — из errors.go, поэтому
// обработчик отвечает кодом по классу, а текст показывает как есть.
var (
	// ErrTopUpRequestNotFound и ErrWithdrawalRequestNotFound — заявки с таким id нет.
	ErrTopUpRequestNotFound      = notFoundError("заявка на пополнение не найдена")
	ErrWithdrawalRequestNotFound = notFoundError("заявка на вывод не найдена")
	// ErrRequestNotPending — по заявке уже принято решение.
	ErrRequestNotPending = stateError("request is not in PENDING status")
	// ErrSelfAction — админ пытается применить действие к самому себе там, где
	// это запрещено: заблокировать, снять роль администратора, пополнить баланс.
	ErrSelfBan       = ruleError("нельзя заблокировать самого себя")
	ErrSelfDemote    = ruleError("нельзя снять роль администратора с самого себя")
	ErrSelfTopUp     = ruleError("admin cannot top up their own balance")
	ErrAdminTopUp    = ruleError("cannot top up an admin balance")
	ErrLastAdmin     = ruleError("нельзя снять роль с последнего администратора")
	ErrNoRoles       = validationError("у пользователя должна быть хотя бы одна роль")
	ErrCommissionLow = &DomainError{Kind: repository.ErrInsufficientFunds, Msg: "commission account holds less than the requested amount"}
	// ErrMailerNotConfigured — рассылка без транспорта. Это ошибка сборки, а не
	// запроса: без настроенного SMTP отчёт об успехе был бы ложью.
	ErrMailerNotConfigured = fmt.Errorf("%w: email transport", ErrNotConfigured)
)

// AdminService управляет административной бизнес-логикой: пользователи,
// решения по заявкам, журнал, настройки, рассылки. Самообслуживание
// пользователя (профиль, адреса, собственные заявки) — в ProfileService и
// WalletService.
type AdminService struct {
	userRepo     repository.UserRepository
	adminUsers   repository.AdminUserRepository
	settingsRepo repository.SettingsRepository
	addressRepo  repository.AddressRepository
	// payouts — заявки на пополнение и вывод; journal — журнал проводок;
	// orders — список заказов панели; shifts — активные смены. Каждый
	// подключается своим With*: сервис собирается из того, что панели нужно, и
	// тест подключает только то, что проверяет.
	payouts repository.PayoutRepository
	journal repository.TransactionJournalRepository
	orders  repository.AdminOrderRepository
	shifts  repository.ShiftMonitorRepository
	// txFacets и orderFacets кэшируют значения фильтров списков: DISTINCT по
	// всей таблице на каждой странице — то, ради чего кэш и есть.
	txFacets    *facetCache[repository.TransactionFacets]
	orderFacets *facetCache[repository.OrderFacets]

	ledger        *Ledger
	reconcileRepo repository.ReconciliationRepository
	sessions      SessionRevoker
	// mailer отправляет письма рассылок. nil — рассылки отвечают ErrNotConfigured.
	mailer EmailSender
	// events, когда подключён, записывает доменные события, порождаемые действием
	// админа, — сегодня только user.verified, которое закрывает заказ верификации
	// и оплачивает выполнившему его модератору.
	events repository.EventRepository
	// roleRepo — справочник ролей. Через него проверяется, что назначаемая или
	// фильтруемая роль вообще существует; nil означает «справочник не
	// подключён», и тогда допустимы только четыре системные роли.
	roleRepo repository.RoleRepository
	// penalties записывает, кто и почему поставил мягкий бан. nil — статус
	// SOFT_BANNED ставится без причины, как любой другой.
	penalties repository.PenaltyRepository
}

// NewAdminService создаёт AdminService. mailer может быть nil — тогда рассылки
// недоступны; SMTP-транспорт здесь не создаётся сам: сервис, который молча
// заводит себе почту, отправлял бы живые письма из тестов и из процессов, где
// её не просили.
func NewAdminService(
	userRepo repository.UserRepository,
	adminUsers repository.AdminUserRepository,
	settingsRepo repository.SettingsRepository,
	mailer EmailSender,
) *AdminService {
	return &AdminService{
		userRepo:     userRepo,
		adminUsers:   adminUsers,
		settingsRepo: settingsRepo,
		mailer:       mailer,
		txFacets:     newFacetCache[repository.TransactionFacets](facetCacheTTL),
		orderFacets:  newFacetCache[repository.OrderFacets](facetCacheTTL),
	}
}

// WithPayouts подключает заявки на пополнение и вывод.
func (s *AdminService) WithPayouts(payouts repository.PayoutRepository) *AdminService {
	s.payouts = payouts
	return s
}

// WithJournal подключает журнал проводок.
func (s *AdminService) WithJournal(journal repository.TransactionJournalRepository) *AdminService {
	s.journal = journal
	return s
}

// WithOrders подключает список заказов панели.
func (s *AdminService) WithOrders(orders repository.AdminOrderRepository) *AdminService {
	s.orders = orders
	return s
}

// WithShifts подключает экран активных смен.
func (s *AdminService) WithShifts(shifts repository.ShiftMonitorRepository) *AdminService {
	s.shifts = shifts
	return s
}

// WithPenalties подключает штрафное состояние к смене статуса пользователя.
func (s *AdminService) WithPenalties(penalties repository.PenaltyRepository) *AdminService {
	s.penalties = penalties
	return s
}

// WithRoles подключает справочник ролей к назначению ролей пользователю.
func (s *AdminService) WithRoles(roles repository.RoleRepository) *AdminService {
	s.roleRepo = roles
	return s
}

// WithEvents подключает outbox доменных событий к тем действиям админа, на
// которые реагируют поведения.
func (s *AdminService) WithEvents(events repository.EventRepository) *AdminService {
	s.events = events
	return s
}

// WithAddresses присоединяет хранилище сохранённых адресов: админ правит адрес
// подачи с карточки пользователя.
func (s *AdminService) WithAddresses(addressRepo repository.AddressRepository) *AdminService {
	s.addressRepo = addressRepo
	return s
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

// WithSessions позволяет сервису завершать сессии пользователя при изменении
// его доступа. Без этого бан или понижение вступали бы в силу лишь по истечении
// refresh-токена.
func (s *AdminService) WithSessions(sessions SessionRevoker) *AdminService {
	s.sessions = sessions
	return s
}

// notConfigured — ErrNotConfigured с именем недостающей зависимости.
func notConfigured(what string) error {
	return fmt.Errorf("%w: %s", ErrNotConfigured, what)
}

// findUser читает пользователя, переводя «нет строки» в ErrUserNotFound.
func (s *AdminService) findUser(ctx context.Context, userID uuid.UUID) (*repository.User, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, userNotFound(err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// Reconcile сравнивает каждый сохранённый баланс с суммой проводок этого
// пользователя. Только чтение: расхождение сообщается, но не правится молча.
func (s *AdminService) Reconcile(ctx context.Context, tolerance money.Amount) (*repository.ReconciliationReport, error) {
	if s.reconcileRepo == nil {
		return nil, notConfigured("reconciliation")
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
// role и status проверяются здесь, а не передаются прямо в запрос: неожиданное
// значение раньше всплывало ошибкой базы и кодом 500, а это и плохой ответ, и
// способ прощупать схему. Роль — любая из справочника (или из четырёх
// системных, когда справочник не подключён): список фильтруется по тому же
// набору, из которого роли назначаются.
func (s *AdminService) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*repository.User, int, error) {
	if role != "" && !s.knownRole(ctx, role) {
		return nil, 0, validationError("invalid role filter")
	}
	if status != "" && status != repository.UserStatusActive && status != repository.UserStatusSoftBanned && status != repository.UserStatusBanned {
		return nil, 0, validationError("invalid status filter")
	}
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	return s.adminUsers.GetUsers(ctx, page, limit, role, status, search)
}

// UpdateUserStatus обновляет статус пользователя: ACTIVE, SOFT_BANNED или
// BANNED. reason — причина мягкого бана, её видит администрация в карточке.
func (s *AdminService) UpdateUserStatus(ctx context.Context, userID, adminID uuid.UUID, status, reason string) error {
	switch status {
	case repository.UserStatusActive, repository.UserStatusSoftBanned, repository.UserStatusBanned:
	default:
		return validationError("invalid status")
	}
	if status != repository.UserStatusActive && userID == adminID {
		return ErrSelfBan
	}

	switch {
	case status == repository.UserStatusSoftBanned && s.penalties != nil:
		// Статус и причина пишутся вместе. Сессии не завершаются: мягкий бан
		// пускает в приложение, а RequireAuth закроет всё лишнее со следующего
		// запроса после истечения кэша пользователя.
		if err := s.penalties.ApplySoftBan(ctx, nil, userID, &adminID, strings.TrimSpace(reason)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUserNotFound
			}
			return err
		}
	case status == repository.UserStatusActive && s.penalties != nil:
		user, err := s.findUser(ctx, userID)
		if err != nil {
			return err
		}
		if user.Status == repository.UserStatusSoftBanned {
			// Снятие мягкого бана заодно стирает его причину.
			if err := s.penalties.LiftSoftBan(ctx, nil, userID); err != nil {
				return err
			}
			break
		}
		if err := s.userRepo.UpdateStatus(ctx, userID, status); err != nil {
			return err
		}
	default:
		if err := s.userRepo.UpdateStatus(ctx, userID, status); err != nil {
			return err
		}
	}

	if status == repository.UserStatusBanned {
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
	if _, err := s.findUser(ctx, userID); err != nil {
		return err
	}
	if s.events == nil {
		if err := s.userRepo.UpdateVerified(ctx, nil, userID, verified); err != nil {
			return err
		}
	} else if err := s.events.RunInTx(ctx, func(tx *sql.Tx) error {
		// Флаг и событие коммитятся вместе. Поведение, закрывающее заказ верификации
		// по этому событию, не должно ни разу увидеть флаг без события или событие
		// без флага.
		if err := s.userRepo.UpdateVerified(ctx, tx, userID, verified); err != nil {
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

// unknownRole — отказ назначить или отфильтровать роль, которой нет в справочнике.
func unknownRole(role string) error {
	return validationError("роль не найдена: " + role)
}

// UpdateUserRole меняет роль пользователя. Смена роли вступает в силу на
// следующем запросе, потому что авторизация читает роль из базы.
func (s *AdminService) UpdateUserRole(ctx context.Context, userID, adminID uuid.UUID, role string) error {
	if !s.knownRole(ctx, role) {
		return unknownRole(role)
	}
	if s.privilegedRole(ctx, role) {
		if err := requireAdminActor(ctx, s.userRepo, adminID); err != nil {
			return err
		}
	}

	current, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if current.Role == repository.RoleAdmin && role != repository.RoleAdmin {
		if err := s.guardLastAdmin(ctx, userID, adminID); err != nil {
			return err
		}
	}

	if err := s.userRepo.SetUserRoles(ctx, userID, []string{role}); err != nil {
		return err
	}
	// Авторизация читает роль из базы на каждом запросе, поэтому изменение уже
	// действует; завершение сессий заставляет клиента подхватить новую роль вместо
	// отрисовки интерфейса, которым он больше не может пользоваться.
	s.revokeSessions(ctx, userID, "role change")
	log.Printf("[AUDIT] admin %s changed role of user %s: %s -> %s", adminID, userID, current.Role, role)
	return nil
}

// guardLastAdmin отказывает снять роль администратора с самого себя и с
// последнего администратора платформы.
func (s *AdminService) guardLastAdmin(ctx context.Context, userID, adminID uuid.UUID) error {
	if userID == adminID {
		return ErrSelfDemote
	}
	admins, err := s.adminUsers.CountAdmins(ctx)
	if err != nil {
		return err
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// systemRoles — роли, которые есть всегда, независимо от справочника. Они
// остаются запасным набором для процесса, поднятого без него: назначить роль,
// которой нет в базе, нельзя, но четыре базовые обязаны работать и тогда.
var systemRoles = map[string]struct{}{
	repository.RoleCustomer:  {},
	repository.RoleExecutor:  {},
	repository.RoleModerator: {},
	repository.RoleAdmin:     {},
}

// privilegedRole — роль, которую назначает только администратор: сам ADMIN и
// любая роль с правом roles.*, потому что её носитель раздаёт права дальше.
func (s *AdminService) privilegedRole(ctx context.Context, role string) bool {
	if role == repository.RoleAdmin {
		return true
	}
	if s.roleRepo == nil {
		return false
	}
	found, err := s.roleRepo.Get(ctx, role)
	return err == nil && managesRoles(found.Permissions)
}

// knownRole сообщает, есть ли такая роль в справочнике. Набор допустимых ролей
// больше не зашит в код: администратор заводит их на странице ролей, и
// назначение и фильтр списка обязаны следовать за справочником, а не за
// константами.
func (s *AdminService) knownRole(ctx context.Context, role string) bool {
	if s.roleRepo == nil {
		_, ok := systemRoles[role]
		return ok
	}
	if _, err := s.roleRepo.Get(ctx, role); err != nil {
		// Роли нет — отказ. Ошибка чтения тоже приводит сюда, и это верно:
		// назначить роль вслепую хуже, чем не назначить.
		return false
	}
	return true
}

// UpdateUserRoles заменяет полный набор ролей пользователя. Он повторяет
// охранные правила однорольного варианта (пользователь не может лишить себя
// админских прав, а последнего админа нельзя понизить) и завершает сессии
// пользователя, чтобы клиент перечитал свои роли.
func (s *AdminService) UpdateUserRoles(ctx context.Context, userID, adminID uuid.UUID, roles []string) error {
	// Нормализуем: убираем дубли и проверяем.
	seen := map[string]struct{}{}
	clean := make([]string, 0, len(roles))
	privileged := false
	for _, role := range roles {
		if !s.knownRole(ctx, role) {
			return unknownRole(role)
		}
		if _, dup := seen[role]; dup {
			continue
		}
		seen[role] = struct{}{}
		clean = append(clean, role)
		privileged = privileged || s.privilegedRole(ctx, role)
	}
	if len(clean) == 0 {
		return ErrNoRoles
	}
	if privileged {
		if err := requireAdminActor(ctx, s.userRepo, adminID); err != nil {
			return err
		}
	}

	current, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}

	// Охраняем роль админа так же, как это делают однорольные обновления.
	_, keepsAdmin := seen[repository.RoleAdmin]
	if current.HasRole(repository.RoleAdmin) && !keepsAdmin {
		if err := s.guardLastAdmin(ctx, userID, adminID); err != nil {
			return err
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
		return validationError("address is required")
	}
	parsed := ParseAddressLine(address)
	if err := parsed.Validate(); err != nil {
		return validationError(err.Error())
	}
	if s.addressRepo == nil {
		return notConfigured("address storage")
	}
	if _, err := s.findUser(ctx, userID); err != nil {
		return err
	}
	record := parsed.ToRecord()
	record.IsDefault = true
	_, err := s.addressRepo.Add(ctx, nil, userID, record)
	return err
}

// UpdateUserName обновляет ФИО пользователя (только для админов).
func (s *AdminService) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	patronymic = strings.TrimSpace(patronymic)
	if lastName == "" || firstName == "" || patronymic == "" {
		return validationError("last_name, first_name and patronymic are required")
	}
	if _, err := s.findUser(ctx, userID); err != nil {
		return err
	}
	return s.userRepo.UpdateUserName(ctx, nil, userID, lastName, firstName, patronymic)
}

// UpdateUserBirthDate исправляет дату рождения пользователя (только для
// админов). Он делит parseBirthDate с регистрацией, поэтому админ не может
// сохранить дату, которую отвергла бы форма регистрации.
func (s *AdminService) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate string) error {
	parsed, err := parseBirthDate(birthDate)
	if err != nil {
		return validationError(err.Error())
	}
	if _, err := s.findUser(ctx, userID); err != nil {
		return err
	}
	return s.userRepo.UpdateUserBirthDate(ctx, nil, userID, parsed)
}

// TopUpUserBalance зачисляет средства прямо на баланс пользователя.
// Пополнять можно только не-админов, и админ не может зачислить самому себе.
func (s *AdminService) TopUpUserBalance(ctx context.Context, userID, adminID uuid.UUID, amount money.Amount) error {
	if !amount.IsPositive() {
		return ErrAmountNotPositive
	}
	if userID == adminID {
		return ErrSelfTopUp
	}

	// Проверяем, что пользователь существует и не является админом
	user, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if user.HasRole(repository.RoleAdmin) {
		return ErrAdminTopUp
	}

	// Через реестр, как и любое другое движение денег. Прежняя реализация
	// зачисляла на баланс и писала строку транзакции сырым SQL, не трогая ни
	// одного системного счёта: собственная история пользователя всё ещё сходилась,
	// поэтому пользовательская сверка продолжала проходить, а книги платформы
	// расходились чуть сильнее с каждым пополнением.
	if s.ledger == nil {
		return notConfigured("ledger")
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
	if s.payouts == nil {
		return nil, notConfigured("payouts")
	}
	limit, offset = page(limit, offset)
	return s.payouts.GetTopUpRequests(ctx, limit, offset)
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
	if s.ledger == nil || s.payouts == nil {
		return notConfigured("ledger")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		req, err := s.payouts.LockTopUpRequest(ctx, tx, requestID)
		if err != nil {
			return ErrTopUpRequestNotFound
		}
		if req.Status != "PENDING" {
			return ErrRequestNotPending
		}
		if err := s.payouts.SetTopUpStatus(ctx, tx, requestID, adminID, status); err != nil {
			return err
		}
		if status != "APPROVED" {
			return nil
		}
		return s.ledger.Deposit(ctx, tx, req.UserID, req.Amount, &adminID)
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return ErrRequestNotPending
		}
		return err
	}
	log.Printf("[AUDIT] admin %s set top-up request %s to %s", adminID, requestID, status)
	return nil
}

// GetWithdrawalRequests перечисляет все заявки на вывод средств.
func (s *AdminService) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*repository.WithdrawalRequest, error) {
	if s.payouts == nil {
		return nil, notConfigured("payouts")
	}
	limit, offset = page(limit, offset)
	return s.payouts.GetWithdrawalRequests(ctx, limit, offset)
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
	if s.ledger == nil || s.payouts == nil {
		return notConfigured("ledger")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		req, err := s.payouts.LockWithdrawalRequest(ctx, tx, requestID)
		if err != nil {
			return ErrWithdrawalRequestNotFound
		}
		if req.Status != "PENDING" {
			return ErrRequestNotPending
		}

		if err := s.payouts.SetWithdrawalStatus(ctx, tx, requestID, adminID, status); err != nil {
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
			return ErrRequestNotPending
		}
		return err
	}
	log.Printf("[AUDIT] admin %s set withdrawal request %s to %s", adminID, requestID, status)
	return nil
}

// GetTransactions отдаёт страницу журнала проводок. Счётчик считается только
// по просьбе (f.Page.WithTotal).
func (s *AdminService) GetTransactions(ctx context.Context, f repository.TransactionsFilter) ([]*repository.Transaction, int, error) {
	if s.journal == nil {
		return nil, 0, notConfigured("transaction journal")
	}
	f.Page.Limit, f.Page.Offset = page(f.Page.Limit, f.Page.Offset)
	return s.journal.GetTransactions(ctx, f)
}

// GetUserTransactions возвращает проводки одного пользователя для его карточки.
func (s *AdminService) GetUserTransactions(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*repository.Transaction, int, error) {
	if s.journal == nil {
		return nil, 0, notConfigured("transaction journal")
	}
	limit, offset = page(limit, offset)
	return s.journal.GetUserTransactions(ctx, userID, limit, offset)
}

// GetUserOrders возвращает заказы пользователя в обеих ролях для его карточки.
func (s *AdminService) GetUserOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*repository.AdminOrder, int, error) {
	if s.orders == nil {
		return nil, 0, notConfigured("admin orders")
	}
	limit, offset = page(limit, offset)
	return s.orders.GetUserOrders(ctx, userID, limit, offset)
}

// TransactionFacets возвращает значения, которые предлагают фильтры журнала.
// Из кэша: см. facetCache.
func (s *AdminService) TransactionFacets(ctx context.Context) (repository.TransactionFacets, error) {
	if s.journal == nil {
		return repository.TransactionFacets{}, notConfigured("transaction journal")
	}
	return s.txFacets.get("", func() (repository.TransactionFacets, error) {
		return s.journal.TransactionFacets(ctx)
	})
}

// GetActiveShifts возвращает все активные сейчас смены исполнителей.
func (s *AdminService) GetActiveShifts(ctx context.Context) ([]*repository.AdminShift, error) {
	if s.shifts == nil {
		return nil, notConfigured("shift monitor")
	}
	return s.shifts.ListActiveWithExecutors(ctx)
}

// GetOrders возвращает одну страницу списка заказов. Общее число подходящих
// под фильтр считается только по просьбе (f.Page.WithTotal): клиент берёт его
// с первой страницы и держит у себя, пока листает и выгружает.
func (s *AdminService) GetOrders(ctx context.Context, f repository.OrdersFilter) ([]*repository.AdminOrder, int, error) {
	if s.orders == nil {
		return nil, 0, notConfigured("admin orders")
	}
	f.Page.Limit, f.Page.Offset = page(f.Page.Limit, f.Page.Offset)
	return s.orders.GetOrders(ctx, f)
}

// OrderFacets возвращает значения фильтров услуги и периода для группы
// статусов. Из кэша, ключ — сама группа.
func (s *AdminService) OrderFacets(ctx context.Context, statuses []repository.OrderStatus) (repository.OrderFacets, error) {
	if s.orders == nil {
		return repository.OrderFacets{}, notConfigured("admin orders")
	}
	key := make([]string, len(statuses))
	for i, st := range statuses {
		key[i] = string(st)
	}
	return s.orderFacets.get(strings.Join(key, ","), func() (repository.OrderFacets, error) {
		return s.orders.OrderFacets(ctx, statuses)
	})
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
		// Как далеко исполнитель может взять заказ руками. Отдельная величина от
		// auto_match_radius_km и обычно заметно меньше: автоподбор сам привозит
		// заказ тому, кто согласился работать, а здесь исполнитель выбирает, куда
		// ехать. Ноль или пусто означает «не задано» — тогда действует
		// ACCEPT_RADIUS_KM из окружения, а за ним умолчание.
		SettingAcceptRadiusKM: true,
		// Радиус обзора: что исполнителю показывают на карте и в списке рядом.
		SettingMapOverviewRadiusKM: true,
	}
	numericKeys["shift_early_exit_penalty"] = true
	// Включение этого заставляет приложения исполнителей сообщать своё положение,
	// что держит сохранённую позицию свежей для карты и автоподбора. Принимаются
	// только «1» и «0», чтобы его нельзя было включить опечаткой.
	if v, ok := settings["geofence_tracking_enabled"]; ok && v != "0" && v != "1" {
		return validationError("setting geofence_tracking_enabled must be 0 or 1")
	}
	// Назначает ли фоновый воркер заказы автоматически. По умолчанию выключено;
	// только «1» или «0», чтобы его нельзя было включить опечаткой.
	if v, ok := settings["auto_matching_enabled"]; ok && v != "0" && v != "1" {
		return validationError("setting auto_matching_enabled must be 0 or 1")
	}
	// Открывать ли смену за исполнителя, который берёт заказ без неё.
	if v, ok := settings[SettingAutoShiftOnAcceptEnabled]; ok && v != "0" && v != "1" {
		return validationError("setting " + SettingAutoShiftOnAcceptEnabled + " must be 0 or 1")
	}
	// Длительность такой смены ограничена тем же списком, что и ручной старт:
	// автоматика не должна уметь открыть смену, которую исполнителю выбрать не дают.
	if v, ok := settings[SettingAutoShiftDurationHours]; ok {
		hours, err := strconv.Atoi(v)
		if err != nil || !IsValidShiftDuration(hours) {
			return validationError(fmt.Sprintf("setting %s must be one of %v", SettingAutoShiftDurationHours, ShiftDurationsHours))
		}
	}
	// Магазин открывается и закрывается только явным «1» или «0»: опечатка не
	// должна ни открыть витрину, ни закрыть её посреди дня.
	if v, ok := settings[SettingShopEnabled]; ok && v != "0" && v != "1" {
		return validationError("setting " + SettingShopEnabled + " must be 0 or 1")
	}
	// Редакция оферты — целое число от 1: покупка сверяет её с той, что
	// принял покупатель, и «1.5» или «0» не совпали бы ни с одной.
	if v, ok := settings[SettingShopOfferVersion]; ok {
		if n, err := strconv.Atoi(v); err != nil || n < 1 {
			return validationError("setting " + SettingShopOfferVersion + " must be a positive integer")
		}
	}
	numericKeys["reject_penalty_share"] = true
	// Доля платформы с завершённого заказа. Снизу ограничена вместе с прочими
	// числовыми настройками, а сверху — прямо здесь, потому что доля выше 100%
	// платила бы исполнителю отрицательное вознаграждение.
	numericKeys[SettingOrderCommissionPercent] = true
	// Настройки геймификации. Каждая из них так или иначе превращается в деньги:
	// шаг скидки за уровень — прямо, потолки — тем, насколько дорого обойдётся
	// ошибка. Поэтому у всех есть границы, а не только неотрицательность.
	numericKeys[SettingAchievementLevelPoints] = true
	numericKeys[SettingAchievementLevelDiscountPP] = true
	numericKeys[SettingAchievementDefaultWeight] = true
	numericKeys[SettingAchievementMaxPointsPerDay] = true
	numericKeys[SettingAchievementMaxBonus] = true
	numericKeys[SettingAchievementMinOrderAmount] = true
	positiveIntKeys := map[string]bool{
		"executor_location_send_interval_seconds": true,
		"max_active_orders":                       true,
		"max_executed_unconfirmed_orders":         true,
	}
	for key, value := range settings {
		if err := validatePenaltySetting(key, value); err != nil {
			return validationError(err.Error())
		}
		if numericKeys[key] {
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return validationError("setting " + key + " must be numeric")
			}
			if v < 0 {
				return validationError("setting " + key + " value cannot be negative")
			}
			if key == "reject_penalty_share" && v > 1 {
				return validationError("setting reject_penalty_share must be between 0 and 1")
			}
			// Радиусы обязаны быть положительными. Ноль читался бы кодом как «не
			// задано» и молча уводил на умолчание — то есть поле показывало бы 0,
			// а действовало бы 0.5. Настройка, которой нельзя верить на слово,
			// хуже отсутствующей, поэтому ноль отвергается сразу.
			if (key == SettingAcceptRadiusKM || key == SettingMapOverviewRadiusKM) && v <= 0 {
				return validationError("setting " + key + " must be greater than zero")
			}
			// Верхняя граница обзора: запрос читает заказы в круге, и радиус в
			// тысячу километров превратил бы экран, открытый у каждого
			// исполнителя, в чтение всей таблицы заказов. Нижнюю границу здесь не
			// проверяем — радиусы можно менять по одному, и пара «обзор меньше
			// взятия» на мгновение допустима; на чтении resolveMapOverviewRadiusKM
			// всё равно поднимет обзор до зоны взятия.
			if key == SettingMapOverviewRadiusKM && v > maxMapOverviewRadiusKM {
				return validationError(fmt.Sprintf("setting %s must not exceed %.0f km", key, maxMapOverviewRadiusKM))
			}
			if key == SettingOrderCommissionPercent && v > 100 {
				return validationError("setting " + SettingOrderCommissionPercent + " must be between 0 and 100")
			}
			// Шаг скидки за уровень выше базовой ставки означал бы, что первый же
			// уровень обнуляет комиссию, а второй уводит её в минус. Зажим в
			// расчёте это переживёт, но настройка, которую зажимают молча, —
			// это настройка, которой никто не верит.
			if key == SettingAchievementLevelDiscountPP && v > 100 {
				return validationError("setting " + SettingAchievementLevelDiscountPP + " must be between 0 and 100")
			}
			// Ноль баллов на уровень — это деление на ноль в буквальном смысле:
			// любой набор баллов давал бы бесконечный уровень.
			if key == SettingAchievementLevelPoints && v < 1 {
				return validationError("setting " + SettingAchievementLevelPoints + " must be at least 1")
			}
		}
		if positiveIntKeys[key] {
			v, err := strconv.Atoi(value)
			if err != nil {
				return validationError("setting " + key + " must be an integer")
			}
			if v < 1 {
				return validationError("setting " + key + " must be at least 1 second")
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

// GetCommission сообщает баланс счёта комиссии и текущую ставку. Ставка
// читается общим ридером настроек: отсутствующая или нечитаемая — ноль, не
// брать ничего — безопасное направление отказа.
func (s *AdminService) GetCommission(ctx context.Context) (*Commission, error) {
	if s.ledger == nil {
		return nil, notConfigured("ledger")
	}
	account, err := s.ledger.AccountBalance(ctx, repository.AccountCommission)
	if err != nil {
		return nil, err
	}
	percent := settingFloat(ctx, s.settingsRepo, SettingOrderCommissionPercent, 0)
	return &Commission{Balance: account.Balance, Percent: percent}, nil
}

// PayoutCommission выводит собранную комиссию из системы. Сюда дотягивается
// только носитель права commission.edit, и он записывается в проводку,
// поэтому у выплаты всегда есть имя.
//
// Списание охраняется балансом счёта, поэтому два админа, выплачивающих
// одновременно, не могут вместе вывести больше, чем собрано.
func (s *AdminService) PayoutCommission(ctx context.Context, adminID uuid.UUID, amount money.Amount) (*Commission, error) {
	if !amount.IsPositive() {
		return nil, ErrAmountNotPositive
	}
	if s.ledger == nil {
		return nil, notConfigured("ledger")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.ledger.Payout(ctx, tx, repository.AccountCommission, adminID, amount,
			repository.TransactionTypeCommissionPayout, &adminID)
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, ErrCommissionLow
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
	// IncludeUnverified добавляет адреса, по ссылке подтверждения которых не
	// переходили. По умолчанию выключено: см. repository.BroadcastEmails.
	IncludeUnverified bool `json:"include_unverified"`
}

// BroadcastEmailResult содержит сводку об отправленных письмах.
type BroadcastEmailResult struct {
	Total      int      `json:"total"`
	Successful int      `json:"successful"`
	Failed     int      `json:"failed"`
	Failures   []string `json:"failures,omitempty"`
}

// SendBroadcastEmail рассылает письма выбранным группам пользователей или
// произвольному списку получателей. Без транспорта — ErrNotConfigured, и ни
// одно письмо не считается отправленным.
func (s *AdminService) SendBroadcastEmail(ctx context.Context, req BroadcastEmailRequest) (*BroadcastEmailResult, error) {
	req.Subject = strings.TrimSpace(req.Subject)
	req.BodyHTML = strings.TrimSpace(req.BodyHTML)
	if req.Subject == "" || req.BodyHTML == "" {
		return nil, validationError("subject and body_html are required")
	}

	var recipientEmails []string
	switch strings.ToUpper(req.TargetGroup) {
	case "CUSTOMERS", "EXECUTORS":
		role := repository.RoleCustomer
		if strings.ToUpper(req.TargetGroup) == "EXECUTORS" {
			role = repository.RoleExecutor
		}
		emails, err := s.adminUsers.BroadcastEmails(ctx, role, req.IncludeUnverified)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve recipients: %w", err)
		}
		if len(emails) == 0 && !req.IncludeUnverified {
			// Самый частый случай пустой рассылки — адреса есть, но их никто не
			// подтвердил: ссылка живёт час, и по ней переходят не все. Отказ
			// обязан это сказать, иначе он выглядит как сломанная рассылка.
			all, err := s.adminUsers.BroadcastEmails(ctx, role, true)
			if err != nil {
				return nil, fmt.Errorf("cannot resolve recipients: %w", err)
			}
			if len(all) > 0 {
				return nil, validationError(fmt.Sprintf("подтверждённых адресов в группе нет: почта указана у %d, но не подтверждена. "+
					"Отметьте «Включая неподтверждённые адреса», чтобы отправить им", len(all)))
			}
		}
		recipientEmails = emails
	case "CUSTOM_EMAILS":
		for _, email := range req.CustomEmails {
			trimmed := strings.TrimSpace(email)
			if trimmed == "" {
				continue
			}
			// Отвергаем всё, что не является обычным адресом: заголовки письма
			// собираются конкатенацией, поэтому CR/LF здесь — инъекция заголовков.
			if !validRecipient.MatchString(trimmed) {
				return nil, validationError("invalid recipient address: " + trimmed)
			}
			recipientEmails = append(recipientEmails, trimmed)
		}
		if len(recipientEmails) == 0 {
			return nil, validationError("список адресов пуст: укажите хотя бы один адрес")
		}
	default:
		return nil, validationError("invalid target_group: must be CUSTOMERS, EXECUTORS, or CUSTOM_EMAILS")
	}

	if len(recipientEmails) == 0 {
		return nil, validationError("у выбранной группы нет ни одного адреса электронной почты")
	}

	if s.mailer == nil {
		// Без настоящего транспорта ничего не отправляется; отчёт об успехе был бы ложью.
		return nil, ErrMailerNotConfigured
	}

	result := &BroadcastEmailResult{
		Total: len(recipientEmails),
	}
	for _, email := range recipientEmails {
		err := s.mailer.SendEmail(email, req.Subject, req.BodyHTML)
		if err != nil {
			result.Failed++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", email, err))
		} else {
			result.Successful++
		}
	}

	return result, nil
}
