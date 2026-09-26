package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// blockedPenalties — репозиторий штрафов, у которого один пользователь в
// тихой блокировке.
type blockedPenalties struct {
	repository.PenaltyRepository
	blocked uuid.UUID
}

func (p *blockedPenalties) Status(ctx context.Context, q repository.Querier, userID uuid.UUID, role string) (*repository.PenaltyStatus, error) {
	st := &repository.PenaltyStatus{UserID: userID, Role: role}
	if userID == p.blocked {
		until := time.Now().Add(time.Hour)
		st.SilentBlockEndsAt = &until
	}
	return st, nil
}

// Видимость каталога: тихая блокировка прячет всё, неверифицированный
// заказчик не видит услуги «только для верифицированных», исполнитель и
// аноним видят каталог целиком.
func TestServiceCatalog_VisibleTo(t *testing.T) {
	blocked := uuid.New()
	catalog := NewServiceCatalog(newMockCatalogRepo()).
		WithPenalties(NewPenaltyService(&blockedPenalties{blocked: blocked}, nil, nil, nil))
	nodes := []*repository.ServiceNode{
		{ID: uuid.New(), Code: "plain", IsActive: true},
		{ID: uuid.New(), Code: "verified_only", IsActive: true, RequiresVerification: true},
	}
	ctx := context.Background()

	codes := func(out []*repository.ServiceNode) []string {
		got := make([]string, 0, len(out))
		for _, n := range out {
			got = append(got, n.Code)
		}
		return got
	}
	if got := codes(catalog.VisibleTo(ctx, &repository.User{ID: blocked, Role: repository.RoleCustomer, Roles: []string{repository.RoleCustomer}}, nodes)); len(got) != 0 {
		t.Fatalf("silently blocked customer sees %v", got)
	}
	unverified := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Roles: []string{repository.RoleCustomer}}
	if got := codes(catalog.VisibleTo(ctx, unverified, nodes)); len(got) != 1 || got[0] != "plain" {
		t.Fatalf("unverified customer sees %v, want [plain]", got)
	}
	verified := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Roles: []string{repository.RoleCustomer}, Verified: true}
	if got := codes(catalog.VisibleTo(ctx, verified, nodes)); len(got) != 2 {
		t.Fatalf("verified customer sees %v", got)
	}
	executor := &repository.User{ID: uuid.New(), Role: repository.RoleExecutor, Roles: []string{repository.RoleExecutor}}
	if got := codes(catalog.VisibleTo(ctx, executor, nodes)); len(got) != 2 {
		t.Fatalf("executor sees %v", got)
	}
	if got := codes(catalog.VisibleTo(ctx, nil, nodes)); len(got) != 2 {
		t.Fatalf("anonymous sees %v", got)
	}
}

// Дерево собирается из плоского списка с сохранением порядка братьев; узел
// без родителя в списке становится корнем, а не теряется.
func TestBuildTree(t *testing.T) {
	root := uuid.New()
	child1, child2, grandchild, orphan := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	missingParent := uuid.New()
	nodes := []*repository.ServiceNode{
		{ID: root, Code: "root"},
		{ID: child1, ParentID: &root, Code: "child1"},
		{ID: grandchild, ParentID: &child1, Code: "grandchild"},
		{ID: child2, ParentID: &root, Code: "child2"},
		{ID: orphan, ParentID: &missingParent, Code: "orphan"},
	}
	tree := buildTree(nodes)
	if len(tree) != 2 || tree[0].Node.Code != "root" || tree[1].Node.Code != "orphan" {
		t.Fatalf("roots: %+v", tree)
	}
	children := tree[0].Children
	if len(children) != 2 || children[0].Node.Code != "child1" || children[1].Node.Code != "child2" {
		t.Fatalf("children of root: %+v", children)
	}
	if len(children[0].Children) != 1 || children[0].Children[0].Node.Code != "grandchild" || len(children[1].Children) != 0 {
		t.Fatalf("grandchildren: %+v", children[0].Children)
	}
	if out, _ := json.Marshal(tree[1]); string(out) != `{"node":{"id":"`+orphan.String()+`","parent_id":"`+missingParent.String()+`","code":"orphan","name":null,"node_type":"","is_auction":false,"is_active":false,"sort_order":0,"requires_verification":false,"min_age":0,"moderator_only":false,"created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z"},"children":[]}` {
		t.Fatalf("leaf children must serialize as [], got %s", out)
	}
}

