package repository_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// explain отдаёт план запроса при выключенном seqscan: на маленькой тестовой
// таблице планировщик иначе всегда выберет последовательное чтение, и по плану
// нельзя было бы понять, пригоден ли индекс вообще.
func explain(t *testing.T, db *sql.DB, query string, args ...interface{}) string {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := conn.QueryContext(ctx, "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return plan.String()
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// --- users --------------------------------------------------------------------

func TestUserLookupsShareOneColumnList(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.New(db)

	id := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	email := "Mixed.Case+" + uuid.New().String()[:8] + "@Example.COM"
	// Все поля, которые раньше читались не каждым FindBy*: pending_email,
	// is_checked, согласие на ПД и набор ролей.
	mustExec(t, db, `UPDATE users SET email = LOWER($2), pending_email = 'next@example.com', is_checked = true,
		pd_consent_version = 3, pd_consent_at = now(), birth_date = '1990-05-17' WHERE id = $1`, id, email)
	if err := repo.SetUserRoles(ctx, id, []string{"CUSTOMER", "EXECUTOR"}); err != nil {
		t.Fatalf("set roles: %v", err)
	}

	byID, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.PendingEmail != "next@example.com" || !byID.Checked || byID.PDConsentVersion == nil || *byID.PDConsentVersion != 3 ||
		byID.PDConsentAt == nil || byID.BirthDate == nil || !reflect.DeepEqual(byID.Roles, []string{"CUSTOMER", "EXECUTOR"}) {
		t.Fatalf("FindByID returned an incomplete user: %+v", byID)
	}

	byPhone, err := repo.FindByPhone(ctx, byID.Phone)
	if err != nil {
		t.Fatalf("FindByPhone: %v", err)
	}
	// Регистр ввода не важен: адрес хранится в нижнем регистре, ищут по LOWER.
	byEmail, err := repo.FindByEmail(ctx, "  "+strings.ToUpper(email)+" ")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	batch, err := repo.FindByIDs(ctx, []uuid.UUID{id})
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	for name, u := range map[string]*repository.User{"FindByPhone": byPhone, "FindByEmail": byEmail, "FindByIDs": batch[id]} {
		if !reflect.DeepEqual(u, byID) {
			t.Errorf("%s differs from FindByID:\n got %+v\nwant %+v", name, u, byID)
		}
	}

	// Пользователь без строк в user_roles всё равно несёт основную роль.
	mustExec(t, db, `DELETE FROM user_roles WHERE user_id = $1`, id)
	bare, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID after roles removed: %v", err)
	}
	if !reflect.DeepEqual(bare.Roles, []string{bare.Role}) {
		t.Errorf("roles fallback = %v, want the primary role %q only", bare.Roles, bare.Role)
	}
}

func TestFindByPhoneIsExactAndIndexed(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.New(db)

	id := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	var phone string
	if err := db.QueryRow(`SELECT phone FROM users WHERE id = $1`, id).Scan(&phone); err != nil {
		t.Fatalf("read phone: %v", err)
	}

	if u, err := repo.FindByPhone(ctx, phone); err != nil || u.ID != id {
		t.Fatalf("exact phone: user %v, err %v", u, err)
	}
	// Сырой ввод больше не подбирается по цифрам: нормализация — дело сервиса.
	if _, err := repo.FindByPhone(ctx, strings.TrimPrefix(phone, "+")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("raw phone: err = %v, want sql.ErrNoRows", err)
	}

	plan := explain(t, db, repository.FindByPhoneSQL, phone)
	t.Logf("FindByPhone plan:\n%s", plan)
	if !strings.Contains(plan, "users_phone_key") {
		t.Errorf("FindByPhone does not use the phone unique index:\n%s", plan)
	}
}

func TestFindByEmailIsCaseInsensitiveAndIndexed(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.New(db)

	id := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	email := "case-" + uuid.New().String()[:8] + "@example.com"
	mustExec(t, db, `UPDATE users SET email = $2 WHERE id = $1`, id, email)

	for _, input := range []string{email, strings.ToUpper(email), " " + email + " "} {
		u, err := repo.FindByEmail(ctx, input)
		if err != nil || u.ID != id {
			t.Fatalf("FindByEmail(%q): user %v, err %v", input, u, err)
		}
	}

	// Уникальность теперь тоже без учёта регистра: второй адрес, отличающийся
	// только регистром, база отвергает.
	other := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, other) })
	if _, err := db.Exec(`UPDATE users SET email = $2 WHERE id = $1`, other, strings.ToUpper(email)); err == nil {
		t.Fatal("a case-variant duplicate email was accepted")
	}

	plan := explain(t, db, repository.FindByEmailSQL, email)
	t.Logf("FindByEmail plan:\n%s", plan)
	if !strings.Contains(plan, "idx_users_email_lower") {
		t.Errorf("FindByEmail does not use the LOWER(email) index:\n%s", plan)
	}

	// Сброс пароля ищет по тому же выражению.
	mustExec(t, db, `UPDATE users SET password_reset_code = '12345678', password_reset_expires_at = now() + interval '1 hour' WHERE id = $1`, id)
	reset, err := repo.ResetPasswordWithCode(ctx, strings.ToUpper(email), "12345678", "newhash")
	if err != nil || reset.ID != id || reset.Password != "newhash" {
		t.Fatalf("ResetPasswordWithCode: user %+v, err %v", reset, err)
	}
}

