package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// ServiceNodeType задаёт тип узла каталога.
type ServiceNodeType string

const (
	ServiceNodeTypeCategory ServiceNodeType = "CATEGORY"
	ServiceNodeTypeVariant  ServiceNodeType = "VARIANT"
)

// Ошибки каталога, которые слой обработчиков отображает в коды статуса HTTP.
var (
	ErrServiceNodeNotFound      = errors.New("service node not found")
	ErrServiceNodeDeleted       = errors.New("service node is deleted")
	ErrServiceNodeNotDeleted    = errors.New("service node is not deleted")
	ErrServiceNodeHasChildren   = errors.New("cannot delete a node that still has children")
	ErrServiceNodeCodeTaken     = errors.New("another node already uses this code")
	ErrServiceNodeParentDeleted = errors.New("parent category is deleted")
)

// LocalizedText хранит переводы одного поля.
type LocalizedText map[string]string

// Value реализует интерфейс driver.Valuer для хранения в JSONB.
func (lt LocalizedText) Value() (driver.Value, error) {
	if lt == nil {
		return nil, nil
	}
	return json.Marshal(lt)
}

// Scan реализует интерфейс sql.Scanner для чтения из JSONB.
func (lt *LocalizedText) Scan(value interface{}) error {
	if value == nil {
		*lt = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into LocalizedText", value)
	}
	return json.Unmarshal(bytes, lt)
}

// BehaviorConfig — конфигурация поведения узла услуги в том виде, как она
// хранится в JSONB. Она намеренно нетипизирована: у каждого поведения свои
// ключи, и объявление их в Go вернуло бы определение поведения в тот самый код,
// вне которого поведение и существует. Умолчания объявляет манифест скрипта, и
// админ-панель рисует форму по нему.
type BehaviorConfig map[string]interface{}

// Value реализует driver.Valuer для хранения в JSONB. Пустая или nil-конфигурация
// сохраняется как пустой объект, поэтому колонка никогда не хранит NULL.
func (c BehaviorConfig) Value() (driver.Value, error) {
	if len(c) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]interface{}(c))
}

// Scan реализует sql.Scanner для чтения из JSONB.
func (c *BehaviorConfig) Scan(value interface{}) error {
	if value == nil {
		*c = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into BehaviorConfig", value)
	}
	return json.Unmarshal(bytes, c)
}

