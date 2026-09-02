package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Behaviors — сторона ядра в скрипте поведения: он превращает строки базы в
// факты, которые скрипту позволено видеть, вызывает хук и превращает ответ
// обратно в ошибки, на которых остальной слой сервисов уже говорит.
//
// Каждый метод безопасен к nil. Установка без скриптов и любой тест, которому
// они безразличны, передают nil и получают ровно то поведение, какое сервис имел
// до появления поведений.
type Behaviors struct {
	engine  *behavior.Engine
	claims  repository.ServiceClaimRepository
	catalog repository.ServiceCatalogRepository
}

// NewBehaviors подключает движок к хранилищу claim'ов. claims может быть nil, и
// тогда услуги «один раз на пользователя» нельзя обеспечить — они отклоняются, а
// не молча разрешаются дважды.
func NewBehaviors(engine *behavior.Engine, claims repository.ServiceClaimRepository) *Behaviors {
	if engine == nil {
		return nil
	}
	return &Behaviors{engine: engine, claims: claims}
}

// WithCatalog позволяет поведениям компилировать скрипты, хранящиеся на узлах
// каталога, — особые услуги, написанные в админ-панели.
func (b *Behaviors) WithCatalog(catalog repository.ServiceCatalogRepository) *Behaviors {
	if b != nil {
		b.catalog = catalog
	}
	return b
}

// codeFor — то поведение, которое узел на самом деле выполняет. Узел с
// собственным скриптом выполняет его, зарегистрированный под своим id; иначе он
// выполняет названное им библиотечное поведение. Всё ниже идёт через это, чтобы
// «какой скрипт» решалось в одном месте.
func (b *Behaviors) codeFor(node *repository.ServiceNode) string {
	if node == nil {
		return ""
	}
	if node.HasOwnScript() {
		return behavior.NodeCode(node.ID.String())
	}
	return node.BehaviorCode
}

// nodeSources отдаёт собственный скрипт узла как два файла, которые компилирует движок.
func nodeSources(node *repository.ServiceNode) []behavior.SourceFile {
	files := make([]behavior.SourceFile, 0, 2)
	if strings.TrimSpace(node.BehaviorConstants) != "" {
		files = append(files, behavior.SourceFile{Name: behavior.ConfigFile, Src: []byte(node.BehaviorConstants)})
	}
	return append(files, behavior.SourceFile{Name: "behavior.star", Src: []byte(node.BehaviorSource)})
}

// Validate компилирует кандидата, не регистрируя его. Админ-панель вызывает его
// перед сохранением: скрипт, который не компилируется, молча снял бы услугу с
// продажи, поэтому его отклоняют, пока на него ещё кто-то смотрит.
func (b *Behaviors) Validate(node *repository.ServiceNode) error {
	if b == nil || node == nil || !node.HasOwnScript() {
		return nil
	}
	return b.engine.Validate(nodeSources(node))
}

// SyncNode компилирует собственный скрипт узла или снимает его с регистрации,
// когда скрипт удалили. Вызывается сразу после сохранения админом, чтобы правка
// применилась к следующему запросу на этом процессе.
func (b *Behaviors) SyncNode(node *repository.ServiceNode) error {
	if b == nil || node == nil {
		return nil
	}
	code := behavior.NodeCode(node.ID.String())
	if !node.HasOwnScript() || node.IsDeleted() {
		b.engine.Remove(code)
		return nil
	}
	return b.engine.CompileFiles(code, nodeSources(node))
}

// RemoveNode снимает регистрацию скрипта узла — для путей, где есть только его
// id: списанный узел перестаёт быть особым сразу, а не на следующей пересинхронизации.
func (b *Behaviors) RemoveNode(nodeID uuid.UUID) {
	if b == nil {
		return
	}
	b.engine.Remove(behavior.NodeCode(nodeID.String()))
}

