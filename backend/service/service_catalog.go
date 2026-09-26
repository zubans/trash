package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ServiceCatalog — каталог услуг глазами вызывающего: какие узлы ему можно
// перечислять. Три правила видимости живут здесь, за один проход по списку:
// тихая блокировка заказчика прячет каталог целиком, услуги «только для
// верифицированных» не показываются неверифицированному заказчику, а узлы со
// скриптом решают о себе сами. Список, показывающий то, в чём откажет
// оформление, хуже, чем отсутствие узла в списке.
type ServiceCatalog struct {
	repo repository.ServiceCatalogRepository
	// behaviors решает видимость узлов, чьи правила приходят из скрипта.
	// Необязательно: без него применяются только встроенные флаги.
	behaviors *Behaviors
	// penalties прячет каталог целиком от заказчика в тихой блокировке. Необязательно.
	penalties *PenaltyService
}

// NewServiceCatalog создаёт ServiceCatalog.
func NewServiceCatalog(repo repository.ServiceCatalogRepository) *ServiceCatalog {
	return &ServiceCatalog{repo: repo}
}

// WithBehaviors подключает скрипты поведений к спискам каталога.
func (c *ServiceCatalog) WithBehaviors(behaviors *Behaviors) *ServiceCatalog {
	c.behaviors = behaviors
	return c
}

// WithPenalties подключает тихую блокировку заказчика к каталогу.
func (c *ServiceCatalog) WithPenalties(penalties *PenaltyService) *ServiceCatalog {
	c.penalties = penalties
	return c
}

// ErrServiceVariantNotFound — варианта нет или он не виден вызывающему: оба
// случая отвечают одинаково, иначе проверка видимости чисто косметическая.
var ErrServiceVariantNotFound = notFoundError("variant not found")

// hideVerificationOnly сообщает, является ли смотрящий заказчиком, не прошедшим
// ручную верификацию. Таким нельзя показывать услуги с флагом
// requires_verification — заказать их они не могут (проверка на создании
// заказа), так что показ только вводил бы в заблуждение. Исполнителей, админов
// и анонимов это не затрагивает.
func hideVerificationOnly(viewer *repository.User) bool {
	return viewer != nil && viewer.HasRole(repository.RoleCustomer) && !viewer.IsVerified()
}

// VisibleTo отбрасывает узлы, которые этот смотрящий видеть не должен.
// Счётчики claim читаются один раз на запрос, а не один раз на узел, и только
// когда среди узлов есть скриптовые: каталог обычных услуг не должен получать
// лишний запрос лишь потому, что где-то существуют скриптовые.
func (c *ServiceCatalog) VisibleTo(ctx context.Context, viewer *repository.User, nodes []*repository.ServiceNode) []*repository.ServiceNode {
	// Тихая блокировка заказчика: каталог пуст. Это то же самое, что видит
	// человек, которому не подходит ни одна услуга, — механика ничего о себе
	// не объявляет.
	if viewer != nil && c.penalties.SilentlyBlocked(ctx, viewer.ID, repository.RoleCustomer) {
		return []*repository.ServiceNode{}
	}
	hide := hideVerificationOnly(viewer)
	var claims map[uuid.UUID]int
	for _, n := range nodes {
		if c.behaviors.Governs(n) {
			claims = c.behaviors.ClaimsFor(ctx, viewer)
			break
		}
	}
	out := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if hide && n.RequiresVerification {
			continue
		}
		if !c.behaviors.Visible(ctx, viewer, n, claims) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// RootCategories — корневые категории, видимые смотрящему.
func (c *ServiceCatalog) RootCategories(ctx context.Context, viewer *repository.User) ([]*repository.ServiceNode, error) {
	nodes, err := c.repo.GetRootCategories(ctx, repository.FilterActive())
	if err != nil {
		return nil, err
	}
	return c.VisibleTo(ctx, viewer, nodes), nil
}

// Children — дети категории, видимые смотрящему.
func (c *ServiceCatalog) Children(ctx context.Context, viewer *repository.User, categoryID uuid.UUID) ([]*repository.ServiceNode, error) {
	nodes, err := c.repo.GetChildren(ctx, categoryID, repository.FilterActive())
	if err != nil {
		return nil, err
	}
	return c.VisibleTo(ctx, viewer, nodes), nil
}

// CategoryVariants — активные варианты во всём поддереве категории.
func (c *ServiceCatalog) CategoryVariants(ctx context.Context, viewer *repository.User, categoryID uuid.UUID) ([]*repository.ServiceNode, error) {
	nodes, err := c.repo.GetDescendants(ctx, categoryID, nil)
	if err != nil {
		return nil, err
	}
	variants := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsVariant() && n.IsActive {
			variants = append(variants, n)
		}
	}
	return c.VisibleTo(ctx, viewer, variants), nil
}