// ServiceNode представляет узел в дереве каталога услуг.
type ServiceNode struct {
	ID                   uuid.UUID       `json:"id"`
	ParentID             *uuid.UUID      `json:"parent_id,omitempty"`
	Code                 string          `json:"code"`
	Name                 LocalizedText   `json:"name"`
	Description          LocalizedText   `json:"description,omitempty"`
	NodeType             ServiceNodeType `json:"node_type"`
	BasePrice            *money.Amount   `json:"base_price,omitempty"`
	IsAuction            bool            `json:"is_auction"`
	IsActive             bool            `json:"is_active"`
	SortOrder            int             `json:"sort_order"`
	RequiresVerification bool            `json:"requires_verification"`
	MinAge               int             `json:"min_age"`
	// ModeratorOnly помечает услугу, заказы по которой видны и доступны для
	// принятия только модераторам (см. миграцию 040).
	ModeratorOnly bool `json:"moderator_only"`
	// BehaviorCode называет скрипт, несущий собственные правила этой услуги (см.
	// пакет behavior и миграцию 043). Пусто для обычной услуги, то есть для
	// любой услуги, существовавшей до поведений: флаги выше — это всё её
	// поведение.
	BehaviorCode string `json:"behavior_code,omitempty"`
	// BehaviorConfig — конфигурация этого скрипта на уровне узла: вознаграждение,
	// которое он платит, требуемая роль. Умолчания объявляет скрипт; здесь лежит
	// только то, что меняет этот узел.
	BehaviorConfig BehaviorConfig `json:"behavior_config,omitempty"`
	// BehaviorConstants и BehaviorSource — собственный скрипт узла, написанный в
	// админ-панели (миграция 044): файл констант и логика. Если они заданы, они
	// полностью заменяют файловое поведение — BehaviorCode тогда лишь фиксирует,
	// с какого библиотечного скрипта админ начал.
	BehaviorConstants string    `json:"behavior_constants,omitempty"`
	BehaviorSource    string    `json:"behavior_source,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// DeletedAt выставляется при списании узла. Строка остаётся на месте, чтобы
	// ссылающиеся на неё заказы продолжали разрешаться; в каталоге эту услугу
	// больше никто не предлагает.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// HasBehavior сообщает, приходят ли правила этого узла из скрипта — собственного
// или одного из библиотечных поведений, поставляемых со сборкой.
func (n *ServiceNode) HasBehavior() bool {
	return n != nil && (n.BehaviorCode != "" || n.HasOwnScript())
}

// HasOwnScript сообщает, несёт ли узел собственный скрипт. Такой узел админ-панель
// называет особой услугой: его правила написаны в конструкторе услуг, а не
// поставлены файлом.
func (n *ServiceNode) HasOwnScript() bool {
	return n != nil && strings.TrimSpace(n.BehaviorSource) != ""
}

// nullableCode хранит пустой код поведения как NULL, чтобы «нет поведения» было
// в базе одним значением, а не двумя.
func nullableCode(code string) interface{} {
	if code == "" {
		return nil
	}
	return code
}

// nullableText делает то же для скрипта, оставленного пустым.
func nullableText(text string) interface{} {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return text
}

// IsCategory возвращает true, если узел — категория.
func (n *ServiceNode) IsCategory() bool {
	return n.NodeType == ServiceNodeTypeCategory
}

// IsVariant возвращает true, если узел — вариант услуги.
func (n *ServiceNode) IsVariant() bool {
	return n.NodeType == ServiceNodeTypeVariant
}

// IsDeleted сообщает, помечен ли узел как удалённый.
func (n *ServiceNode) IsDeleted() bool {
	return n != nil && n.DeletedAt != nil
}

// IsOrderable сообщает, можно ли размещать новые заказы по этому узлу.
func (n *ServiceNode) IsOrderable() bool {
	return n != nil && n.IsVariant() && n.IsActive && !n.IsDeleted()
}

// ServiceNodeFilter сужает запрос по дереву. Нулевое значение возвращает все
// живые узлы, активные и нет, — именно это админ-панель показывает по умолчанию.
type ServiceNodeFilter struct {
	// ActiveOnly отбрасывает узлы, выключенные в приложении.
	ActiveOnly bool
	// IncludeDeleted оставляет мягко удалённые узлы в результате. Их запрашивает
	// только админский экран каталога.
	IncludeDeleted bool
}

// FilterActive возвращает только узлы, которые приложение может предлагать пользователям.
var FilterActive = ServiceNodeFilter{ActiveOnly: true}

// FilterLive возвращает активные и неактивные узлы, но не удалённые.
var FilterLive = ServiceNodeFilter{}

// where отдаёт фильтр как SQL-предикаты, добавляемые к существующему WHERE.
// col — псевдоним таблицы, содержащей колонки узла.
func (f ServiceNodeFilter) where(col string) string {
	clause := ""
	if !f.IncludeDeleted {
		clause += " AND " + col + "deleted_at IS NULL"
	}
	if f.ActiveOnly {
		clause += " AND " + col + "is_active = TRUE"
	}
	return clause
}

// ServiceCatalogRepository описывает операции хранения каталога услуг.
type ServiceCatalogRepository interface {
	// CRUD
	CreateNode(ctx context.Context, node *ServiceNode) error
	UpdateNode(ctx context.Context, node *ServiceNode) error
	// DeleteNode мягко удаляет узел: строка выживает, чтобы исторические заказы
	// продолжали разрешаться, а каталог перестаёт его предлагать.
	DeleteNode(ctx context.Context, id uuid.UUID) error
	// RestoreNode возвращает мягко удалённый узел, выключенным.
	RestoreNode(ctx context.Context, id uuid.UUID) error
	// GetNodeByID возвращает узел, даже если тот удалён, потому что заказы,
	// размещённые до удаления, всё равно обязаны отрисовать свою услугу.
	GetNodeByID(ctx context.Context, id uuid.UUID) (*ServiceNode, error)
	// GetNodesByIDs разрешает набор узлов одним запросом — для списковых
	// эндпоинтов, которым нужен вариант услуги каждой возвращаемой строки. Как и
	// GetNodeByID, он включает удалённые узлы, и так же id без узла просто
	// отсутствует в результате, а не даёт ошибку.
	GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*ServiceNode, error)
	// GetNodeByCode ищет только живой узел; удалённый код свободен для того, чтобы
	// его занял новый узел.
	GetNodeByCode(ctx context.Context, code string) (*ServiceNode, error)

	// Навигация по дереву
	GetRootCategories(ctx context.Context, filter ServiceNodeFilter) ([]*ServiceNode, error)
	GetChildren(ctx context.Context, parentID uuid.UUID, filter ServiceNodeFilter) ([]*ServiceNode, error)
	GetDescendants(ctx context.Context, ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error)
	GetAncestors(ctx context.Context, descendantID uuid.UUID) ([]*ServiceNode, error)
	GetVariantPath(ctx context.Context, variantID uuid.UUID) ([]*ServiceNode, error)

	// Помощники каталога
	GetActiveVariants(ctx context.Context) ([]*ServiceNode, error)
	// ListNodesWithScript возвращает все живые узлы с собственным скриптом, чтобы
	// движок поведений мог скомпилировать их при старте и подхватить правки,
	// сделанные другим процессом.
	ListNodesWithScript(ctx context.Context) ([]*ServiceNode, error)
	GetVariantWithCategory(ctx context.Context, id uuid.UUID) (*ServiceNode, []*ServiceNode, error)

	// Транзакционные помощники, используемые слоем сервисов.
	HasChildren(ctx context.Context, id uuid.UUID) (bool, error)
	HasOrders(ctx context.Context, id uuid.UUID) (bool, error)
	IsDescendantOf(ctx context.Context, candidateAncestor, candidateDescendant uuid.UUID) (bool, error)
}

type serviceCatalogRepo struct {
	db *sql.DB
}

// NewServiceCatalogRepository создаёт новый репозиторий каталога услуг.
func NewServiceCatalogRepository(db *sql.DB) ServiceCatalogRepository {
	return &serviceCatalogRepo{db: db}
}

const serviceNodeColumns = `
    id, parent_id, code, name, description, node_type, base_price,
    is_auction, is_active, sort_order, COALESCE(requires_verification, false), COALESCE(min_age, 0), COALESCE(moderator_only, false),
    COALESCE(behavior_code, ''), COALESCE(behavior_config, '{}'::jsonb),
    COALESCE(behavior_constants, ''), COALESCE(behavior_source, ''), created_at, updated_at, deleted_at
`

// rowScanner удовлетворяется и *sql.Row, и *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanServiceNodeInto(s rowScanner) (*ServiceNode, error) {
	var n ServiceNode
	err := s.Scan(
		&n.ID, &n.ParentID, &n.Code, &n.Name, &n.Description, &n.NodeType,
		&n.BasePrice, &n.IsAuction, &n.IsActive, &n.SortOrder, &n.RequiresVerification,
		&n.MinAge, &n.ModeratorOnly, &n.BehaviorCode, &n.BehaviorConfig,
		&n.BehaviorConstants, &n.BehaviorSource, &n.CreatedAt, &n.UpdatedAt, &n.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func scanServiceNode(row *sql.Row) (*ServiceNode, error) {
	return scanServiceNodeInto(row)
}

func scanServiceNodeRows(rows *sql.Rows) (*ServiceNode, error) {
	return scanServiceNodeInto(rows)
}

func (r *serviceCatalogRepo) queryNodes(ctx context.Context, query string, args ...interface{}) ([]*ServiceNode, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := []*ServiceNode{}
	for rows.Next() {
		n, err := scanServiceNodeRows(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (r *serviceCatalogRepo) CreateNode(ctx context.Context, node *ServiceNode) error {
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}
	now := time.Now()
	node.CreatedAt = now
	node.UpdatedAt = now
	node.DeletedAt = nil

	// Уникальный индекс покрывает только живые узлы, поэтому сообщаем о коллизии
	// сообщением, которое админ-панель может показать, а не ошибкой драйвера.
	taken, err := r.codeTaken(ctx, node.Code, node.ID)
	if err != nil {
		return err
	}
	if taken {
		return ErrServiceNodeCodeTaken
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
        INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, requires_verification, min_age, moderator_only, behavior_code, behavior_config, behavior_constants, behavior_source, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
    `
	_, err = tx.ExecContext(ctx, query,
		node.ID, node.ParentID, node.Code, node.Name, node.Description,
		node.NodeType, node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.ModeratorOnly,
		nullableCode(node.BehaviorCode), node.BehaviorConfig,
		nullableText(node.BehaviorConstants), nullableText(node.BehaviorSource),
		node.CreatedAt, node.UpdatedAt,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `SELECT rebuild_service_node_paths($1)`, node.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *serviceCatalogRepo) UpdateNode(ctx context.Context, node *ServiceNode) error {
	node.UpdatedAt = time.Now()

	existing, err := r.GetNodeByID(ctx, node.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrServiceNodeNotFound
		}
		return err
	}
	if existing == nil {
		return ErrServiceNodeNotFound
	}
	// Удалённый узел сначала восстанавливается, а потом правится: правка на месте
	// позволила бы админу менять услугу, которой в каталоге уже нет.
	if existing.IsDeleted() {
		return ErrServiceNodeDeleted
	}

	// node_type неизменяем (H2).
	node.NodeType = existing.NodeType
	// code неизменяем (H2); сохраняем существующее значение, чтобы избежать случайных изменений.
	node.Code = existing.Code
	node.DeletedAt = nil

	if node.ParentID != nil {
		// Предотвращаем циклы: нельзя назначить родителем себя или любого потомка.
		if *node.ParentID == node.ID {
			return errors.New("cannot set parent to self")
		}
		isDescendant, err := r.IsDescendantOf(ctx, *node.ParentID, node.ID)
		if err != nil {
			return err
		}
		if isDescendant {
			return errors.New("cannot set parent to descendant")
		}
		// Перенос живого узла под удалённую категорию спрятал бы его из каталога,
		// так его и не удалив.
		parent, err := r.GetNodeByID(ctx, *node.ParentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("parent not found")
			}
			return err
		}
		if parent.IsDeleted() {
			return ErrServiceNodeParentDeleted
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
        UPDATE service_nodes
        SET parent_id = $2, name = $3, description = $4, base_price = $5,
            is_auction = $6, is_active = $7, sort_order = $8, requires_verification = $9, min_age = $10, moderator_only = $11,
            behavior_code = $12, behavior_config = $13,
            behavior_constants = $14, behavior_source = $15, updated_at = $16
        WHERE id = $1 AND deleted_at IS NULL
    `
	_, err = tx.ExecContext(ctx, query,
		node.ID, node.ParentID, node.Name, node.Description,
		node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.ModeratorOnly,
		nullableCode(node.BehaviorCode), node.BehaviorConfig,
		nullableText(node.BehaviorConstants), nullableText(node.BehaviorSource),
		node.UpdatedAt,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `SELECT rebuild_service_node_paths($1)`, node.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteNode списывает узел вместе со всем его поддеревом, не удаляя строки. У
// заказов есть внешний ключ на вариант, по которому они размещены, поэтому
// жёсткое удаление либо упало бы, либо утащило бы историю заказов; пометка узла
// оставляет эту историю читаемой, а каталог перестаёт предлагать услугу.
//
// Удаление каскадное: снимая категорию, гасим и всех её живых потомков одним
// оператором, а не отказываем, пока у неё есть дети. Обход идёт рекурсивно по
// parent_id, поэтому не зависит от корректности closure-таблицы; уже удалённые
// узлы пропускаются по deleted_at, так что их is_active/deleted_at не трогаются.
func (r *serviceCatalogRepo) DeleteNode(ctx context.Context, id uuid.UUID) error {
	node, err := r.GetNodeByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrServiceNodeNotFound
		}
		return err
	}
	if node == nil {
		return ErrServiceNodeNotFound
	}
	if node.IsDeleted() {
		return ErrServiceNodeDeleted
	}

	// is_active выключается тем же оператором: любой запрос каталога и так по нему
	// фильтрует, поэтому услуга исчезает даже для вызывающего, появившегося раньше
	// deleted_at. Пару скрепляет check-ограничение в базе.
	res, err := r.db.ExecContext(ctx, `
        WITH RECURSIVE subtree AS (
            SELECT id FROM service_nodes WHERE id = $1
            UNION ALL
            SELECT sn.id FROM service_nodes sn
            JOIN subtree s ON sn.parent_id = s.id
        )
        UPDATE service_nodes
        SET deleted_at = now(), is_active = FALSE, updated_at = now()
        WHERE deleted_at IS NULL AND id IN (SELECT id FROM subtree)
    `, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Кто-то удалил его между чтением и обновлением.
		return ErrServiceNodeDeleted
	}
	return nil
}

// RestoreNode снимает пометку удаления. Узел возвращается выключенным, чтобы
// админ осознанно включил его снова, прежде чем заказчики его увидят.
func (r *serviceCatalogRepo) RestoreNode(ctx context.Context, id uuid.UUID) error {
	node, err := r.GetNodeByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrServiceNodeNotFound
		}
		return err
	}
	if node == nil {
		return ErrServiceNodeNotFound
	}
	if !node.IsDeleted() {
		return ErrServiceNodeNotDeleted
	}

	// Восстановление в удалённую ветку дало бы узел, до которого никому не добраться.
	if node.ParentID != nil {
		parent, err := r.GetNodeByID(ctx, *node.ParentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrServiceNodeParentDeleted
			}
			return err
		}
		if parent.IsDeleted() {
			return ErrServiceNodeParentDeleted
		}
	}

	// Пока узел был удалён, код был свободен, поэтому его может держать новый узел.
	taken, err := r.codeTaken(ctx, node.Code, id)
	if err != nil {
		return err
	}
	if taken {
		return ErrServiceNodeCodeTaken
	}

	_, err = r.db.ExecContext(ctx, `
        UPDATE service_nodes
        SET deleted_at = NULL, updated_at = now()
        WHERE id = $1 AND deleted_at IS NOT NULL
    `, id)
	return err
}

// codeTaken сообщает, использует ли этот код живой узел, отличный от exclude.
func (r *serviceCatalogRepo) codeTaken(ctx context.Context, code string, exclude uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM service_nodes
            WHERE code = $1 AND deleted_at IS NULL AND id <> $2
        )
    `, code, exclude).Scan(&exists)
	return exists, err
}