// SyncAll компилирует каждый скрипт узла в каталоге и убирает исчезнувшие. Он
// выполняется при старте и по таймеру: правка другой реплики или изменение,
// сделанное прямо в базе, должны дойти и до этого процесса.
func (b *Behaviors) SyncAll(ctx context.Context) error {
	if b == nil || b.catalog == nil {
		return nil
	}
	nodes, err := b.catalog.ListNodesWithScript(ctx)
	if err != nil {
		return err
	}

	live := make(map[string]struct{}, len(nodes))
	var failed []string
	for _, node := range nodes {
		code := behavior.NodeCode(node.ID.String())
		live[code] = struct{}{}
		if err := b.engine.CompileFiles(code, nodeSources(node)); err != nil {
			// Сообщается, но не фатально, и намеренно не регистрируется: узел
			// откатывается к «неизвестному поведению», чьи проверки закрыты.
			b.engine.Remove(code)
			failed = append(failed, node.Code)
			log.Printf("[behavior] node %s (%s): %v", node.Code, node.ID, err)
		}
	}
	for _, m := range b.engine.Manifests() {
		if !behavior.IsNodeCode(m.Code) {
			continue
		}
		if _, ok := live[m.Code]; !ok {
			b.engine.Remove(m.Code)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("node scripts failed to compile: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ErrBehaviorUnavailable — то, что получает вызывающий, когда скрипт, правящий
// услугой, не может быть выполнен: не скомпилировался, вышел за таймаут, вернул
// бессмыслицу. Любая проверка отказывает на нём в безопасную сторону: услугу,
// чьи правила нельзя вычислить, никто не может ни заказать, ни взять.
var ErrBehaviorUnavailable = errors.New("услуга временно недоступна")

// Engine открывает скомпилированные поведения — для админского эндпоинта,
// который их перечисляет. Больше он никому не нужен.
func (b *Behaviors) Engine() *behavior.Engine {
	if b == nil {
		return nil
	}
	return b.engine
}

// governs сообщает, приходят ли правила этого узла из скрипта, который есть у движка.
func (b *Behaviors) governs(node *repository.ServiceNode) bool {
	return b != nil && node != nil && node.HasBehavior()
}

// Governs сообщает, приходят ли правила этого узла из скрипта вообще.
// Вызывающие используют это, чтобы пропустить работу, нужную только скриптовой
// услуге: обычная услуга не должна платить за механизм, которым не пользуется.
func (b *Behaviors) Governs(node *repository.ServiceNode) bool {
	return b.governs(node)
}

// Manifest возвращает статическое объявление за поведением узла.
func (b *Behaviors) Manifest(node *repository.ServiceNode) (behavior.Manifest, bool) {
	if !b.governs(node) {
		return behavior.Manifest{}, false
	}
	return b.engine.Manifest(b.codeFor(node))
}

// Config возвращает конфигурацию узла, наложенную поверх собственных констант
// поведения, — то же слияние, что видит скрипт через f.config.
//
// Ядру она нужна везде, где оно проверяет то же, что читает скрипт: роль
// проверяющего, например, — константа в config.star, и проверка, смотрящая
// только в колонку узла, отказывала бы в том, что скрипт разрешает, как только
// эта константа изменится.
func (b *Behaviors) Config(node *repository.ServiceNode) map[string]interface{} {
	if !b.governs(node) {
		return nil
	}
	merged := map[string]interface{}{}
	if m, ok := b.engine.Manifest(b.codeFor(node)); ok {
		for k, v := range m.Defaults {
			merged[k] = v
		}
	}
	for k, v := range node.BehaviorConfig {
		merged[k] = v
	}
	return merged
}

// ConfigString читает одну строковую настройку из этого слияния.
func (b *Behaviors) ConfigString(node *repository.ServiceNode, key, fallback string) string {
	if value, ok := b.Config(node)[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

// OncePerUser сообщает, обязан ли заказ этого узла занять claim за пользователем.
func (b *Behaviors) OncePerUser(node *repository.ServiceNode) bool {
	m, ok := b.Manifest(node)
	return ok && m.OncePerUser
}

// ReleasesClaimOnCancel сообщает, возвращает ли отмена заказа по этому узлу
// пользователю его единственную попытку.
func (b *Behaviors) ReleasesClaimOnCancel(node *repository.ServiceNode) bool {
	m, ok := b.Manifest(node)
	return ok && m.OncePerUser && m.ReleaseClaimOnCancel
}

// Visible решает, можно ли перечислять узел каталога для этого смотрящего. Ему
// передают счётчики claim'ов смотрящего, потому что список судит много узлов
// разом и не должен делать запрос на каждый.
func (b *Behaviors) Visible(ctx context.Context, viewer *repository.User, node *repository.ServiceNode, claims map[uuid.UUID]int) bool {
	if !b.governs(node) {
		return true
	}
	facts := behavior.Facts{
		User:    actorFacts(viewer),
		Variant: variantFacts(node),
		Config:  node.BehaviorConfig,
		Claims:  claims[node.ID],
	}
	visible, err := b.engine.Visible(b.codeFor(node), facts)
	if err != nil {
		// Скрыт, а не показан: услуга, чьё правило видимости нельзя выполнить, —
		// это услуга, которую никто и заказать не может (CanOrder падает так же),
		// и её показ породил бы лишь отказ при оформлении.
		b.report(node, behavior.HookVisible, err)
		return false
	}
	return visible
}

// CanOrder — скриптовая половина canCustomerOrderVariant.
func (b *Behaviors) CanOrder(ctx context.Context, customer *repository.User, variant *repository.ServiceNode) error {
	if !b.governs(variant) {
		return nil
	}
	claims, err := b.claimCount(ctx, customer, variant)
	if err != nil {
		return err
	}
	facts := behavior.Facts{
		User:    actorFacts(customer),
		Variant: variantFacts(variant),
		Config:  variant.BehaviorConfig,
		Claims:  claims,
	}
	return b.translate(variant, behavior.HookCanOrder, b.engine.CanOrder(b.codeFor(variant), facts))
}

// CanViewOrTake — скриптовая половина canViewOrTakeOrder.
func (b *Behaviors) CanViewOrTake(ctx context.Context, viewer, customer *repository.User, variant *repository.ServiceNode) error {
	if !b.governs(variant) {
		return nil
	}
	facts := behavior.Facts{
		Viewer:   actorFacts(viewer),
		Customer: actorFacts(customer),
		User:     actorFacts(customer),
		Variant:  variantFacts(variant),
		Config:   variant.BehaviorConfig,
	}
	return b.translate(variant, behavior.HookCanViewOrTake, b.engine.CanViewOrTake(b.codeFor(variant), facts))
}

// Price возвращает цену, которую диктует поведение, если оно её диктует.
// Скрипт, назначающий цену услуге, полностью перекрывает каталог, включая
// тарифные коэффициенты: «бесплатно» обязано значить бесплатно даже для срочного заказа.
func (b *Behaviors) Price(ctx context.Context, variant *repository.ServiceNode) (money.Amount, bool, error) {
	if !b.governs(variant) {
		return money.Zero, false, nil
	}
	facts := behavior.Facts{
		Variant: variantFacts(variant),
		Config:  variant.BehaviorConfig,
	}
	rubles, ok, err := b.engine.Price(b.codeFor(variant), facts)
	if err != nil {
		b.report(variant, behavior.HookPrice, err)
		return money.Zero, false, ErrBehaviorUnavailable
	}
	if !ok {
		return money.Zero, false, nil
	}
	return money.FromRubles(rubles), true, nil
}

// claimCount отвечает на вопрос «сколько раз этот пользователь заказывал этот
// вариант», и только для тех поведений, которым это важно. Поведению «один раз
// на пользователя» без хранилища claim'ов отказывают: разрешить означало бы, что
// ограничение молча не действует.
func (b *Behaviors) claimCount(ctx context.Context, user *repository.User, variant *repository.ServiceNode) (int, error) {
	if user == nil || !b.OncePerUser(variant) {
		return 0, nil
	}
	if b.claims == nil {
		return 0, ErrBehaviorUnavailable
	}
	count, err := b.claims.CountForVariant(ctx, user.ID, variant.ID)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ClaimsFor загружает claim'ы пользователя одним запросом — для списков каталога.
func (b *Behaviors) ClaimsFor(ctx context.Context, user *repository.User) map[uuid.UUID]int {
	if b == nil || b.claims == nil || user == nil {
		return nil
	}
	counts, err := b.claims.CountsForUser(ctx, user.ID)
	if err != nil {
		log.Printf("[behavior] cannot read service claims of %s: %v", user.ID, err)
		return nil
	}
	return counts
}

// translate превращает ответ скрипта в словарь ошибок слоя сервисов: отказ
// сохраняет своё сообщение, всё прочее становится ErrBehaviorUnavailable и
// логируется.
func (b *Behaviors) translate(node *repository.ServiceNode, hook string, err error) error {
	if err == nil {
		return nil
	}
	var denied *behavior.DeniedError
	if errors.As(err, &denied) {
		return errors.New(denied.Message)
	}
	b.report(node, hook, err)
	return ErrBehaviorUnavailable
}

func (b *Behaviors) report(node *repository.ServiceNode, hook string, err error) {
	code := b.codeFor(node)
	log.Printf("[behavior] %s.%s on node %s: %v", code, hook, node.Code, err)
	metrics.BehaviorHookError(code, hook)
}

// Code — поведение, которое выполняет узел, для вызывающих вне этого файла,
// которым нужно его назвать: диспетчеру, когда он записывает, кто просил эффект.
func (b *Behaviors) Code(node *repository.ServiceNode) string {
	if b == nil {
		return ""
	}
	return b.codeFor(node)
}

// actorFacts отдаёт пользователя для скрипта. Nil остаётся nil: «нет
// пользователя» — случай, который скрипты обязаны обрабатывать (анонимный посетитель), а не ошибка.
func actorFacts(u *repository.User) *behavior.Actor {
	if u == nil {
		return nil
	}
	return &behavior.Actor{
		ID:         u.ID.String(),
		Role:       u.Role,
		Roles:      u.Roles,
		IsVerified: u.IsVerified(),
		Age:        u.GetAge(),
		Status:     u.Status,
	}
}

func variantFacts(n *repository.ServiceNode) *behavior.VariantFacts {
	if n == nil {
		return nil
	}
	v := &behavior.VariantFacts{ID: n.ID.String(), Code: n.Code}
	if n.BasePrice != nil {
		v.BasePrice = n.BasePrice.Rubles()
	}
	return v
}

func orderFacts(o *repository.Order) *behavior.OrderFacts {
	if o == nil {
		return nil
	}
	f := &behavior.OrderFacts{
		ID:         o.ID.String(),
		Status:     string(o.Status),
		CustomerID: o.CustomerID.String(),
		Amount:     o.HoldAmount.Rubles(),
		IsUrgent:   o.IsUrgent,
		IsAsap:     o.IsAsap,
	}
	if o.ExecutorID != nil {
		f.ExecutorID = o.ExecutorID.String()
	}
	return f
}