func TestUpdateUserEmailReturnsFullUser(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.New(db)

	id := createTestUser(t, db, "EXECUTOR")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	mustExec(t, db, `UPDATE users SET is_checked = true WHERE id = $1`, id)

	u, err := repo.UpdateUserEmail(ctx, id, "New.Address@Example.com", "token-"+id.String(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("UpdateUserEmail: %v", err)
	}
	if u.PendingEmail != "new.address@example.com" {
		t.Errorf("pending email = %q, want lower-cased", u.PendingEmail)
	}
	if !u.Checked || !reflect.DeepEqual(u.Roles, []string{"EXECUTOR"}) || u.Password == "" {
		t.Errorf("UpdateUserEmail returned a partial user: %+v", u)
	}
	if _, err := repo.UpdateUserEmail(ctx, uuid.New(), "x@example.com", "t", time.Now()); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unknown user: err = %v, want sql.ErrNoRows", err)
	}
}

func TestSetUserRolesWritesTheSetAtOnce(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.New(db)

	id := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })

	if err := repo.SetUserRoles(ctx, id, []string{"MODERATOR", "", "EXECUTOR", "EXECUTOR"}); err != nil {
		t.Fatalf("SetUserRoles: %v", err)
	}
	u, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !reflect.DeepEqual(u.Roles, []string{"EXECUTOR", "MODERATOR"}) {
		t.Errorf("roles = %v, want [EXECUTOR MODERATOR]", u.Roles)
	}
	// Основная роль ушла из набора — она перенаправлена на первую из нового.
	if u.Role != "MODERATOR" {
		t.Errorf("primary role = %q, want MODERATOR", u.Role)
	}
}

// --- chat ---------------------------------------------------------------------

// seedOrderChat создаёт заказ с заказчиком и исполнителем и чат по нему.
func seedOrderChat(t *testing.T, db *sql.DB) (customerID, executorID, orderID, chatID uuid.UUID) {
	t.Helper()
	customerID, _, orderID = seedBehaviorOrder(t, db)
	executorID = createTestUser(t, db, "EXECUTOR")
	mustExec(t, db, `UPDATE orders SET executor_id = $2, status = 'ASSIGNED' WHERE id = $1`, orderID, executorID)
	if err := db.QueryRow(`INSERT INTO chats (order_id) VALUES ($1) RETURNING id`, orderID).Scan(&chatID); err != nil {
		t.Fatalf("create chat: %v", err)
	}
	return customerID, executorID, orderID, chatID
}