func (r *serviceCatalogRepo) GetNodeByID(ctx context.Context, id uuid.UUID) (*ServiceNode, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+serviceNodeColumns+" FROM service_nodes WHERE id = $1", id,
	)
	return scanServiceNode(row)
}

func (r *serviceCatalogRepo) GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*ServiceNode, error) {
	result := make(map[uuid.UUID]*ServiceNode, len(ids))
	placeholders, args := idList(ids)
	if len(args) == 0 {
		return result, nil
	}
	nodes, err := r.queryNodes(ctx,
		"SELECT "+serviceNodeColumns+" FROM service_nodes WHERE id IN ("+placeholders+")",
		args...,
	)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		result[n.ID] = n
	}
	return result, nil
}

func (r *serviceCatalogRepo) GetNodeByCode(ctx context.Context, code string) (*ServiceNode, error) {
	row := r.db.QueryRowContext(ctx,
		"SELECT "+serviceNodeColumns+" FROM service_nodes WHERE code = $1 AND deleted_at IS NULL", code,
	)
	return scanServiceNode(row)
}

func (r *serviceCatalogRepo) GetRootCategories(ctx context.Context, filter ServiceNodeFilter) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE parent_id IS NULL" +
		filter.where("") + " ORDER BY sort_order, name->>'ru'"
	return r.queryNodes(ctx, query)
}

