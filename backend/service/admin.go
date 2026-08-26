package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// maxAdminPageSize caps admin listings so a single request cannot ask for the
// whole table.
const maxAdminPageSize = 200

// SessionRevoker ends every session of a user. Satisfied by *AuthService;
// AdminService only needs this much of it.
type SessionRevoker interface {
	RevokeAllSessions(userID uuid.UUID) error
}

// AdminService manages administrative business logic.
type AdminService struct {
	userRepo      repository.UserRepository
	adminRepo     repository.AdminRepository
	settingsRepo  repository.SettingsRepository
	addressRepo   repository.CustomerAddressRepository
	ledger        *Ledger
	reconcileRepo repository.ReconciliationRepository
	sessions      SessionRevoker
	mailer        MailSender
	jwtSecret     []byte
}

// NewAdminService creates a new AdminService.
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

// WithAddresses attaches the saved-address store used by the customer profile.
func (s *AdminService) WithAddresses(addressRepo repository.CustomerAddressRepository) *AdminService {
	s.addressRepo = addressRepo
	return s
}

// ListAddresses returns a customer's saved pickup addresses.
func (s *AdminService) ListAddresses(userID uuid.UUID) ([]repository.CustomerAddress, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.List(userID)
}

// AddAddress saves a new pickup address.
//
// The address arrives already split into its parts when it came from the
// suggestion list. A client that still sends a single line — the mobile builds
// in people's hands do — has it split here, so those keep working and are no
// longer held to the old numeric-house-number format.
func (s *AdminService) AddAddress(userID uuid.UUID, address Address) ([]repository.CustomerAddress, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	if err := address.Validate(); err != nil {
		return nil, err
	}
	return s.addressRepo.Add(userID, address.ToRecord())
}

// DeleteAddress removes one of the customer's addresses.
func (s *AdminService) DeleteAddress(userID, addressID uuid.UUID) ([]repository.CustomerAddress, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.Delete(userID, addressID)
}

// SetDefaultAddress marks which address new orders should start from.
func (s *AdminService) SetDefaultAddress(userID, addressID uuid.UUID) ([]repository.CustomerAddress, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.SetDefault(userID, addressID)
}

// SetDefaultAddressByValue is the same, for clients that identify an address by
// its text.
func (s *AdminService) SetDefaultAddressByValue(userID uuid.UUID, address string) ([]repository.CustomerAddress, error) {
	if s.addressRepo == nil {
		return nil, errors.New("address storage is not configured")
	}
	return s.addressRepo.SetDefaultByValue(userID, strings.TrimSpace(address))
}

// WithLedger attaches the ledger. Top-ups and withdrawals move money, and the
// ledger is the only thing that can move it.
func (s *AdminService) WithLedger(ledger *Ledger) *AdminService {
	s.ledger = ledger
	return s
}

// WithReconciliation enables the balance/ledger consistency report.
func (s *AdminService) WithReconciliation(repo repository.ReconciliationRepository) *AdminService {
	s.reconcileRepo = repo
	return s
}

// Reconcile compares every stored balance with the sum of that user's ledger
// entries. Read-only: a mismatch is reported, never silently corrected.
func (s *AdminService) Reconcile(tolerance money.Amount) (*repository.ReconciliationReport, error) {
	if s.reconcileRepo == nil {
		return nil, errors.New("reconciliation is not configured")
	}
	if tolerance.IsNegative() {
		tolerance = money.Zero
	}
	return s.reconcileRepo.Reconcile(tolerance)
}

// WithSessions lets the service end a user's sessions when their access is
// changed. Without it, a ban or a demotion would only take effect once the
// refresh token expires.
func (s *AdminService) WithSessions(sessions SessionRevoker) *AdminService {
	s.sessions = sessions
	return s
}

// revokeSessions ends every session of a user, logging but not failing on error:
// the access change itself has already been persisted.
func (s *AdminService) revokeSessions(userID uuid.UUID, reason string) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.RevokeAllSessions(userID); err != nil {
		log.Printf("[AUDIT] failed to end sessions of user %s after %s: %v", userID, reason, err)
	}
}

// GetUsers retrieves a list of users with filters and search.
//
// role and status are validated here rather than handed straight to the enum
// columns: an unexpected value used to surface as a database error and a 500,
// which is both a bad answer and a way to probe the schema.
func (s *AdminService) GetUsers(page, limit int, role, status, search string) ([]*repository.User, int, error) {
	if role != "" && role != "CUSTOMER" && role != "EXECUTOR" && role != "ADMIN" {
		return nil, 0, errors.New("invalid role filter")
	}
	if status != "" && status != "ACTIVE" && status != "BANNED" {
		return nil, 0, errors.New("invalid status filter")
	}
	if limit > maxAdminPageSize {
		limit = maxAdminPageSize
	}
	return s.adminRepo.GetUsers(page, limit, role, status, search)
}