func TestGetUnreadOrderIDs(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewChatRepository(db)

	customerID, executorID, orderID, chatID := seedOrderChat(t, db)
	contains := func(ids []uuid.UUID, id uuid.UUID) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	unreadFor := func(userID uuid.UUID) []uuid.UUID {
		t.Helper()
		ids, err := repo.GetUnreadOrderIDs(ctx, userID)
		if err != nil {
			t.Fatalf("GetUnreadOrderIDs: %v", err)
		}
		return ids
	}

	if ids := unreadFor(customerID); contains(ids, orderID) {
		t.Fatal("an order without messages is reported unread")
	}
	// Сообщение исполнителя: непрочитанное для заказчика, но не для автора.
	mustExec(t, db, `INSERT INTO messages (chat_id, sender_id, text, status) VALUES ($1, $2, 'hi', 'sent')`, chatID, executorID)
	if ids := unreadFor(customerID); !contains(ids, orderID) {
		t.Errorf("customer does not see the executor's message as unread: %v", ids)
	}
	if ids := unreadFor(executorID); contains(ids, orderID) {
		t.Errorf("the sender sees their own message as unread: %v", ids)
	}
	if _, err := repo.MarkMessagesAsRead(ctx, chatID, customerID); err != nil {
		t.Fatalf("MarkMessagesAsRead: %v", err)
	}
	if ids := unreadFor(customerID); contains(ids, orderID) {
		t.Errorf("a read message is still reported unread: %v", ids)
	}

	plan := explain(t, db, repository.UnreadOrderIDsSQL, customerID)
	t.Logf("GetUnreadOrderIDs plan:\n%s", plan)
	if !strings.Contains(plan, "idx_messages_chat_unread") {
		t.Errorf("the EXISTS probe does not use the unread partial index:\n%s", plan)
	}
	if !strings.Contains(plan, "idx_orders_customer") && !strings.Contains(plan, "idx_orders_executor") {
		t.Errorf("the query does not start from the user's orders:\n%s", plan)
	}
}

