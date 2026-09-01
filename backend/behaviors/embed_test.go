package behaviors_test

import (
	"testing"

	"healthlogin/backend/behavior"
	"healthlogin/backend/behaviors"
)

// The binary carries the scripts, and only this test proves it: everything else
// reads them from disk. A wrong embed pattern would otherwise ship an image
// whose services all fail closed on the first request.
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
	// The constants file is a separate script; if it had not been executed the
	// manifest would carry no defaults, and the behaviour would pay nothing.
	m, _ := engine.Manifest("verification")
	if len(m.Defaults) == 0 {
		t.Error("verification loaded without its config.star constants")
	}
}
