package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Особые услуги: скрипт, написанный в админ-панели и хранимый на узле.
// Эти тесты фиксируют контракт, на который опирается редактор: скрипт, который
// не компилируется, отклоняется до сохранения, а тот, что компилируется,
// действует для этого узла и ни для какого другого.

const nodeScriptConstants = `
REWARD = 50
MSG_CLOSED = "услуга закрыта"
`

const nodeScriptSource = `
MANIFEST = {
    "name": "Тест",
    "once_per_user": True,
    "events": ["order.executed"],
}

def visible(f):
    return f.user != None and f.claims == 0

def can_order(f):
    if f.user == None:
        return MSG_CLOSED
    return None

def price(f):
    return 0
`

func nodeWithScript(constants, source string) *repository.ServiceNode {
	free := money.Zero
	return &repository.ServiceNode{
		ID:                uuid.New(),
		Code:              "special_service",
		NodeType:          repository.ServiceNodeTypeVariant,
		BasePrice:         &free,
		IsActive:          true,
		BehaviorConstants: constants,
		BehaviorSource:    source,
	}
}

func newBehaviorsForTest() *Behaviors {
	return NewBehaviors(behavior.New(behavior.DefaultLimits), newVerificationClaims())
}

func TestNodeScriptIsCompiledAndApplied(t *testing.T) {
	ctx := context.Background()
	behaviors := newBehaviorsForTest()
	node := nodeWithScript(nodeScriptConstants, nodeScriptSource)

	if err := behaviors.Validate(node); err != nil {
		t.Fatalf("the script must compile: %v", err)
	}
	if err := behaviors.SyncNode(node); err != nil {
		t.Fatalf("sync: %v", err)
	}

	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: "ACTIVE"}
	if !behaviors.Visible(ctx, customer, node, nil) {
		t.Error("the node's own visible hook did not run")
	}
	if behaviors.Visible(ctx, customer, node, map[uuid.UUID]int{node.ID: 1}) {
		t.Error("claims are not reaching the node's script: it stayed visible after one order")
	}
	if err := behaviors.CanOrder(ctx, customer, node); err != nil {
		t.Errorf("can_order refused a valid customer: %v", err)
	}
	price, ok, err := behaviors.Price(ctx, node)
	if err != nil || !ok || !price.IsZero() {
		t.Errorf("price = %s (set: %v, err: %v), want the script's 0", price, ok, err)
	}
	// Файл констант — отдельный скрипт; логика выше читает из него MSG_CLOSED,
	// поэтому анонимный посетитель получает ровно это сообщение.
	err = behaviors.CanOrder(ctx, nil, node)
	if err == nil || !strings.Contains(err.Error(), "услуга закрыта") {
		t.Errorf("refusal = %v, want the message from the node's constants", err)
	}
}

// Сломанный скрипт не должен доходить до базы: сохранённый, он провалил бы все
// проверки узла, а заказчик прочёл бы это как исчезновение услуги.
func TestBrokenNodeScriptIsRefusedBeforeSaving(t *testing.T) {
	behaviors := newBehaviorsForTest()
	node := nodeWithScript("", `
MANIFEST = {"name": "Сломанный"}

def visible(f)
    return True
`)
	err := behaviors.Validate(node)
	if err == nil {
		t.Fatal("a script with a syntax error was accepted")
	}
	// Это сообщение админ видит в редакторе, поэтому оно обязано сказать, где именно.
	if !strings.Contains(err.Error(), "behavior.star") {
		t.Errorf("error %q does not name the file the mistake is in", err)
	}
}

// Очистка скрипта снова делает узел обычной услугой, а не такой, чьи проверки
// закрыты, потому что код поведения всё ещё указывает в никуда.
func TestClearingTheScriptMakesTheNodeOrdinary(t *testing.T) {
	ctx := context.Background()
	behaviors := newBehaviorsForTest()
	node := nodeWithScript(nodeScriptConstants, nodeScriptSource)
	if err := behaviors.SyncNode(node); err != nil {
		t.Fatalf("sync: %v", err)
	}

	node.BehaviorSource = ""
	node.BehaviorConstants = ""
	if err := behaviors.SyncNode(node); err != nil {
		t.Fatalf("sync after clearing: %v", err)
	}
	if err := behaviors.CanOrder(ctx, nil, node); err != nil {
		t.Errorf("an ordinary node is still being judged by a script: %v", err)
	}
	if !behaviors.Visible(ctx, nil, node, nil) {
		t.Error("an ordinary node is hidden by a script that no longer exists")
	}
}

// Две особые услуги не должны видеть правила друг друга.
func TestNodeScriptsAreIsolated(t *testing.T) {
	ctx := context.Background()
	behaviors := newBehaviorsForTest()

	permissive := nodeWithScript("", `
MANIFEST = {"name": "Открытая"}

def visible(f):
    return True
`)
	closed := nodeWithScript("", `
MANIFEST = {"name": "Закрытая"}

def visible(f):
    return False
`)
	for _, node := range []*repository.ServiceNode{permissive, closed} {
		if err := behaviors.SyncNode(node); err != nil {
			t.Fatalf("sync %s: %v", node.Code, err)
		}
	}

	customer := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: "ACTIVE"}
	if !behaviors.Visible(ctx, customer, permissive, nil) {
		t.Error("the permissive service is hidden")
	}
	if behaviors.Visible(ctx, customer, closed, nil) {
		t.Error("the closed service is shown: the two scripts share a registration")
	}
}