func TestGetOrCreateSupportChatDoesNotWriteOnRead(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewChatRepository(db)

	userID := createTestUser(t, db, "CUSTOMER")
	adminID := createTestUser(t, db, "ADMIN")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, userID, adminID) })

	first, err := repo.GetOrCreateSupportChat(ctx, userID)
	if err != nil {
		t.Fatalf("first GetOrCreateSupportChat: %v", err)
	}
	rowVersion := func() string {
		t.Helper()
		var xmin string
		if err := db.QueryRow(`SELECT xmin::text FROM support_chats WHERE id = $1`, first.ID).Scan(&xmin); err != nil {
			t.Fatalf("read xmin: %v", err)
		}
		return xmin
	}
	before := rowVersion()

	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text) VALUES ($1, $2, 'admin reply')`, first.ID, adminID)
	second, err := repo.GetOrCreateSupportChat(ctx, userID)
	if err != nil {
		t.Fatalf("second GetOrCreateSupportChat: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second call created another chat: %s vs %s", second.ID, first.ID)
	}
	if second.UnreadCount != 1 || second.LastMessage == nil || *second.LastMessage != "admin reply" {
		t.Errorf("chat = %+v, want unread 1 and last message 'admin reply'", second)
	}
	// Открытие чата — чтение: строка не переписана.
	if after := rowVersion(); after != before {
		t.Errorf("support_chats row was rewritten on a plain read: xmin %s -> %s", before, after)
	}
}

func TestGetAdminSupportChatList(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewChatRepository(db)

	adminID := createTestUser(t, db, "ADMIN")
	quietUser := createTestUser(t, db, "CUSTOMER")
	busyUser := createTestUser(t, db, "EXECUTOR")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id IN ($1, $2, $3)`, adminID, quietUser, busyUser) })
	mustExec(t, db, `UPDATE users SET last_name = 'Иванов', first_name = 'Иван' WHERE id = $1`, busyUser)

	quiet, err := repo.GetOrCreateSupportChat(ctx, quietUser)
	if err != nil {
		t.Fatalf("quiet chat: %v", err)
	}
	busy, err := repo.GetOrCreateSupportChat(ctx, busyUser)
	if err != nil {
		t.Fatalf("busy chat: %v", err)
	}
	// У тихого чата одно старое сообщение, у занятого — два от пользователя
	// (непрочитанные админом), одно прочитанное и последнее — от админа.
	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text, created_at) VALUES ($1, $2, 'old', now() - interval '2 hours')`, quiet.ID, quietUser)
	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text, created_at, read_at) VALUES ($1, $2, 'seen', now() - interval '3 minutes', now())`, busy.ID, busyUser)
	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text, created_at) VALUES ($1, $2, 'one', now() - interval '2 minutes')`, busy.ID, busyUser)
	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text, created_at) VALUES ($1, $2, 'two', now() - interval '1 minute')`, busy.ID, busyUser)
	mustExec(t, db, `INSERT INTO support_messages (chat_id, sender_id, text, file_name, created_at) VALUES ($1, $2, '', 'photo.jpg', now())`, busy.ID, adminID)

	items, err := repo.GetAdminSupportChatList(ctx, repository.MaxHistoryPageSize)
	if err != nil {
		t.Fatalf("GetAdminSupportChatList: %v", err)
	}
	pos := map[uuid.UUID]int{}
	byID := map[uuid.UUID]*repository.SupportChatListItem{}
	for i, it := range items {
		pos[it.ChatID] = i
		byID[it.ChatID] = it
	}
	b, q := byID[busy.ID], byID[quiet.ID]
	if b == nil || q == nil {
		t.Fatalf("both chats must be listed; busy=%v quiet=%v", b != nil, q != nil)
	}
	if pos[busy.ID] > pos[quiet.ID] {
		t.Errorf("the chat with the newer message must come first")
	}
	if b.UnreadCount != 2 {
		t.Errorf("busy unread = %d, want 2 (only the user's unread messages)", b.UnreadCount)
	}
	if b.LastMessage == nil || *b.LastMessage != "photo.jpg" || b.LastTime == nil {
		t.Errorf("busy last message = %v, want the attachment name", b.LastMessage)
	}
	if b.FullName != "Иванов Иван" {
		t.Errorf("busy full name = %q", b.FullName)
	}
	if q.FullName != q.Phone {
		t.Errorf("quiet full name = %q, want the phone fallback", q.FullName)
	}
	if q.UnreadCount != 1 || q.LastMessage == nil || *q.LastMessage != "old" {
		t.Errorf("quiet item = %+v", q)
	}
}

// --- batched writes -----------------------------------------------------------