// Правила узла: у варианта есть цена, у аукциона она нулевая, у категории
// цены нет (ноль от общей формы — это «нет цены»), код по шаблону.
func TestServiceCatalogAdmin_ValidateNode(t *testing.T) {
	admin := NewServiceCatalogAdmin(newMockCatalogRepo(), nil)
	price := money.FromRubles(100)
	zero := money.Amount(0)
	name := repository.LocalizedText{"ru": "Услуга"}

	cases := map[string]struct {
		node   repository.ServiceNode
		create bool
		want   string
	}{
		"code required":       {repository.ServiceNode{NodeType: repository.ServiceNodeTypeVariant, Name: name, BasePrice: &price}, true, "code is required"},
		"code pattern":        {repository.ServiceNode{Code: "Bad Code", NodeType: repository.ServiceNodeTypeVariant, Name: name, BasePrice: &price}, true, "code must match"},
		"node type":           {repository.ServiceNode{Code: "ok", NodeType: "LEAF", Name: name}, true, "node_type must be"},
		"name ru":             {repository.ServiceNode{Code: "ok", NodeType: repository.ServiceNodeTypeVariant, Name: repository.LocalizedText{"en": "x"}}, true, "name must contain"},
		"variant price":       {repository.ServiceNode{NodeType: repository.ServiceNodeTypeVariant, Name: name}, false, "VARIANT must have base_price"},
		"auction price":       {repository.ServiceNode{NodeType: repository.ServiceNodeTypeVariant, Name: name, BasePrice: &price, IsAuction: true}, false, "auction variant base_price must be 0"},
		"category with price": {repository.ServiceNode{NodeType: repository.ServiceNodeTypeCategory, Name: name, BasePrice: &price}, false, "CATEGORY cannot have base_price"},
		"unknown behavior":    {repository.ServiceNode{NodeType: repository.ServiceNodeTypeCategory, Name: name, BehaviorCode: "nope"}, false, "unknown behavior_code"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			err := admin.validateNode(&tc.node, tc.create)
			if err == nil || !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want validation error containing %q", err, tc.want)
			}
		})
	}
	category := repository.ServiceNode{NodeType: repository.ServiceNodeTypeCategory, Name: name, BasePrice: &zero}
	if err := admin.validateNode(&category, false); err != nil || category.BasePrice != nil {
		t.Fatalf("category with zero price: err=%v price=%v", err, category.BasePrice)
	}
}

// Форма узла не несёт серверных полей: id, время создания и списания в
// строку из тела запроса не попадают.
func TestServiceNodeForm_IgnoresServerFields(t *testing.T) {
	body := `{"id":"` + uuid.New().String() + `","code":"x","node_type":"VARIANT","name":{"ru":"X"},
	          "base_price":100,"created_at":"2000-01-01T00:00:00Z","updated_at":"2000-01-01T00:00:00Z",
	          "deleted_at":"2000-01-01T00:00:00Z"}`
	var form ServiceNodeForm
	if err := json.Unmarshal([]byte(body), &form); err != nil {
		t.Fatal(err)
	}
	node := form.node()
	if node.ID != uuid.Nil || node.DeletedAt != nil || !node.CreatedAt.IsZero() || !node.UpdatedAt.IsZero() {
		t.Fatalf("server fields leaked from the body: %+v", node)
	}
	if node.Code != "x" || node.BasePrice == nil || *node.BasePrice != money.FromRubles(100) {
		t.Fatalf("editable fields lost: %+v", node)
	}
}