// Variants — все активные варианты, видимые смотрящему.
func (c *ServiceCatalog) Variants(ctx context.Context, viewer *repository.User) ([]*repository.ServiceNode, error) {
	nodes, err := c.repo.GetActiveVariants(ctx)
	if err != nil {
		return nil, err
	}
	return c.VisibleTo(ctx, viewer, nodes), nil
}

// VariantWithPath — вариант вместе с путём до него от корня.
type VariantWithPath struct {
	Variant *repository.ServiceNode   `json:"variant"`
	Path    []*repository.ServiceNode `json:"path"`
}

// Variant — один вариант по id. Вариант, который вызывающий не видит в списке,
// не читается и по id.
func (c *ServiceCatalog) Variant(ctx context.Context, viewer *repository.User, id uuid.UUID) (*VariantWithPath, error) {
	variant, path, err := c.repo.GetVariantWithCategory(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
			return nil, ErrServiceVariantNotFound
		}
		return nil, err
	}
	if variant == nil || len(c.VisibleTo(ctx, viewer, []*repository.ServiceNode{variant})) == 0 {
		return nil, ErrServiceVariantNotFound
	}
	return &VariantWithPath{Variant: variant, Path: path}, nil
}

// --- Админ -------------------------------------------------------------------

// ServiceCatalogAdmin — конструктор услуг: дерево, проверки узла и родителя,
// создание, правка, списание и восстановление. Скрипт узла компилируется до
// записи строки и регистрируется в движке сразу после неё.
type ServiceCatalogAdmin struct {
	repo      repository.ServiceCatalogRepository
	behaviors *Behaviors
}

// NewServiceCatalogAdmin создаёт ServiceCatalogAdmin. behaviors может быть nil:
// тогда узлы со скриптом не сохраняются, а библиотека поведений пуста.
func NewServiceCatalogAdmin(repo repository.ServiceCatalogRepository, behaviors *Behaviors) *ServiceCatalogAdmin {
	return &ServiceCatalogAdmin{repo: repo, behaviors: behaviors}
}

// ServiceNodeForm — то, что админ-панель присылает при создании и правке
// узла. Только редактируемые поля: id, время создания, правки и списания
// строка получает от сервера. Имена JSON совпадают с полями узла, поэтому
// форма конструктора не меняется.
type ServiceNodeForm struct {
	ParentID *uuid.UUID `json:"parent_id"`
	// Code и NodeType неизменяемы: читаются только при создании.
	Code                 string                     `json:"code"`
	NodeType             repository.ServiceNodeType `json:"node_type"`
	Name                 repository.LocalizedText   `json:"name"`
	Description          repository.LocalizedText   `json:"description"`
	BasePrice            *money.Amount              `json:"base_price"`
	IsAuction            bool                       `json:"is_auction"`
	IsActive             bool                       `json:"is_active"`
	SortOrder            int                        `json:"sort_order"`
	RequiresVerification bool                       `json:"requires_verification"`
	MinAge               int                        `json:"min_age"`
	ModeratorOnly        bool                       `json:"moderator_only"`
	BehaviorCode         string                     `json:"behavior_code"`
	BehaviorConfig       repository.BehaviorConfig  `json:"behavior_config"`
	BehaviorConstants    string                     `json:"behavior_constants"`
	BehaviorSource       string                     `json:"behavior_source"`
}

