package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// The service constructor is now a script editor. These tests cover the part of
// it the browser cannot be trusted with: a script is compiled before the node is
// written, and a broken one never reaches the database.

func newScriptTestEnv(t *testing.T) (*catalogTestEnv, *service.Behaviors) {
	t.Helper()
	env := newCatalogTestEnv()

	engine := behavior.New(behavior.DefaultLimits)
	behaviors := service.NewBehaviors(engine, nil).WithCatalog(env.repo)
	h := NewServiceCatalogHandler(env.repo).WithBehaviors(behaviors)

	r := chi.NewRouter()
	r.Get("/admin/service-behaviors", h.AdminListBehaviors)
	r.Post("/admin/service-nodes", h.AdminCreateNode)
	r.Put("/admin/service-nodes/{id}", h.AdminUpdateNode)
	env.router = r
	return env, behaviors
}

const validNodeScript = `
MANIFEST = {"name": "Спец", "events": []}

def visible(f):
    return f.user != None
`

func TestAdminCreateNode_CompilesTheScriptBeforeSaving(t *testing.T) {
	env, behaviors := newScriptTestEnv(t)

	rec := env.do(t, http.MethodPost, "/admin/service-nodes", map[string]interface{}{
		"code":               "special_service",
		"node_type":          "VARIANT",
		"name":               map[string]string{"ru": "Спец-услуга"},
		"base_price":         0,
		"is_active":          true,
		"behavior_constants": "GREETING = \"привет\"\n",
		"behavior_source":    validNodeScript,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created repository.ServiceNode
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	stored, err := env.repo.GetNodeByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("stored node: %v", err)
	}
	if !stored.HasOwnScript() {
		t.Fatal("the node was saved without its script")
	}

	// Registered in the running engine, so the rule applies to the next request
	// rather than after a restart.
	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: "ACTIVE"}
	if !behaviors.Visible(context.Background(), customer, stored, nil) {
		t.Error("the saved script is not running: the node is hidden from everyone")
	}
	if behaviors.Visible(context.Background(), nil, stored, nil) {
		t.Error("the saved script is not running: it hides nobody")
	}
}

func TestAdminCreateNode_RefusesABrokenScript(t *testing.T) {
	env, _ := newScriptTestEnv(t)

	rec := env.do(t, http.MethodPost, "/admin/service-nodes", map[string]interface{}{
		"code":            "broken_service",
		"node_type":       "VARIANT",
		"name":            map[string]string{"ru": "Сломанная"},
		"base_price":      0,
		"behavior_source": "def visible(f)\n    return True\n",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a script that does not compile, got %d: %s", rec.Code, rec.Body.String())
	}
	// The admin reads this in the editor, so it has to say what is wrong.
	if !strings.Contains(rec.Body.String(), "не компилируется") {
		t.Errorf("unhelpful error: %s", rec.Body.String())
	}
	if len(env.repo.nodes) != 2 {
		t.Errorf("a node with a broken script was written: %d nodes stored", len(env.repo.nodes))
	}
}

// Clearing the script in the editor makes the node an ordinary service again.
func TestAdminUpdateNode_ClearingTheScript(t *testing.T) {
	env, behaviors := newScriptTestEnv(t)

	special := env.repo.add(&repository.ServiceNode{
		ID:             uuid.New(),
		Code:           "special_existing",
		Name:           repository.LocalizedText{"ru": "Спец"},
		NodeType:       repository.ServiceNodeTypeVariant,
		BasePrice:      env.variant.BasePrice,
		IsActive:       true,
		BehaviorSource: "MANIFEST = {\"name\": \"x\"}\n\ndef visible(f):\n    return False\n",
	})
	if err := behaviors.SyncNode(special); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if behaviors.Visible(context.Background(), nil, special, nil) {
		t.Fatal("the fixture script is not in force")
	}

	rec := env.do(t, http.MethodPut, "/admin/service-nodes/"+special.ID.String(), map[string]interface{}{
		"name":       map[string]string{"ru": "Спец"},
		"base_price": 150,
		"is_active":  true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	stored, _ := env.repo.GetNodeByID(context.Background(), special.ID)
	if stored.HasOwnScript() {
		t.Fatal("the script was not cleared")
	}
	if !behaviors.Visible(context.Background(), nil, stored, nil) {
		t.Error("the node is still judged by a script it no longer has")
	}
}

// The library is what the editor offers as a template, so it has to carry the
// text and not only the names.
func TestAdminListBehaviors_ReturnsTheScriptText(t *testing.T) {
	env, behaviors := newScriptTestEnv(t)
	if err := behaviors.Engine().CompileFiles("sample", []behavior.SourceFile{
		{Name: behavior.ConfigFile, Src: []byte("REWARD = 10\n")},
		{Name: "behavior.star", Src: []byte("MANIFEST = {\"name\": \"Пример\"}\n")},
	}); err != nil {
		t.Fatalf("compile sample: %v", err)
	}

	rec := env.do(t, http.MethodGet, "/admin/service-behaviors", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var library []behavior.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &library); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(library) != 1 {
		t.Fatalf("expected the one library behaviour, got %d", len(library))
	}
	if !strings.Contains(library[0].ConstantsSource, "REWARD = 10") {
		t.Errorf("constants text missing: %q", library[0].ConstantsSource)
	}
	if !strings.Contains(library[0].Source, "MANIFEST") {
		t.Errorf("script text missing: %q", library[0].Source)
	}
}

// An ordinary catalog must not pay for the scripted-service machinery: no claim
// lookup, no script call, nothing but the rules it always had.
func TestOrdinaryCatalogDoesNotTouchTheBehaviorStores(t *testing.T) {
	env, behaviors := newScriptTestEnv(t)
	claims := &countingClaims{}
	behaviors = service.NewBehaviors(behaviors.Engine(), claims).WithCatalog(env.repo)

	h := NewServiceCatalogHandler(env.repo).WithBehaviors(behaviors)
	r := chi.NewRouter()
	r.Get("/service-variants", h.ListVariants)
	env.router = r

	if rec := env.do(t, http.MethodGet, "/service-variants", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if claims.calls != 0 {
		t.Errorf("a catalog of ordinary services made %d claim queries", claims.calls)
	}
}

// countingClaims records whether the claim store was consulted at all.
type countingClaims struct {
	repository.ServiceClaimRepository
	calls int
}

func (c *countingClaims) CountsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	c.calls++
	return map[uuid.UUID]int{}, nil
}