func (r *serviceCatalogRepo) GetChildren(ctx context.Context, parentID uuid.UUID, filter ServiceNodeFilter) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE parent_id = $1" +
		filter.where("") + " ORDER BY sort_order, name->>'ru'"
	return r.queryNodes(ctx, query, parentID)
}

func (r *serviceCatalogRepo) GetDescendants(ctx context.Context, ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.descendant_id WHERE p.ancestor_id = $1 AND p.depth > 0 AND sn.deleted_at IS NULL"
	args := []interface{}{ancestorID}
	if maxDepth != nil {
		query += " AND p.depth <= $2"
		args = append(args, *maxDepth)
	}
	query += " ORDER BY p.depth, sn.sort_order, sn.name->>'ru'"
	return r.queryNodes(ctx, query, args...)
}

// GetAncestors и GetVariantPath сохраняют удалённые узлы: их читают, чтобы
// отрисовать положение узла, в том числе для заказов по услуге, списанной
// позже. Живой узел никогда не может стоять под удалённым, поэтому путь живого
// узла тоже всегда живой.
func (r *serviceCatalogRepo) GetAncestors(ctx context.Context, descendantID uuid.UUID) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.ancestor_id WHERE p.descendant_id = $1 AND p.depth > 0 ORDER BY p.depth"
	return r.queryNodes(ctx, query, descendantID)
}