// node собирает строку из формы. Серверные поля остаются нулевыми: их ставит
// вызывающий (id и неизменяемые поля существующего узла) или репозиторий.
func (f *ServiceNodeForm) node() *repository.ServiceNode {
	return &repository.ServiceNode{
		ParentID: f.ParentID, Code: f.Code, NodeType: f.NodeType,
		Name: f.Name, Description: f.Description, BasePrice: f.BasePrice,
		IsAuction: f.IsAuction, IsActive: f.IsActive, SortOrder: f.SortOrder,
		RequiresVerification: f.RequiresVerification, MinAge: f.MinAge, ModeratorOnly: f.ModeratorOnly,
		BehaviorCode: f.BehaviorCode, BehaviorConfig: f.BehaviorConfig,
		BehaviorConstants: f.BehaviorConstants, BehaviorSource: f.BehaviorSource,
	}
}

// ServiceNodeTree — узел вместе с детьми, в порядке показа.
type ServiceNodeTree struct {
	Node     *repository.ServiceNode `json:"node"`
	Children []*ServiceNodeTree      `json:"children"`
}

// DeleteResult — что случилось при списании узла.
type DeleteResult struct {
	Message string `json:"message"`
	Soft    bool   `json:"soft"`
	// HadOrders — по узлу были заказы; их история сохраняется.
	HadOrders bool `json:"had_orders"`
	// DeletedCount — сколько узлов ушло вместе с этим (узел + поддерево), чтобы
	// админ-панель могла сказать «удалена категория и N вложенных элементов».
	DeletedCount int `json:"deleted_count"`
}

var serviceNodeCodePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// maxScriptBytes ограничивает одно поле скрипта. Поведение — это страница
// правил, а не программа; предел стоит, чтобы случайная вставка не забила колонку.
const maxScriptBytes = 64 * 1024

// Ошибки конструктора услуг.
var (
	// ErrServiceNodeNotFound — узла нет.
	ErrServiceNodeNotFound = notFoundError("node not found")
	// ErrServiceNodeDeleted — удалённый узел сначала восстанавливают.
	ErrServiceNodeDeleted = conflictError("node is deleted: restore it before editing")
)

// Library — библиотечные поведения, поставляемые со сборкой, каждое с полным
// текстом: конструктор услуг показывает их как стартовый шаблон особой услуги.
// Скрипты отдельных узлов здесь не перечисляются — скрипт узла есть часть
// этого узла и правится на нём.
func (a *ServiceCatalogAdmin) Library() []behavior.Manifest {
	if a.behaviors == nil || a.behaviors.Engine() == nil {
		return []behavior.Manifest{}
	}
	return a.behaviors.Engine().Library()
}

// Tree — весь каталог деревом, одним запросом. Списанные узлы показываются
// только по просьбе.
func (a *ServiceCatalogAdmin) Tree(ctx context.Context, includeDeleted bool) ([]*ServiceNodeTree, error) {
	nodes, err := a.repo.ListAll(ctx, repository.ServiceNodeFilter{IncludeDeleted: includeDeleted})
	if err != nil {
		return nil, err
	}
	return buildTree(nodes), nil
}

// buildTree собирает дерево из плоского списка, сохраняя порядок списка среди
// братьев. Корни — узлы без родителя и узлы, чей родитель в список не попал
// (живой узел под списанной категорией невозможен, но дерево не должно молча
// терять строку, если это когда-нибудь случится).
func buildTree(nodes []*repository.ServiceNode) []*ServiceNodeTree {
	byID := make(map[uuid.UUID]*ServiceNodeTree, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = &ServiceNodeTree{Node: n, Children: []*ServiceNodeTree{}}
	}
	roots := make([]*ServiceNodeTree, 0)
	for _, n := range nodes {
		item := byID[n.ID]
		if n.ParentID != nil {
			if parent, ok := byID[*n.ParentID]; ok {
				parent.Children = append(parent.Children, item)
				continue
			}
		}
		roots = append(roots, item)
	}
	return roots
}

// Get — один узел, включая списанный.
func (a *ServiceCatalogAdmin) Get(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, error) {
	node, err := a.repo.GetNodeByID(ctx, id)
	if err != nil {
		return nil, catalogNotFound(err)
	}
	if node == nil {
		return nil, ErrServiceNodeNotFound
	}
	return node, nil
}

