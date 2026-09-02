package behaviors_test

import (
	"testing"

	"healthlogin/backend/behavior"
	"healthlogin/backend/behaviors"
)

// Скрипты несёт сам бинарник, и доказывает это только этот тест: всё остальное
// читает их с диска. Иначе неверный шаблон embed выпустил бы образ, все услуги
// которого отказывают на первом же запросе.
func TestEmbeddedBehaviorsCompile(t *testing.T) {
	engine := behavior.New(behavior.DefaultLimits)
	if err := engine.Load(behaviors.FS, "embedded"); err != nil {
		t.Fatalf("embedded behaviours failed to load: %v", err)
	}
	manifests := engine.Manifests()
	if len(manifests) == 0 {
		t.Fatal("no behaviours are embedded in the binary")
	}
	if !engine.Has("verification") {
		t.Errorf("the verification behaviour is not embedded: got %v", manifests)
	}
	// Файл констант — отдельный скрипт; если бы он не выполнился, манифест не нёс
	// бы умолчаний, и поведение не платило бы ничего.
	m, _ := engine.Manifest("verification")
	if len(m.Defaults) == 0 {
		t.Error("verification loaded without its config.star constants")
	}
}
