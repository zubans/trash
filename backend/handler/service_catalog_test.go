package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// mockCatalogRepo is an in-memory service catalog with the same delete
// semantics as the database: nothing is ever removed, nodes are marked.
type mockCatalogRepo struct {
	nodes     map[uuid.UUID]*repository.ServiceNode
	withOrder map[uuid.UUID]bool
}

func newMockCatalogRepo() *mockCatalogRepo {
	return &mockCatalogRepo{
		nodes:     make(map[uuid.UUID]*repository.ServiceNode),
		withOrder: make(map[uuid.UUID]bool),
	}
}

func (m *mockCatalogRepo) add(node *repository.ServiceNode) *repository.ServiceNode {
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}
	m.nodes[node.ID] = node
	return node
}

func (m *mockCatalogRepo) CreateNode(ctx context.Context, node *repository.ServiceNode) error {
	m.add(node)
	return nil
}

func (m *mockCatalogRepo) UpdateNode(ctx context.Context, node *repository.ServiceNode) error {
	existing, ok := m.nodes[node.ID]
	if !ok {
		return repository.ErrServiceNodeNotFound
	}
	if existing.IsDeleted() {
		return repository.ErrServiceNodeDeleted
	}
	node.CreatedAt = existing.CreatedAt
	m.nodes[node.ID] = node
	return nil
}

func (m *mockCatalogRepo) DeleteNode(ctx context.Context, id uuid.UUID) error {
	node, ok := m.nodes[id]
	if !ok {
		return repository.ErrServiceNodeNotFound
	}
	if node.IsDeleted() {
		return repository.ErrServiceNodeDeleted
	}
	if children, _ := m.HasChildren(context.Background(), id); children {
		return repository.ErrServiceNodeHasChildren
	}
	now := time.Now()
	node.DeletedAt = &now
	node.IsActive = false
	return nil
}

func (m *mockCatalogRepo) RestoreNode(ctx context.Context, id uuid.UUID) error {
	node, ok := m.nodes[id]
	if !ok {
		return repository.ErrServiceNodeNotFound
	}
	if !node.IsDeleted() {
		return repository.ErrServiceNodeNotDeleted
	}
	if node.ParentID != nil {
		if parent, ok := m.nodes[*node.ParentID]; !ok || parent.IsDeleted() {
			return repository.ErrServiceNodeParentDeleted
		}
	}
	node.DeletedAt = nil
	return nil
}