// catalogNotFound переводит «нет строки» в ошибку домена, оставляя сбой сбоем.
func catalogNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrServiceNodeNotFound) || errors.Is(err, repository.ErrNotFound) {
		return ErrServiceNodeNotFound
	}
	return err
}

// Create заводит узел. Скрипт компилируется в работающий движок сразу,
// поэтому услуга ведёт себя как отредактировано уже на следующем запросе, а
// не после перезапуска.
func (a *ServiceCatalogAdmin) Create(ctx context.Context, form ServiceNodeForm) (*repository.ServiceNode, error) {
	node := form.node()
	if err := a.validateNode(node, true); err != nil {
		return nil, err
	}
	if err := a.validateParent(ctx, node.ParentID); err != nil {
		return nil, err
	}
	if err := a.repo.CreateNode(ctx, node); err != nil {
		return nil, catalogError(err)
	}
	a.syncBehavior(node)
	return node, nil
}

// Update правит узел. code и node_type неизменяемы: они берутся у сохранённого
// узла, а не из формы, поэтому клиент их не шлёт, а правила проверяются по
// настоящему типу узла.
func (a *ServiceCatalogAdmin) Update(ctx context.Context, id uuid.UUID, form ServiceNodeForm) (*repository.ServiceNode, error) {
	existing, err := a.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.IsDeleted() {
		return nil, ErrServiceNodeDeleted
	}
	node := form.node()
	node.ID = id
	node.NodeType = existing.NodeType
	node.Code = existing.Code
	node.CreatedAt = existing.CreatedAt

	if err := a.validateNode(node, false); err != nil {
		return nil, err
	}
	if err := a.validateParent(ctx, node.ParentID); err != nil {
		return nil, err
	}
	if err := a.repo.UpdateNode(ctx, node); err != nil {
		return nil, catalogError(err)
	}
	a.syncBehavior(node)
	return node, nil
}

// Delete списывает узел вместе с поддеревом. Узел списывается, а не удаляется:
// у размещённых по нему заказов остаётся их услуга, а сам узел можно позже
// восстановить.
func (a *ServiceCatalogAdmin) Delete(ctx context.Context, id uuid.UUID) (*DeleteResult, error) {
	// Читаем перед удалением: ответ после него не изменился бы, но админ-панель
	// хочет сказать, что история заказов сохраняется.
	hadOrders, _ := a.repo.HasOrders(ctx, id)

	// Удаление каскадное, поэтому и снятие поведений — по всему поддереву.
	// Собираем живых потомков до удаления: после него GetDescendants их уже не
	// вернёт (они отфильтруются по deleted_at). Сам узел добавляем отдельно,
	// потому что GetDescendants отдаёт только потомков.
	subtree := []uuid.UUID{id}
	if descendants, err := a.repo.GetDescendants(ctx, id, nil); err == nil {
		for _, d := range descendants {
			subtree = append(subtree, d.ID)
		}
	}
	if err := a.repo.DeleteNode(ctx, id); err != nil {
		return nil, catalogError(err)
	}
	// Списанный узел немедленно перестаёт выполнять свой скрипт; строка его
	// хранит, поэтому восстановление возвращает услугу ровно такой, какой она была.
	if a.behaviors != nil {
		for _, nodeID := range subtree {
			a.behaviors.RemoveNode(nodeID)
		}
	}
	return &DeleteResult{Message: "node deleted successfully", Soft: true, HadOrders: hadOrders, DeletedCount: len(subtree)}, nil
}

// Restore возвращает списанный узел — выключенным, чтобы его публиковали
// заново осознанно.
func (a *ServiceCatalogAdmin) Restore(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, error) {
	if err := a.repo.RestoreNode(ctx, id); err != nil {
		return nil, catalogError(err)
	}
	node, err := a.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	a.syncBehavior(node)
	return node, nil
}