func TestSetPermissionsBatch(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewRoleRepository(db)

	code := sanitizeRoleCode("batch_" + uuid.New().String()[:8])
	if err := repo.Create(ctx, &repository.Role{Code: code, Name: "Пакетная роль"}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM roles WHERE code = $1`, code) })

	if err := repo.SetPermissions(ctx, code, []string{"orders.read", " ", "orders.write", "orders.read"}); err != nil {
		t.Fatalf("SetPermissions: %v", err)
	}
	perms, err := repo.PermissionsByRole(ctx)
	if err != nil {
		t.Fatalf("PermissionsByRole: %v", err)
	}
	if !reflect.DeepEqual(perms[code], []string{"orders.read", "orders.write"}) {
		t.Errorf("permissions = %v", perms[code])
	}
	if err := repo.SetPermissions(ctx, code, nil); err != nil {
		t.Fatalf("clear permissions: %v", err)
	}
	if perms, _ := repo.PermissionsByRole(ctx); len(perms[code]) != 0 {
		t.Errorf("permissions after clear = %v", perms[code])
	}
	if err := repo.SetPermissions(ctx, "no-such-role-"+code, []string{"x"}); !errors.Is(err, repository.ErrRoleNotFound) {
		t.Errorf("unknown role: err = %v", err)
	}
}

func TestAddCodesBatch(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewGiftRepository(db)

	code := "batch-" + uuid.New().String()[:8]
	seedShopGift(t, db, code, repository.GiftKindCertificate, nil)

	added, err := repo.AddCodes(ctx, code, []string{"A1", "", "B2", "A1"})
	if err != nil {
		t.Fatalf("AddCodes: %v", err)
	}
	if added != 2 {
		t.Errorf("added = %d, want 2 (blank and duplicate skipped)", added)
	}
	// Повтор уже загруженного файла ничего не добавляет.
	added, err = repo.AddCodes(ctx, code, []string{"A1", "B2", "C3"})
	if err != nil || added != 1 {
		t.Errorf("second load: added = %d, err = %v; want 1", added, err)
	}
	if free, _ := repo.CountFreeCodes(ctx, code); free != 3 {
		t.Errorf("free codes = %d, want 3", free)
	}
}

func TestUpdateSettingsBatch(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewSettingsRepository(db)

	prefix := "test_batch_" + uuid.New().String()[:8] + "_"
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM system_settings WHERE key LIKE $1`, prefix+"%") })

	if err := repo.UpdateSettings(ctx, map[string]string{prefix + "a": "1", prefix + "b": "two"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if err := repo.UpdateSettings(ctx, map[string]string{prefix + "a": "3"}); err != nil {
		t.Fatalf("UpdateSettings (upsert): %v", err)
	}
	if err := repo.UpdateSettings(ctx, nil); err != nil {
		t.Fatalf("UpdateSettings (empty): %v", err)
	}
	all, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if all[prefix+"a"] != "3" || all[prefix+"b"] != "two" {
		t.Errorf("settings = a:%q b:%q", all[prefix+"a"], all[prefix+"b"])
	}
}

func TestListEscalationsLoadsSubmissionsInOneQuery(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewSubmissionRepository(db)

	_, _, orderA := seedBehaviorOrder(t, db)
	_, _, orderB := seedBehaviorOrder(t, db)
	executorID := createTestUser(t, db, "EXECUTOR")
	for _, rec := range []struct {
		order uuid.UUID
		field string
	}{{orderA, "a1"}, {orderA, "a2"}, {orderB, "b1"}} {
		if err := repo.Record(ctx, nil, &repository.OrderSubmission{
			OrderID: rec.order, ExecutorID: executorID, Fields: map[string]string{"last_name": rec.field},
		}); err != nil {
			t.Fatalf("record %s: %v", rec.field, err)
		}
	}
	code := "batch-" + uuid.New().String()[:8]
	for _, order := range []uuid.UUID{orderA, orderB} {
		if err := repo.Escalate(ctx, nil, &repository.BehaviorEscalation{OrderID: order, BehaviorCode: code, Reason: "test"}); err != nil {
			t.Fatalf("escalate: %v", err)
		}
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM behavior_escalations WHERE behavior_code = $1`, code) })

	list, err := repo.ListEscalations(ctx, repository.EscalationOpen, repository.MaxHistoryPageSize)
	if err != nil {
		t.Fatalf("ListEscalations: %v", err)
	}
	got := map[uuid.UUID][]string{}
	for _, e := range list {
		if e.BehaviorCode != code {
			continue
		}
		for _, s := range e.Submissions {
			got[e.OrderID] = append(got[e.OrderID], s.Fields["last_name"])
		}
	}
	if !reflect.DeepEqual(got[orderA], []string{"a1", "a2"}) || !reflect.DeepEqual(got[orderB], []string{"b1"}) {
		t.Errorf("submissions per order = %v", got)
	}
}

// --- orders / transactions ----------------------------------------------------

func TestFindByCustomerIsLimited(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewOrderRepository(db)

	customerID, variantID, _ := seedBehaviorOrder(t, db)
	for i := 0; i < 4; i++ {
		mustExec(t, db, `INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount, final_amount, created_at)
			 VALUES ($1, $2, $3, 'SEARCHING', 0, 0, now() + ($4 || ' minutes')::interval)`, uuid.New(), customerID, variantID, i+1)
	}
	page, err := repo.FindByCustomer(ctx, customerID, 2)
	if err != nil {
		t.Fatalf("FindByCustomer: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("limit 2 returned %d orders", len(page))
	}
	if page[0].CreatedAt.Before(page[1].CreatedAt) {
		t.Errorf("orders are not newest-first")
	}
	all, err := repo.FindByCustomer(ctx, customerID, 0)
	if err != nil || len(all) != 5 {
		t.Errorf("default page: %d orders, err %v; want 5", len(all), err)
	}
	viaForwarder, err := repo.GetCustomerOrders(ctx, customerID)
	if err != nil || len(viaForwarder) != 5 {
		t.Errorf("GetCustomerOrders: %d orders, err %v; want 5", len(viaForwarder), err)
	}
}

func TestTransactionPeriodFilterAndScan(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	admin := repository.NewAdminRepository(db)
	txRepo := repository.NewTransactionRepository(db)

	userID := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})
	var account string
	if err := db.QueryRow(`SELECT code FROM system_accounts ORDER BY code LIMIT 1`).Scan(&account); err != nil {
		t.Fatalf("system account: %v", err)
	}
	march, april := uuid.New(), uuid.New()
	mustExec(t, db, `INSERT INTO transactions (id, user_id, type, amount, counterparty, created_at) VALUES
		($1, $3, 'TOP_UP', 100, $4, '2020-03-15T12:00:00Z'),
		($2, $3, 'FINE', 40, $4, '2020-04-15T12:00:00Z')`, march, april, userID, account)

	for _, tc := range []struct {
		period string
		want   int
	}{{"2020-03", 1}, {"2020-04", 0}, {"2020-3", 0}, {"", 1}} {
		txs, total, err := admin.GetTransactions(ctx, repository.TransactionsFilter{Search: march.String(), Period: tc.period, Limit: 10})
		if err != nil {
			t.Fatalf("period %q: %v", tc.period, err)
		}
		if len(txs) != tc.want || total != tc.want {
			t.Errorf("period %q: %d rows, total %d; want %d", tc.period, len(txs), total, tc.want)
		}
		if tc.want == 1 && (txs[0].Direction != 1 || txs[0].Counterparty != account) {
			t.Errorf("period %q: scanned %+v", tc.period, txs[0])
		}
	}

	// История пользователя отдаёт тот же payload, что и админский журнал.
	history, err := txRepo.GetTransactionsByUserID(ctx, userID, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("GetTransactionsByUserID: %d rows, err %v", len(history), err)
	}
	if history[0].ID != april || history[0].Direction != -1 || history[0].Counterparty != account {
		t.Errorf("history[0] = %+v, want the FINE with direction -1 and counterparty", history[0])
	}
	adminView, _, err := admin.GetUserTransactions(ctx, userID, 10, 0)
	if err != nil {
		t.Fatalf("GetUserTransactions: %v", err)
	}
	if adminView[0].UserPhone == "" || adminView[0].Amount != money.FromRubles(40) {
		t.Errorf("admin view = %+v", adminView[0])
	}
}

func TestNotFoundErrorsWrapErrNotFound(t *testing.T) {
	for name, err := range map[string]error{
		"address":      repository.ErrAddressNotFound,
		"escalation":   repository.ErrEscalationNotFound,
		"role":         repository.ErrRoleNotFound,
		"perk":         repository.ErrPerkNotFound,
		"refreshToken": repository.ErrRefreshTokenNotFound,
		"passport":     repository.ErrPassportNotFound,
		"serviceNode":  repository.ErrServiceNodeNotFound,
		"perkRule":     repository.ErrPerkRuleNotFound,
		"shopProduct":  repository.ErrShopProductNotFound,
		"pickupPoint":  repository.ErrShopPickupPointNotFound,
		"shopOrder":    repository.ErrShopOrderNotFound,
	} {
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("%s: %v does not wrap ErrNotFound", name, err)
		}
	}
}