// UpdateUserStatus updates user status (e.g., ACTIVE or BANNED).
func (s *AdminService) UpdateUserStatus(userID, adminID uuid.UUID, status string) error {
	if status != "ACTIVE" && status != "BANNED" {
		return errors.New("invalid status")
	}
	if status == "BANNED" && userID == adminID {
		return errors.New("нельзя заблокировать самого себя")
	}
	if err := s.userRepo.UpdateStatus(userID, status); err != nil {
		return err
	}
	if status == "BANNED" {
		// A ban has to end the sessions too: RequireAuth rejects the banned user
		// on the next request, but their refresh token would otherwise keep
		// minting access tokens.
		s.revokeSessions(userID, "ban")
	}
	log.Printf("[AUDIT] admin %s set status of user %s to %s", adminID, userID, status)
	return nil
}

// UpdateUserRole updates a user's role. Role changes take effect on the next
// request because authorization reads the role from the database.
func (s *AdminService) UpdateUserRole(userID, adminID uuid.UUID, role string) error {
	if role != "CUSTOMER" && role != "EXECUTOR" && role != "ADMIN" {
		return errors.New("invalid role")
	}

	current, err := s.userRepo.FindByID(userID)
	if err != nil {
		return errors.New("user not found")
	}
	if current.Role == "ADMIN" && role != "ADMIN" {
		if userID == adminID {
			return errors.New("нельзя снять роль администратора с самого себя")
		}
		admins, err := s.adminRepo.CountAdmins()
		if err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("нельзя снять роль с последнего администратора")
		}
	}

	if err := s.userRepo.UpdateRole(userID, role); err != nil {
		return err
	}
	// Authorization reads the role from the database on every request, so the
	// change is already effective; ending the sessions makes the client pick up
	// its new role instead of rendering a UI it can no longer use.
	s.revokeSessions(userID, "role change")
	log.Printf("[AUDIT] admin %s changed role of user %s: %s -> %s", adminID, userID, current.Role, role)
	return nil
}

// UpdateUserAddress updates a customer's pickup address (admin-only).
func (s *AdminService) UpdateUserAddress(userID uuid.UUID, address string) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("address is required")
	}
	parsed := ParseAddressLine(address)
	if err := parsed.Validate(); err != nil {
		return err
	}
	normalizedAddress := parsed.Compose()
	if _, err := s.userRepo.FindByID(userID); err != nil {
		return errors.New("user not found")
	}
	return s.userRepo.UpdateCustomerAddress(userID, normalizedAddress)
}

// UpdateUserName updates a user's full name (admin-only).
func (s *AdminService) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error {
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	patronymic = strings.TrimSpace(patronymic)
	if lastName == "" || firstName == "" || patronymic == "" {
		return errors.New("last_name, first_name and patronymic are required")
	}
	if _, err := s.userRepo.FindByID(userID); err != nil {
		return errors.New("user not found")
	}
	return s.userRepo.UpdateUserName(userID, lastName, firstName, patronymic)
}

// TopUpUserBalance adds funds directly to a user's balance.
// Only non-admin users may be topped up, and an admin cannot credit themselves.
func (s *AdminService) TopUpUserBalance(userID, adminID uuid.UUID, amount money.Amount) error {
	if !amount.IsPositive() {
		return errors.New("amount must be greater than zero")
	}

	if userID == adminID {
		return errors.New("admin cannot top up their own balance")
	}

	// Verify user exists and is not an admin
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return errors.New("user not found")
	}
	if user.Role == "ADMIN" {
		return errors.New("cannot top up an admin balance")
	}

	return s.adminRepo.TopUpUserBalance(userID, adminID, amount)
}

// page normalises a requested page size. Admin listings used to return whole
// tables, which is both a slow response and an easy way to strain the database.
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

// GetTopUpRequests lists balance top-up requests, newest first.
func (s *AdminService) GetTopUpRequests(limit, offset int) ([]*repository.TopUpRequest, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetTopUpRequests(limit, offset)
}

// CreateTopUpRequest creates a pending balance top-up request.
func (s *AdminService) CreateTopUpRequest(userID uuid.UUID, amount money.Amount) (*repository.TopUpRequest, error) {
	if !amount.IsPositive() {
		return nil, errors.New("amount must be greater than zero")
	}

	// Verify user exists
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.Status == "BANNED" {
		return nil, errors.New("cannot request top-up for a banned user")
	}

	return s.adminRepo.CreateTopUpRequest(nil, userID, amount)
}