func (m *mockCatalogRepo) GetNodeByID(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, error) {
	if node, ok := m.nodes[id]; ok {
		return node, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockCatalogRepo) GetNodeByCode(ctx context.Context, code string) (*repository.ServiceNode, error) {
	for _, n := range m.nodes {
		if n.Code == code && !n.IsDeleted() {
			return n, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockCatalogRepo) matches(n *repository.ServiceNode, filter repository.ServiceNodeFilter) bool {
	if n.IsDeleted() && !filter.IncludeDeleted {
		return false
	}
	return n.IsActive || !filter.ActiveOnly
}

func (m *mockCatalogRepo) GetRootCategories(ctx context.Context, filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	out := []*repository.ServiceNode{}
	for _, n := range m.nodes {
		if n.ParentID == nil && m.matches(n, filter) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *mockCatalogRepo) GetChildren(ctx context.Context, parentID uuid.UUID, filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	out := []*repository.ServiceNode{}
	for _, n := range m.nodes {
		if n.ParentID != nil && *n.ParentID == parentID && m.matches(n, filter) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *mockCatalogRepo) GetDescendants(ctx context.Context, ancestorID uuid.UUID, maxDepth *int) ([]*repository.ServiceNode, error) {
	return m.GetChildren(context.Background(), ancestorID, repository.FilterLive)
}

func (m *mockCatalogRepo) GetAncestors(ctx context.Context, descendantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}

func (m *mockCatalogRepo) GetVariantPath(ctx context.Context, variantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}

func (m *mockCatalogRepo) GetActiveVariants(ctx context.Context) ([]*repository.ServiceNode, error) {
	out := []*repository.ServiceNode{}
	for _, n := range m.nodes {
		if n.IsVariant() && n.IsActive && !n.IsDeleted() {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *mockCatalogRepo) GetVariantWithCategory(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, []*repository.ServiceNode, error) {
	node, err := m.GetNodeByID(context.Background(), id)
	return node, nil, err
}

func (m *mockCatalogRepo) HasChildren(ctx context.Context, id uuid.UUID) (bool, error) {
	children, _ := m.GetChildren(context.Background(), id, repository.FilterLive)
	return len(children) > 0, nil
}

func (m *mockCatalogRepo) HasOrders(ctx context.Context, id uuid.UUID) (bool, error) {
	return m.withOrder[id], nil
}

func (m *mockCatalogRepo) IsDescendantOf(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return false, nil
}

// catalogTestEnv wires the handler into a router so that URL params resolve the
// same way they do in main.
type catalogTestEnv struct {
	repo    *mockCatalogRepo
	router  chi.Router
	rootCat *repository.ServiceNode
	variant *repository.ServiceNode
}

func newCatalogTestEnv() *catalogTestEnv {
	repo := newMockCatalogRepo()
	h := NewServiceCatalogHandler(repo)

	r := chi.NewRouter()
	r.Get("/admin/service-nodes", h.AdminListNodes)
	r.Put("/admin/service-nodes/{id}", h.AdminUpdateNode)
	r.Delete("/admin/service-nodes/{id}", h.AdminDeleteNode)
	r.Post("/admin/service-nodes/{id}/restore", h.AdminRestoreNode)

	root := repo.add(&repository.ServiceNode{
		ID:       uuid.New(),
		Code:     "dog_walking",
		Name:     repository.LocalizedText{"ru": "Выгул собак"},
		NodeType: repository.ServiceNodeTypeCategory,
		IsActive: true,
	})
	price := money.FromRubles(150)
	variant := repo.add(&repository.ServiceNode{
		ID:        uuid.New(),
		ParentID:  &root.ID,
		Code:      "dog_walk_morning",
		Name:      repository.LocalizedText{"ru": "Утренний выгул"},
		NodeType:  repository.ServiceNodeTypeVariant,
		BasePrice: &price,
		IsActive:  true,
	})

	return &catalogTestEnv{repo: repo, router: r, rootCat: root, variant: variant}
}

func (e *catalogTestEnv) do(t *testing.T, method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// TestAdminUpdateNode_VariantKeepsItsTypeWhenBodyOmitsIt pins the fix for the
// admin panel bug: node_type is immutable and therefore not sent on update, and
// validating the request against its own empty type rejected every variant edit
// — including a plain "switch it off" — with "CATEGORY cannot have base_price".
func TestAdminUpdateNode_VariantKeepsItsTypeWhenBodyOmitsIt(t *testing.T) {
	env := newCatalogTestEnv()

	rec := env.do(t, http.MethodPut, "/admin/service-nodes/"+env.variant.ID.String(), map[string]interface{}{
		"parent_id":  env.rootCat.ID,
		"name":       map[string]string{"ru": "Утренний выгул"},
		"base_price": 150,
		"is_active":  false,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ := env.repo.GetNodeByID(context.Background(), env.variant.ID)
	if stored.NodeType != repository.ServiceNodeTypeVariant {
		t.Errorf("node_type changed to %q", stored.NodeType)
	}
	if stored.Code != "dog_walk_morning" {
		t.Errorf("code changed to %q", stored.Code)
	}
	if stored.IsActive {
		t.Error("expected the variant to be switched off")
	}
}

// TestAdminUpdateNode_CategoryZeroPriceIsNoPrice covers the other half of the
// same form: a shared form sends base_price 0 for a category.
func TestAdminUpdateNode_CategoryZeroPriceIsNoPrice(t *testing.T) {
	env := newCatalogTestEnv()

	rec := env.do(t, http.MethodPut, "/admin/service-nodes/"+env.rootCat.ID.String(), map[string]interface{}{
		"name":       map[string]string{"ru": "Выгул собак"},
		"base_price": 0,
		"is_active":  false,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	stored, _ := env.repo.GetNodeByID(context.Background(), env.rootCat.ID)
	if stored.BasePrice != nil {
		t.Errorf("expected no base price on the category, got %v", stored.BasePrice)
	}
}

func TestAdminUpdateNode_CategoryWithRealPriceIsRejected(t *testing.T) {
	env := newCatalogTestEnv()

	rec := env.do(t, http.MethodPut, "/admin/service-nodes/"+env.rootCat.ID.String(), map[string]interface{}{
		"name":       map[string]string{"ru": "Выгул собак"},
		"base_price": 500,
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAdminDeleteNode_KeepsOrderedVariant is the fix for "cannot delete variant
// with existing orders": the delete goes through and the row survives for the
// orders that reference it.
func TestAdminDeleteNode_KeepsOrderedVariant(t *testing.T) {
	env := newCatalogTestEnv()
	env.repo.withOrder[env.variant.ID] = true

	rec := env.do(t, http.MethodDelete, "/admin/service-nodes/"+env.variant.ID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Soft      bool `json:"soft"`
		HadOrders bool `json:"had_orders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Soft || !resp.HadOrders {
		t.Errorf("expected a soft delete reported with order history, got %+v", resp)
	}

	stored, err := env.repo.GetNodeByID(context.Background(), env.variant.ID)
	if err != nil {
		t.Fatalf("deleted variant no longer resolves: %v", err)
	}
	if !stored.IsDeleted() || stored.IsActive {
		t.Errorf("expected a deleted, inactive node, got deleted=%v active=%v", stored.IsDeleted(), stored.IsActive)
	}
	if stored.IsOrderable() {
		t.Error("a deleted variant must not be orderable")
	}
}

func TestAdminDeleteNode_CategoryWithLiveChildrenIsRejected(t *testing.T) {
	env := newCatalogTestEnv()

	rec := env.do(t, http.MethodDelete, "/admin/service-nodes/"+env.rootCat.ID.String(), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	// Retiring the child first frees the category.
	if rec := env.do(t, http.MethodDelete, "/admin/service-nodes/"+env.variant.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("expected the variant delete to succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, http.MethodDelete, "/admin/service-nodes/"+env.rootCat.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("expected the category delete to succeed, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminUpdateNode_DeletedNodeIsRejected(t *testing.T) {
	env := newCatalogTestEnv()
	if err := env.repo.DeleteNode(context.Background(), env.variant.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rec := env.do(t, http.MethodPut, "/admin/service-nodes/"+env.variant.ID.String(), map[string]interface{}{
		"name":       map[string]string{"ru": "Утренний выгул"},
		"base_price": 150,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminRestoreNode_ComesBackSwitchedOff(t *testing.T) {
	env := newCatalogTestEnv()
	if err := env.repo.DeleteNode(context.Background(), env.variant.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rec := env.do(t, http.MethodPost, "/admin/service-nodes/"+env.variant.ID.String()+"/restore", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	stored, _ := env.repo.GetNodeByID(context.Background(), env.variant.ID)
	if stored.IsDeleted() {
		t.Error("expected the node to be restored")
	}
	if stored.IsActive {
		t.Error("a restored node must stay switched off until an admin publishes it")
	}

	// Restoring twice is a conflict, not a silent success.
	if rec := env.do(t, http.MethodPost, "/admin/service-nodes/"+env.variant.ID.String()+"/restore", nil); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 on the second restore, got %d", rec.Code)
	}
}

func TestAdminListNodes_HidesDeletedUnlessAsked(t *testing.T) {
	env := newCatalogTestEnv()
	if err := env.repo.DeleteNode(context.Background(), env.variant.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	countVariants := func(url string) int {
		rec := env.do(t, http.MethodGet, url, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", url, rec.Code)
		}
		var tree []struct {
			Children []struct {
				Node repository.ServiceNode `json:"node"`
			} `json:"children"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &tree); err != nil {
			t.Fatalf("decode tree: %v", err)
		}
		total := 0
		for _, root := range tree {
			total += len(root.Children)
		}
		return total
	}

	if got := countVariants("/admin/service-nodes"); got != 0 {
		t.Errorf("expected the deleted variant to be hidden, got %d children", got)
	}
	if got := countVariants("/admin/service-nodes?include_deleted=true"); got != 1 {
		t.Errorf("expected the deleted variant to be listed, got %d children", got)
	}
}
