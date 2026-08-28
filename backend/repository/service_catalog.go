package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// ServiceNodeType defines the type of a catalog node.
type ServiceNodeType string

const (
	ServiceNodeTypeCategory ServiceNodeType = "CATEGORY"
	ServiceNodeTypeVariant  ServiceNodeType = "VARIANT"
)

// Catalog errors the handler layer maps to HTTP status codes.
var (
	ErrServiceNodeNotFound      = errors.New("service node not found")
	ErrServiceNodeDeleted       = errors.New("service node is deleted")
	ErrServiceNodeNotDeleted    = errors.New("service node is not deleted")
	ErrServiceNodeHasChildren   = errors.New("cannot delete a node that still has children")
	ErrServiceNodeCodeTaken     = errors.New("another node already uses this code")
	ErrServiceNodeParentDeleted = errors.New("parent category is deleted")
)

// LocalizedText stores translations for one field.
type LocalizedText map[string]string

// Value implements the driver.Valuer interface for JSONB storage.
func (lt LocalizedText) Value() (driver.Value, error) {
	if lt == nil {
		return nil, nil
	}
	return json.Marshal(lt)
}

// Scan implements the sql.Scanner interface for JSONB retrieval.
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

// ServiceNode represents a node in the service catalog tree.
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
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	// DeletedAt is set when the node was retired. The row stays in place so
	// that orders which reference it keep resolving; nothing in the catalog
	// offers it any more.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// IsCategory returns true if the node is a category.
func (n *ServiceNode) IsCategory() bool {
	return n.NodeType == ServiceNodeTypeCategory
}

// IsVariant returns true if the node is a service variant.
func (n *ServiceNode) IsVariant() bool {
	return n.NodeType == ServiceNodeTypeVariant
}

// IsDeleted reports whether the node was soft-deleted.
func (n *ServiceNode) IsDeleted() bool {
	return n != nil && n.DeletedAt != nil
}

// IsOrderable reports whether new orders may be placed for this node.
func (n *ServiceNode) IsOrderable() bool {
	return n != nil && n.IsVariant() && n.IsActive && !n.IsDeleted()
}

// ServiceNodeFilter narrows a tree query. The zero value returns every live
// node, active or not, which is what the admin panel shows by default.
type ServiceNodeFilter struct {
	// ActiveOnly drops nodes that are switched off in the app.
	ActiveOnly bool
	// IncludeDeleted keeps soft-deleted nodes in the result. Only the admin
	// catalog screen asks for them.
	IncludeDeleted bool
}

// FilterActive returns only the nodes the app may offer to users.
var FilterActive = ServiceNodeFilter{ActiveOnly: true}

// FilterLive returns active and inactive nodes but no deleted ones.
var FilterLive = ServiceNodeFilter{}

// where renders the filter as SQL predicates appended to an existing WHERE
// clause. col is the table alias holding the node columns.
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

// ServiceCatalogRepository defines storage operations for the service catalog.
type ServiceCatalogRepository interface {
	// CRUD
	CreateNode(ctx context.Context, node *ServiceNode) error
	UpdateNode(ctx context.Context, node *ServiceNode) error
	// DeleteNode soft-deletes a node: the row survives so that historical
	// orders keep resolving, and the catalog stops offering it.
	DeleteNode(ctx context.Context, id uuid.UUID) error
	// RestoreNode brings a soft-deleted node back, switched off.
	RestoreNode(ctx context.Context, id uuid.UUID) error
	// GetNodeByID returns the node even when it was deleted, because orders
	// placed before the deletion still have to render their service.
	GetNodeByID(ctx context.Context, id uuid.UUID) (*ServiceNode, error)
	// GetNodeByCode looks up a live node only; a deleted code is free to be
	// taken by a new node.
	GetNodeByCode(ctx context.Context, code string) (*ServiceNode, error)

	// Tree navigation
	GetRootCategories(ctx context.Context, filter ServiceNodeFilter) ([]*ServiceNode, error)
	GetChildren(ctx context.Context, parentID uuid.UUID, filter ServiceNodeFilter) ([]*ServiceNode, error)
	GetDescendants(ctx context.Context, ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error)
	GetAncestors(ctx context.Context, descendantID uuid.UUID) ([]*ServiceNode, error)
	GetVariantPath(ctx context.Context, variantID uuid.UUID) ([]*ServiceNode, error)

	// Catalog helpers
	GetActiveVariants(ctx context.Context) ([]*ServiceNode, error)
	GetVariantWithCategory(ctx context.Context, id uuid.UUID) (*ServiceNode, []*ServiceNode, error)

	// Transactional helpers used by the service layer.
	HasChildren(ctx context.Context, id uuid.UUID) (bool, error)
	HasOrders(ctx context.Context, id uuid.UUID) (bool, error)
	IsDescendantOf(ctx context.Context, candidateAncestor, candidateDescendant uuid.UUID) (bool, error)
}