// ApproveTopUpRequest credits the requested amount to the user.
//
// The money comes in from the DEPOSITS account, which represents the outside
// world: a top-up used to make a balance grow with nothing on the other side.
func (s *AdminService) ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideTopUp(requestID, adminID, "APPROVED")
}

// RejectTopUpRequest closes a request without moving money.
func (s *AdminService) RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideTopUp(requestID, adminID, "REJECTED")
}

func (s *AdminService) decideTopUp(requestID, adminID uuid.UUID, status string) error {
	if s.ledger == nil {
		return errors.New("ledger is not configured")
	}

	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		req, err := s.adminRepo.LockTopUpRequest(tx, requestID)
		if err != nil {
			return errors.New("request not found")
		}
		if req.Status != "PENDING" {
			return errors.New("request is not in PENDING status")
		}
		if err := s.adminRepo.SetTopUpStatus(tx, requestID, adminID, status); err != nil {
			return err
		}
		if status != "APPROVED" {
			return nil
		}
		return s.ledger.Deposit(tx, req.UserID, req.Amount, &adminID)
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

// GetWithdrawalRequests lists all balance withdrawal requests.
func (s *AdminService) GetWithdrawalRequests(limit, offset int) ([]*repository.WithdrawalRequest, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetWithdrawalRequests(limit, offset)
}

// CreateWithdrawalRequest reserves the requested amount and records a pending
// request for it.
//
// The money is taken out of the balance immediately, exactly like an order hold.
// Previously a request only checked the balance and left the funds spendable, so
// a user could queue several requests against the same money and spend it while
// they waited — the payout queue then contained amounts that could not all be
// honoured.
func (s *AdminService) CreateWithdrawalRequest(userID uuid.UUID, amount money.Amount) (*repository.WithdrawalRequest, error) {
	if !amount.IsPositive() {
		return nil, errors.New("amount must be greater than zero")
	}
	if s.ledger == nil {
		return nil, errors.New("ledger is not configured")
	}

	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.Status == "BANNED" {
		return nil, errors.New("cannot request withdrawal for a banned user")
	}

	pending, err := s.adminRepo.HasPendingWithdrawal(userID)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, errors.New("у вас уже есть заявка на вывод в обработке")
	}

	var created *repository.WithdrawalRequest
	err = s.ledger.RunInTx(func(tx *sql.Tx) error {
		// Guarded debit: the balance has to cover the request at this moment,
		// not at some earlier read.
		// The money moves out of the balance and onto the payouts account, where
		// it waits for an admin decision.
		if err := s.ledger.Reserve(tx, userID, repository.AccountPayouts, amount, repository.TransactionTypeWithdrawalHold, nil); err != nil {
			return err
		}
		req, err := s.adminRepo.CreateWithdrawalRequest(tx, userID, amount)
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

// ApproveWithdrawalRequest marks a reserved withdrawal as paid out. No balance
// movement happens here: the money left the balance when the request was
// created, and this records that reservation being spent.
func (s *AdminService) ApproveWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideWithdrawal(requestID, adminID, "APPROVED")
}

// RejectWithdrawalRequest returns the reserved money to the user.
func (s *AdminService) RejectWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	return s.decideWithdrawal(requestID, adminID, "REJECTED")
}

func (s *AdminService) decideWithdrawal(requestID, adminID uuid.UUID, status string) error {
	if s.ledger == nil {
		return errors.New("ledger is not configured")
	}

	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		req, err := s.adminRepo.LockWithdrawalRequest(tx, requestID)
		if err != nil {
			return errors.New("request not found")
		}
		if req.Status != "PENDING" {
			return errors.New("request is not in PENDING status")
		}

		if err := s.adminRepo.SetWithdrawalStatus(tx, requestID, adminID, status); err != nil {
			return err
		}

		if status == "REJECTED" {
			// Give the reserved money back.
			return s.ledger.Release(tx, repository.AccountPayouts, req.UserID, req.Amount, repository.TransactionTypeRefund, nil, &adminID)
		}

		// Paid out: the reservation leaves the system through the account that
		// represents the outside world.
		return s.ledger.Settle(tx, repository.AccountPayouts, repository.AccountDeposits, req.UserID, req.Amount, repository.TransactionTypeWithdrawalPaid, &adminID)
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

// GetTransactions retrieves transaction history.
func (s *AdminService) GetTransactions(limit, offset int) ([]*repository.Transaction, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetTransactions(limit, offset)
}

// GetActiveShifts returns all currently active executor shifts.
func (s *AdminService) GetActiveShifts() ([]*repository.AdminShift, error) {
	return s.adminRepo.GetActiveShifts()
}

// GetActiveOrders returns customer orders that are still active (searching or assigned).
func (s *AdminService) GetActiveOrders(limit, offset int) ([]*repository.AdminOrder, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetActiveOrders(limit, offset)
}

// GetCompletedOrders returns completed customer orders.
func (s *AdminService) GetCompletedOrders(limit, offset int) ([]*repository.AdminOrder, error) {
	limit, offset = page(limit, offset)
	return s.adminRepo.GetCompletedOrders(limit, offset)
}

// GetProfile returns the authenticated user's profile including customer address.
func (s *AdminService) GetProfile(userID uuid.UUID) (map[string]interface{}, error) {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	user.Password = ""

	profile := map[string]interface{}{
		"id":         user.ID,
		"role":       user.Role,
		"phone":      user.Phone,
		"email":      user.Email,
		"balance":    user.Balance,
		"status":     user.Status,
		"created_at": user.CreatedAt,
		"first_name": user.FirstName,
		"last_name":  user.LastName,
		"patronymic": user.Patronymic,
		"address":    "",
	}

	cp, err := s.userRepo.GetCustomerProfile(userID)
	if err != nil {
		log.Printf("[GetProfile] failed to load customer profile for %s: %v", userID, err)
	} else if cp != nil {
		profile["address"] = cp.Address
		profile["last_geo"] = cp.LastGeo.String
	}

	// Saved addresses. "address" and "default_address" carry the same value so
	// that the dashboards, which read the former, keep working alongside the
	// profile page, which reads the latter.
	profile["addresses"] = []repository.CustomerAddress{}
	if s.addressRepo != nil {
		addresses, err := s.addressRepo.List(userID)
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

// GetSettings retrieves global settings.
func (s *AdminService) GetSettings() (map[string]string, error) {
	return s.settingsRepo.GetSettings()
}

// UpdateSettings updates global settings.
func (s *AdminService) UpdateSettings(settings map[string]string) error {
	// Numeric settings must be non-negative when applicable.
	numericKeys := map[string]bool{
		"standard_tariff_coeff":  true,
		"increased_tariff_coeff": true,
		"urgent_tariff_coeff":    true,
		"asap_tariff_coeff":      true,
		"geofence_fine_amount":   true,
		"min_balance_limit":      true,
	}
	numericKeys["shift_early_exit_penalty"] = true
	// Enabling this makes executor apps report their position, which is what
	// arms the geofence fine. Only "1" or "0" are accepted so it cannot be
	// switched on by a typo.
	if v, ok := settings["geofence_tracking_enabled"]; ok && v != "0" && v != "1" {
		return errors.New("setting geofence_tracking_enabled must be 0 or 1")
	}
	numericKeys["reject_penalty_share"] = true
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
	return s.settingsRepo.UpdateSettings(settings)
}

// BroadcastEmailRequest defines payload for email broadcast.
type BroadcastEmailRequest struct {
	TargetGroup  string   `json:"target_group"` // CUSTOMERS, EXECUTORS, CUSTOM_EMAILS
	CustomEmails []string `json:"custom_emails,omitempty"`
	Subject      string   `json:"subject"`
	BodyHTML     string   `json:"body_html"`
}

// BroadcastEmailResult contains summary of sent emails.
type BroadcastEmailResult struct {
	Total      int      `json:"total"`
	Successful int      `json:"successful"`
	Failed     int      `json:"failed"`
	Failures   []string `json:"failures,omitempty"`
}

// SendBroadcastEmail dispatches email broadcasts to selected user groups or custom recipient list.
func (s *AdminService) SendBroadcastEmail(req BroadcastEmailRequest) (*BroadcastEmailResult, error) {
	req.Subject = strings.TrimSpace(req.Subject)
	req.BodyHTML = strings.TrimSpace(req.BodyHTML)
	if req.Subject == "" || req.BodyHTML == "" {
		return nil, errors.New("subject and body_html are required")
	}

	var recipientEmails []string
	switch strings.ToUpper(req.TargetGroup) {
	case "CUSTOMERS":
		users, _, err := s.adminRepo.GetUsers(1, 10000, "CUSTOMER", "", "")
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.Email != "" && u.EmailVerified {
				recipientEmails = append(recipientEmails, u.Email)
			}
		}
	case "EXECUTORS":
		users, _, err := s.adminRepo.GetUsers(1, 10000, "EXECUTOR", "", "")
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
			// Reject anything that is not a plain address: the message headers
			// are built by concatenation, so a CR/LF here is header injection.
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
		// Without a real transport nothing is sent; reporting success would be a lie.
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