// syncBehavior регистрирует (или снимает с регистрации) собственный скрипт узла
// в работающем движке. Скрипт уже скомпилирован в validateNode, поэтому сбой
// здесь — неожиданность, которую стоит залогировать; страхует периодическая
// пересинхронизация в воркере поведений, она же разносит правку по процессам.
func (a *ServiceCatalogAdmin) syncBehavior(node *repository.ServiceNode) {
	if a.behaviors == nil {
		return
	}
	if err := a.behaviors.SyncNode(node); err != nil {
		log.Printf("[behavior] node %s saved but not loaded: %v", node.Code, err)
	}
}

// catalogError переводит ошибки репозитория в классы домена: цикл родителя —
// ошибка в запросе, конфликты состояния — конфликты, «нет строки» — 404.
func catalogError(err error) error {
	switch {
	case errors.Is(err, repository.ErrServiceNodeParentCycle):
		return validationError(err.Error())
	case errors.Is(err, repository.ErrServiceNodeDeleted),
		errors.Is(err, repository.ErrServiceNodeNotDeleted),
		errors.Is(err, repository.ErrServiceNodeHasChildren),
		errors.Is(err, repository.ErrServiceNodeCodeTaken),
		errors.Is(err, repository.ErrServiceNodeParentDeleted):
		return conflictError(err.Error())
	default:
		return catalogNotFound(err)
	}
}

func (a *ServiceCatalogAdmin) validateParent(ctx context.Context, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	parent, err := a.repo.GetNodeByID(ctx, *parentID)
	if err != nil || parent == nil {
		return validationError("parent not found")
	}
	if parent.NodeType != repository.ServiceNodeTypeCategory {
		return validationError("parent must be a category")
	}
	// Узел под удалённой категорией был бы недостижим из каталога.
	if parent.IsDeleted() {
		return validationError("parent category is deleted")
	}
	return nil
}

func (a *ServiceCatalogAdmin) validateNode(node *repository.ServiceNode, isCreate bool) error {
	if isCreate {
		if node.Code == "" {
			return validationError("code is required")
		}
		if !serviceNodeCodePattern.MatchString(node.Code) {
			return validationError("code must match ^[a-z0-9_]+$")
		}
		if node.NodeType != repository.ServiceNodeTypeCategory && node.NodeType != repository.ServiceNodeTypeVariant {
			return validationError("node_type must be CATEGORY or VARIANT")
		}
	}
	if node.Name == nil || node.Name["ru"] == "" {
		return validationError("name must contain at least the 'ru' key")
	}

	// Собственный скрипт узла компилируется здесь, до записи строки: скрипт,
	// который не компилируется, провалил бы все проверки узла, а заказчик прочёл бы
	// это как исчезновение услуги. Лучше отказать в сохранении, пока админ ещё
	// смотрит в редактор.
	if node.HasOwnScript() {
		if len(node.BehaviorSource) > maxScriptBytes || len(node.BehaviorConstants) > maxScriptBytes {
			return validationError(fmt.Sprintf("скрипт длиннее %d КБ", maxScriptBytes/1024))
		}
		if a.behaviors == nil {
			return validationError("service behaviors are not available on this server")
		}
		if err := a.behaviors.Validate(node); err != nil {
			return validationError("скрипт не компилируется: " + err.Error())
		}
	} else if node.BehaviorCode != "" {
		// Библиотечный код, за которым нет скрипта, точно так же отказывает в безопасную сторону.
		if a.behaviors == nil || !a.behaviors.Engine().Has(node.BehaviorCode) {
			return validationError("unknown behavior_code: " + node.BehaviorCode)
		}
	}

	if node.NodeType == repository.ServiceNodeTypeVariant {
		if node.BasePrice == nil {
			return validationError("VARIANT must have base_price")
		}
		if node.IsAuction && *node.BasePrice != 0 {
			return validationError("auction variant base_price must be 0")
		}
	} else {
		// Клиент, у которого одна форма на оба типа узлов, шлёт base_price: 0 для
		// категории. Это «нет цены», а не противоречащая цена.
		if node.BasePrice != nil && node.BasePrice.IsZero() {
			node.BasePrice = nil
		}
		if node.BasePrice != nil {
			return validationError("CATEGORY cannot have base_price")
		}
	}
	return nil
}
