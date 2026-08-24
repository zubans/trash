package repository

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ServiceNodeType defines the type of a catalog node.
type ServiceNodeType string

const (
	ServiceNodeTypeCategory ServiceNodeType = "CATEGORY"
	ServiceNodeTypeVariant  ServiceNodeType = "VARIANT"
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
	BasePrice            *float64        `json:"base_price,omitempty"`
	IsAuction            bool            `json:"is_auction"`
	IsActive             bool            `json:"is_active"`
	SortOrder            int             `json:"sort_order"`
	RequiresVerification bool            `json:"requires_verification"`
	MinAge               int             `json:"min_age"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// IsCategory returns true if the node is a category.
func (n *ServiceNode) IsCategory() bool {
	return n.NodeType == ServiceNodeTypeCategory
}

// IsVariant returns true if the node is a service variant.
func (n *ServiceNode) IsVariant() bool {
	return n.NodeType == ServiceNodeTypeVariant
}

// ServiceCatalogRepository defines storage operations for the service catalog.
type ServiceCatalogRepository interface {
	// CRUD
	CreateNode(node *ServiceNode) error
	UpdateNode(node *ServiceNode) error
	DeleteNode(id uuid.UUID) error
	GetNodeByID(id uuid.UUID) (*ServiceNode, error)
	GetNodeByCode(code string) (*ServiceNode, error)

	// Tree navigation
	GetRootCategories(activeOnly bool) ([]*ServiceNode, error)
	GetChildren(parentID uuid.UUID, activeOnly bool) ([]*ServiceNode, error)
	GetDescendants(ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error)
	GetAncestors(descendantID uuid.UUID) ([]*ServiceNode, error)
	GetVariantPath(variantID uuid.UUID) ([]*ServiceNode, error)

	// Catalog helpers
	GetActiveVariants() ([]*ServiceNode, error)
	GetVariantWithCategory(id uuid.UUID) (*ServiceNode, []*ServiceNode, error)

	// Transactional helpers used by the service layer.
	HasChildren(id uuid.UUID) (bool, error)
	HasOrders(id uuid.UUID) (bool, error)
	IsDescendantOf(candidateAncestor, candidateDescendant uuid.UUID) (bool, error)
}

type serviceCatalogRepo struct {
	db *sql.DB
}

// NewServiceCatalogRepository creates a new service catalog repository.
func NewServiceCatalogRepository(db *sql.DB) ServiceCatalogRepository {
	_, _ = db.Exec(`ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS requires_verification BOOLEAN NOT NULL DEFAULT FALSE;`)
	_, _ = db.Exec(`ALTER TABLE service_nodes ADD COLUMN IF NOT EXISTS min_age INT NOT NULL DEFAULT 0;`)
	return &serviceCatalogRepo{db: db}
}

const serviceNodeColumns = `
    id, parent_id, code, name, description, node_type, base_price,
    is_auction, is_active, sort_order, COALESCE(requires_verification, false), COALESCE(min_age, 0), created_at, updated_at
`

func scanServiceNode(row *sql.Row) (*ServiceNode, error) {
	var n ServiceNode
	err := row.Scan(
		&n.ID, &n.ParentID, &n.Code, &n.Name, &n.Description, &n.NodeType,
		&n.BasePrice, &n.IsAuction, &n.IsActive, &n.SortOrder, &n.RequiresVerification, &n.MinAge, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func scanServiceNodeRows(rows *sql.Rows) (*ServiceNode, error) {
	var n ServiceNode
	err := rows.Scan(
		&n.ID, &n.ParentID, &n.Code, &n.Name, &n.Description, &n.NodeType,
		&n.BasePrice, &n.IsAuction, &n.IsActive, &n.SortOrder, &n.RequiresVerification, &n.MinAge, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *serviceCatalogRepo) CreateNode(node *ServiceNode) error {
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}
	now := time.Now()
	node.CreatedAt = now
	node.UpdatedAt = now

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
        INSERT INTO service_nodes (id, parent_id, code, name, description, node_type, base_price, is_auction, is_active, sort_order, requires_verification, min_age, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    `
	_, err = tx.Exec(query,
		node.ID, node.ParentID, node.Code, node.Name, node.Description,
		node.NodeType, node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.CreatedAt, node.UpdatedAt,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`SELECT rebuild_service_node_paths($1)`, node.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *serviceCatalogRepo) UpdateNode(node *ServiceNode) error {
	node.UpdatedAt = time.Now()

	existing, err := r.GetNodeByID(node.ID)
	if err != nil {
		return err
	}
	if existing == nil {
		return errors.New("node not found")
	}

	// node_type is immutable (H2).
	node.NodeType = existing.NodeType
	// code is immutable (H2); keep the existing value to avoid accidental changes.
	node.Code = existing.Code

	// Prevent cycles: cannot set parent to self or any descendant.
	if node.ParentID != nil {
		if *node.ParentID == node.ID {
			return errors.New("cannot set parent to self")
		}
		isDescendant, err := r.IsDescendantOf(*node.ParentID, node.ID)
		if err != nil {
			return err
		}
		if isDescendant {
			return errors.New("cannot set parent to descendant")
		}
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
        UPDATE service_nodes
        SET parent_id = $2, name = $3, description = $4, base_price = $5,
            is_auction = $6, is_active = $7, sort_order = $8, requires_verification = $9, min_age = $10, updated_at = $11
        WHERE id = $1
    `
	_, err = tx.Exec(query,
		node.ID, node.ParentID, node.Name, node.Description,
		node.BasePrice, node.IsAuction, node.IsActive, node.SortOrder,
		node.RequiresVerification, node.MinAge, node.UpdatedAt,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`SELECT rebuild_service_node_paths($1)`, node.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *serviceCatalogRepo) DeleteNode(id uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	node, err := r.GetNodeByID(id)
	if err != nil {
		return err
	}
	if node == nil {
		return errors.New("node not found")
	}

	hasChildren, err := r.HasChildren(id)
	if err != nil {
		return err
	}
	if hasChildren {
		return errors.New("cannot delete node with children")
	}

	hasOrders, err := r.HasOrders(id)
	if err != nil {
		return err
	}
	if hasOrders {
		return errors.New("cannot delete variant with existing orders")
	}

	_, err = tx.Exec(`DELETE FROM service_nodes WHERE id = $1`, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *serviceCatalogRepo) GetNodeByID(id uuid.UUID) (*ServiceNode, error) {
	row := r.db.QueryRow(
		"SELECT "+serviceNodeColumns+" FROM service_nodes WHERE id = $1", id,
	)
	return scanServiceNode(row)
}

func (r *serviceCatalogRepo) GetNodeByCode(code string) (*ServiceNode, error) {
	row := r.db.QueryRow(
		"SELECT "+serviceNodeColumns+" FROM service_nodes WHERE code = $1", code,
	)
	return scanServiceNode(row)
}

func (r *serviceCatalogRepo) GetRootCategories(activeOnly bool) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE parent_id IS NULL"
	if activeOnly {
		query += " AND is_active = TRUE"
	}
	query += " ORDER BY sort_order, name->>'ru'"

	rows, err := r.db.Query(query)
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

func (r *serviceCatalogRepo) GetChildren(parentID uuid.UUID, activeOnly bool) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE parent_id = $1"
	if activeOnly {
		query += " AND is_active = TRUE"
	}
	query += " ORDER BY sort_order, name->>'ru'"

	rows, err := r.db.Query(query, parentID)
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

func (r *serviceCatalogRepo) GetDescendants(ancestorID uuid.UUID, maxDepth *int) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.descendant_id WHERE p.ancestor_id = $1 AND p.depth > 0"
	args := []interface{}{ancestorID}
	if maxDepth != nil {
		query += " AND p.depth <= $2"
		args = append(args, *maxDepth)
	}
	query += " ORDER BY p.depth, sn.sort_order, sn.name->>'ru'"

	rows, err := r.db.Query(query, args...)
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

func (r *serviceCatalogRepo) GetAncestors(descendantID uuid.UUID) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.ancestor_id WHERE p.descendant_id = $1 AND p.depth > 0 ORDER BY p.depth"

	rows, err := r.db.Query(query, descendantID)
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

func (r *serviceCatalogRepo) GetVariantPath(variantID uuid.UUID) ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_node_paths p JOIN service_nodes sn ON sn.id = p.ancestor_id WHERE p.descendant_id = $1 ORDER BY p.depth"

	rows, err := r.db.Query(query, variantID)
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

func (r *serviceCatalogRepo) GetActiveVariants() ([]*ServiceNode, error) {
	query := "SELECT " + serviceNodeColumns + " FROM service_nodes WHERE node_type = 'VARIANT' AND is_active = TRUE ORDER BY sort_order, name->>'ru'"

	rows, err := r.db.Query(query)
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

func (r *serviceCatalogRepo) GetVariantWithCategory(id uuid.UUID) (*ServiceNode, []*ServiceNode, error) {
	variant, err := r.GetNodeByID(id)
	if err != nil {
		return nil, nil, err
	}
	if variant == nil {
		return nil, nil, errors.New("variant not found")
	}
	path, err := r.GetVariantPath(id)
	if err != nil {
		return nil, nil, err
	}
	return variant, path, nil
}

func (r *serviceCatalogRepo) HasChildren(id uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM service_nodes WHERE parent_id = $1`, id).Scan(&count)
	return count > 0, err
}

func (r *serviceCatalogRepo) HasOrders(id uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM orders WHERE service_variant_id = $1`, id).Scan(&count)
	return count > 0, err
}

func (r *serviceCatalogRepo) IsDescendantOf(candidateAncestor, candidateDescendant uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM service_node_paths
            WHERE ancestor_id = $1 AND descendant_id = $2 AND depth > 0
        )
    `, candidateAncestor, candidateDescendant).Scan(&exists)
	return exists, err
}
