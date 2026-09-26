package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Бизнес-умолчания. Каждое можно переопределить через system_settings, чтобы
// правила не были закопаны в коде магическими числами.
const (
	defaultMaxActiveOrders        = 3
	defaultMaxExecutedUnconfirmed = 6
	defaultRejectPenaltyShare     = 0.5
	defaultMinBalanceLimit        = 0.0
)

// ErrExecutorNotEligible сообщает, что исполнитель не может взять конкретный заказ.
var ErrExecutorNotEligible = errors.New("executor is not eligible for this order")

// ErrCustomerNotEligible сообщает, что заказчик не может заказать конкретный вариант услуги.
var ErrCustomerNotEligible = errors.New("customer is not eligible for this service")

// executorRefusal и customerRefusal — отказ с причиной, который обработчик
// всё равно читает как ErrExecutorNotEligible / ErrCustomerNotEligible.
func executorRefusal(msg string) error {
	return &DomainError{Kind: ErrExecutorNotEligible, Msg: msg}
}

func customerRefusal(msg string) error {
	return &DomainError{Kind: ErrCustomerNotEligible, Msg: msg}
}

// canCustomerOrderVariant — единственное место, решающее, может ли заказчик
// разместить заказ по варианту услуги. Вариант с флагом
// requires_verification может заказать только вручную верифицированный
// заказчик — зеркало canExecutorTakeOrder на стороне исполнителя. Он
// используется и при фильтрации каталога, и при собственно создании заказа,
// поэтому проверку нельзя обойти, отправив напрямую известный id варианта.
// Возраст (min_age) намеренно проверяется только на стороне исполнителя: он
// ограничивает, кто может выполнять работу, а не кто может её попросить.
//
// Вариант, управляемый скриптом поведения, получает хук can_order этого скрипта
// поверх этих правил, но никогда вместо них: скрипт может ограничить, кто
// заказывает услугу, но не может выдать освобождение от бана.
func canCustomerOrderVariant(ctx context.Context, behaviors *Behaviors, blocks penaltyGate, customer *repository.User, variant *repository.ServiceNode) error {
	if customer == nil {
		return ErrCustomerNotEligible
	}
	if customer.IsBlocked() {
		return customerRefusal("аккаунт заблокирован")
	}
	if silentlyBlocked(ctx, blocks, customer, repository.RoleCustomer) {
		return ErrCustomerNotEligible
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !customer.IsVerified() {
		return customerRefusal("для этой услуги требуется подтверждённый аккаунт")
	}
	// Аукцион — только для заказчика с подтверждённой личностью: заказ
	// неподтверждённого исполнители не увидят и ставку по нему не подадут
	// (canViewOrTakeOrder), поэтому создавать его незачем.
	if variant.IsAuction && !customer.IsVerified() {
		return customerRefusal("аукцион доступен только заказчикам с подтверждённой личностью")
	}
	if err := behaviors.CanOrder(ctx, customer, variant); err != nil {
		return behaviorRefusal(err, ErrCustomerNotEligible)
	}
	return nil
}

// behaviorRefusal классифицирует отказ скрипта: сам скрипт отдаёт текст без
// класса, а обработчику нужен класс. Недоступность скрипта — не отказ, а сбой,
// и она проходит как есть.
func behaviorRefusal(err error, kind error) error {
	if errors.Is(err, ErrBehaviorUnavailable) {
		return err
	}
	return &DomainError{Kind: kind, Msg: err.Error(), Cause: err}
}

// penaltyGate — тихая блокировка роли. Ему удовлетворяет *PenaltyService;
// предикатам допуска нужно ровно столько.
type penaltyGate interface {
	SilentlyBlocked(ctx context.Context, userID uuid.UUID, role string) bool
}

// silentlyBlocked — проверка тихой блокировки там, где она должна выглядеть как
// обычная недоступность. Без подключённого механизма штрафов — никогда.
func silentlyBlocked(ctx context.Context, blocks penaltyGate, user *repository.User, role string) bool {
	return blocks != nil && user != nil && blocks.SilentlyBlocked(ctx, user.ID, role)
}

// canExecutorTakeOrder — единственное место, решающее, позволено ли
// исполнителю работать по данному варианту услуги. Он используется и при
// фильтрации списков заказов, и когда исполнитель реально действует по заказу,
// поэтому ограничения нельзя обойти, вызвав эндпоинт напрямую с известным id
// заказа.
func canExecutorTakeOrder(executor *repository.User, variant *repository.ServiceNode) error {
	if executor == nil {
		return ErrExecutorNotEligible
	}
	if executor.IsBlocked() {
		return executorRefusal("аккаунт заблокирован")
	}
	if variant == nil {
		return nil
	}
	if variant.RequiresVerification && !executor.IsVerified() {
		return executorRefusal("для этого заказа требуется подтверждённый аккаунт")
	}
	if variant.MinAge > 0 && executor.GetAge() < variant.MinAge {
		return executorRefusal(fmt.Sprintf("для этого заказа требуется возраст не менее %d лет", variant.MinAge))
	}
	return nil
}

// canViewOrTakeOrder — единственный предикат, решающий, может ли смотрящий
// (исполнитель и/или модератор) и ВИДЕТЬ, и ПРИНЯТЬ данный заказ. Через него
// идут списки заказов (карта и таблица) и путь принятия, поэтому то, с чем
// исполнитель может действовать, никогда не расходится с показанным ему.
//
// viewerBlocked — тихая блокировка смотрящего в роли исполнителя, посчитанная
// вызывающим: списки считают её один раз на смотрящего, а раунд подбора — пакетом
// на всех кандидатов, вместо чтения по одному на каждую пару заказ×исполнитель.
//
// Правила:
//   - Аукцион: заказ неверифицированного заказчика (или без строки заказчика)
//     не виден никому, ставку по нему нельзя ни подать, ни принять.
//   - Услуга только для модераторов: видеть и брать заказ может только
//     MODERATOR; обычные проверки исполнителя не применяются (это доверенный персонал).
//   - Скриптовая услуга: что скажет хук can_view_or_take её поведения, поверх
//     правил ниже.
//   - Обычная услуга: применяются проверки исполнителя (бан,
//     requires_verification, min_age), а поверх них — сегментация по
//   - верификации заказчика: заказ неверифицированного заказчика виден всем
//     (именно это позволяет неверифицированному исполнителю работать с их пулом);
//   - заказ верифицированного заказчика виден только верифицированному
//     исполнителю или модератору.
func canViewOrTakeOrder(ctx context.Context, behaviors *Behaviors, viewerBlocked bool, viewer *repository.User, customer *repository.User, variant *repository.ServiceNode) error {
	if viewer == nil {
		return ErrExecutorNotEligible
	}
	// Тихая блокировка исполнителя: заказы ему просто не показываются, и взять
	// он их не может — тем же отказом, что и любой недоступный заказ.
	if viewerBlocked {
		return ErrExecutorNotEligible
	}
	// Аукцион открыт только по заказу верифицированного заказчика. Правило
	// одно для списка аукционов, подачи ставки и принятия ставки. Заказчик без
	// строки здесь не «нет сведений», а отказ: подтверждённость без строки не
	// установить.
	if variant != nil && variant.IsAuction && (customer == nil || !customer.IsVerified()) {
		return ErrAuctionCustomerNotVerified
	}
	if variant != nil && variant.ModeratorOnly {
		if !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
		if viewer.IsBlocked() {
			return executorRefusal("аккаунт заблокирован")
		}
		return behaviorGate(ctx, behaviors, viewer, customer, variant)
	}
	if err := canExecutorTakeOrder(viewer, variant); err != nil {
		return err
	}
	if customer != nil && customer.IsVerified() {
		if !viewer.IsVerified() && !viewer.HasRole(repository.RoleModerator) {
			return ErrExecutorNotEligible
		}
	}
	// Скрипт выполняется последним и может только сузить разрешённое встроенными правилами.
	// Услуга верификации пользуется этим, чтобы допускать одних модераторов.
	return behaviorGate(ctx, behaviors, viewer, customer, variant)
}

func behaviorGate(ctx context.Context, behaviors *Behaviors, viewer, customer *repository.User, variant *repository.ServiceNode) error {
	if err := behaviors.CanViewOrTake(ctx, viewer, customer, variant); err != nil {
		return behaviorRefusal(err, ErrExecutorNotEligible)
	}
	return nil
}

// eligibilityFor загружает исполнителя, вариант услуги и заказчика заказа —
// входные данные canViewOrTakeOrder — и применяет предикат. Это точка
// принятия для одного заказа: взятие, ставка и принятие ставки идут через неё,
// а списки грузят то же самое пакетом. Вариант возвращается вызывающему: ему
// он обычно нужен и дальше (аукцион ли это), а грузить дважды незачем.
func eligibilityFor(ctx context.Context, users repository.UserRepository, catalog repository.ServiceCatalogRepository,
	behaviors *Behaviors, blocks penaltyGate, executorID uuid.UUID, order *repository.Order) (*repository.ServiceNode, error) {
	viewer, err := users.FindByID(ctx, executorID)
	if err != nil {
		return nil, userNotFound(err)
	}
	variant, err := catalog.GetNodeByID(ctx, order.ServiceVariantID)
	if err != nil {
		return nil, err
	}
	// Отсутствующий заказчик читается как «нет сведений»: правила допуска это
	// умеют, а отказывать исполнителю из-за чужой строки неправильно.
	customer, _ := users.FindByID(ctx, order.CustomerID)
	blocked := silentlyBlocked(ctx, blocks, viewer, repository.RoleExecutor)
	return variant, canViewOrTakeOrder(ctx, behaviors, blocked, viewer, customer, variant)
}