type serviceCatalogRepo struct {
	db *sql.DB
}

// NewServiceCatalogRepository creates a new service catalog repository.
func NewServiceCatalogRepository(db *sql.DB) ServiceCatalogRepository {
	return &serviceCatalogRepo{db: db}
}

const serviceNodeColumns = `
    id, parent_id, code, name, description, node_type, base_price,
    is_auction, is_active, sort_order, COALESCE(requires_verification, false), COALESCE(min_age, 0), created_at, updated_at, deleted_at
`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanServiceNodeInto(s rowScanner) (*ServiceNode, error) {
	var n ServiceNode
	err := s.Scan(
		&n.ID, &n.ParentID, &n.Code, &n.Name, &n.Description, &n.NodeType,
		&n.BasePrice, &n.IsAuction, &n.IsActive, &n.SortOrder, &n.RequiresVerification,
		&n.MinAge, &n.CreatedAt, &n.UpdatedAt, &n.DeletedAt,
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

	// The unique index only covers live nodes, so report the collision with a
	// message the admin panel can show instead of a driver error.
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
        INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, requires_verification, min_age, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    `
	_, err = tx.ExecContext(ctx, query,
		node.ID, node.ParentID, node.Code, node.Name, node.Description,
		node.NodeType, node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.CreatedAt, node.UpdatedAt,
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
	// A deleted node is restored first, then edited: editing it in place would
	// let an admin change a service the catalog no longer lists.
	if existing.IsDeleted() {
		return ErrServiceNodeDeleted
	}

	// node_type is immutable (H2).
	node.NodeType = existing.NodeType
	// code is immutable (H2); keep the existing value to avoid accidental changes.
	node.Code = existing.Code
	node.DeletedAt = nil

	if node.ParentID != nil {
		// Prevent cycles: cannot set parent to self or any descendant.
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
		// Moving a live node under a deleted category would hide it from the
		// catalog without ever deleting it.
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
            is_auction = $6, is_active = $7, sort_order = $8, requires_verification = $9, min_age = $10, updated_at = $11
        WHERE id = $1 AND deleted_at IS NULL
    `
	_, err = tx.ExecContext(ctx, query,
		node.ID, node.ParentID, node.Name, node.Description,
		node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.UpdatedAt,
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

// DeleteNode retires a node without removing the row. Orders keep a foreign key
// to the variant they were placed for, so a hard delete would either fail or
// take the order history with it; marking the node keeps that history readable
// while the catalog stops offering the service.
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

	// Children are deleted from the leaves up: a category whose children were
	// all retired can go, one that still holds live children cannot.
	hasChildren, err := r.HasChildren(ctx, id)
	if err != nil {
		return err
	}
	if hasChildren {
		return ErrServiceNodeHasChildren
	}

	// is_active is switched off in the same statement: every catalog query
	// already filters on it, so the service disappears even from a caller that
	// predates deleted_at. The database check constraint pins the pair.
	res, err := r.db.ExecContext(ctx, `
        UPDATE service_nodes
        SET deleted_at = now(), is_active = FALSE, updated_at = now()
        WHERE id = $1 AND deleted_at IS NULL
    `, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Someone deleted it between the read and the update.
		return ErrServiceNodeDeleted
	}
	return nil
}

// RestoreNode clears the deletion mark. The node comes back switched off, so an
// admin has to re-enable it deliberately before customers see it again.
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

	// Restoring into a deleted branch would produce a node nobody can reach.
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

	// The code was free while the node was deleted, so a new node may hold it.
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

// codeTaken reports whether a live node other than exclude uses the code.
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

// GetAncestors and GetVariantPath keep deleted nodes: they are read to render a
// node's position, including for orders placed on a service that was retired
// afterwards. A live node can never sit under a deleted one, so a live node's
// path is always live too.
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

// HasChildren counts live children only: a category whose whole subtree was
// retired can be retired in turn.
func (r *serviceCatalogRepo) HasChildren(ctx context.Context, id uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_nodes WHERE parent_id = $1 AND deleted_at IS NULL`, id).Scan(&count)
	return count > 0, err
}

// HasOrders reports whether the node was ever ordered. It no longer blocks
// deletion — it tells the admin panel that retiring the service leaves order
// history behind.
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