func (r *serviceCatalogRepo) GetVariantPath(ctx context.Context, variantID uuid.UUID) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.ancestor_id WHERE p.descendant_id = $1 ORDER BY p.depth"
	return r.queryNodes(ctx, query, variantID)
}

func (r *serviceCatalogRepo) GetActiveVariants(ctx context.Context) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE node_type = 'VARIANT'" +
		FilterActive.where("") + " ORDER BY sort_order, name->>'ru'"
	return r.queryNodes(ctx, query)
}

func (r *serviceCatalogRepo) ListNodesWithScript(ctx context.Context) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes" +
		" WHERE behavior_source IS NOT NULL AND behavior_source <> '' AND deleted_at IS NULL"
	return r.queryNodes(ctx, query)
}

func (r *serviceCatalogRepo) GetVariantWithCategory(ctx context.Context, id uuid.UUID) (*ServiceNode, []*ServiceNode, error) {
	variant, err := r.GetNodeByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if variant == nil {
		return nil, nil, errors.New("variant not found")
	}
	path, err := r.GetVariantPath(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return variant, path, nil
}

// HasChildren считает только живых детей: категорию, всё поддерево которой
// списано, можно списать следом.
func (r *serviceCatalogRepo) HasChildren(ctx context.Context, id uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_nodes WHERE parent_id = $1 AND deleted_at IS NULL`, id).Scan(&count)
	return count > 0, err
}

// HasOrders сообщает, заказывали ли узел когда-либо. Он больше не блокирует
// удаление — он говорит админ-панели, что списание услуги оставляет за собой
// историю заказов.
func (r *serviceCatalogRepo) HasOrders(ctx context.Context, id uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE service_variant_id = $1`, id).Scan(&count)
	return count > 0, err
}

func (r *serviceCatalogRepo) IsDescendantOf(ctx context.Context, candidateAncestor, candidateDescendant uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
        SELECT EXISTS(
            SELECT 1 FROM service_node_paths
            WHERE ancestor_id = $1 AND descendant_id = $2 AND depth > 0
        )
    `, candidateAncestor, candidateDescendant).Scan(&exists)
	return exists, err
}
