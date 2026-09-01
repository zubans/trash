package behavior

import (
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"go.starlark.net/starlark"
)

// Hook names. A script defines the ones it needs and no more; an undefined hook
// means "no opinion", and the core keeps its own default.
const (
	HookVisible       = "visible"
	HookCanOrder      = "can_order"
	HookCanViewOrTake = "can_view_or_take"
	HookPrice         = "price"
	HookOnEvent       = "on_event"
)

var hookNames = []string{HookVisible, HookCanOrder, HookCanViewOrTake, HookPrice, HookOnEvent}

// Limits bound one hook call. They exist because a hook runs on the request
// path: a script with a runaway loop must fail its own call, not the process.
type Limits struct {
	MaxSteps uint64
	Timeout  time.Duration
}

// DefaultLimits are generous for the decisions these scripts make (a handful of
// comparisons) and small enough that a mistake is a failed hook.
var DefaultLimits = Limits{MaxSteps: 200_000, Timeout: 100 * time.Millisecond}

type script struct {
	code     string
	globals  starlark.StringDict
	manifest Manifest
}

// NodeCodePrefix marks a behaviour that belongs to one catalog node — a script
// written in the admin panel rather than shipped as a file. The prefix keeps the
// two namespaces apart: a node can never shadow a library behaviour, and a
// library behaviour can never be edited away by a node.
const NodeCodePrefix = "node:"

// NodeCode is the behaviour code under which a node's own script is registered.
func NodeCode(nodeID string) string { return NodeCodePrefix + nodeID }

// IsNodeCode reports whether a code belongs to a node's own script.
func IsNodeCode(code string) bool { return strings.HasPrefix(code, NodeCodePrefix) }

// Engine holds the compiled behaviour scripts. Compilation happens once at
// startup; the globals are frozen afterwards, which is what makes concurrent
// calls from request handlers safe.
type Engine struct {
	limits Limits

	mu      sync.RWMutex
	scripts map[string]*script
}

// New creates an empty engine.
func New(limits Limits) *Engine {
	if limits.MaxSteps == 0 {
		limits.MaxSteps = DefaultLimits.MaxSteps
	}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultLimits.Timeout
	}
	return &Engine{limits: limits, scripts: map[string]*script{}}
}

// ConfigFile is executed before the rest of a behaviour, and its globals are
// visible to every file after it. That is what lets a behaviour keep its
// constants — amounts, roles, event names, messages — in one file separate from
// the logic that reads them.
const ConfigFile = "config.star"

// SourceFile is one file of a behaviour, in the order it must be executed.
type SourceFile struct {
	Name string
	Src  []byte
}

// Load compiles every behaviour directory at the root of fsys. The directory
// name is the behaviour code that service_nodes.behavior_code names, and the
// *.star files inside it make up the behaviour: ConfigFile first, then the rest
// in name order, each seeing the globals of the ones before it.
//
// Loading the same code twice replaces it, which is how a directory on disk
// overrides the copy embedded in the binary.
//
// A behaviour that fails to compile is reported and skipped rather than fatal:
// one broken behaviour must not stop the service from starting. Nodes pointing
// at it fall back to the built-in rules and refuse orders (see the service
// layer), which is the safe direction.
func (e *Engine) Load(fsys fs.FS, label string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read behaviors from %s: %w", label, err)
	}
	var failed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".star") {
				// A script outside a behaviour directory belongs to no
				// behaviour and would silently never run.
				log.Printf("[behavior] %s/%s: ignored, a behaviour must live in its own directory", label, entry.Name())
			}
			continue
		}
		code := entry.Name()
		files, err := readBehaviorDir(fsys, code)
		if err != nil {
			failed = append(failed, code)
			log.Printf("[behavior] %s/%s: %v", label, code, err)
			continue
		}
		if len(files) == 0 {
			continue
		}
		if err := e.CompileFiles(code, files); err != nil {
			failed = append(failed, code)
			log.Printf("[behavior] %s/%s: %v", label, code, err)
			continue
		}
		log.Printf("[behavior] loaded %q from %s", code, label)
	}
	if len(failed) > 0 {
		return fmt.Errorf("behaviors failed to load: %s", strings.Join(failed, ", "))
	}
	return nil
}

// readBehaviorDir collects one behaviour's files in execution order.
func readBehaviorDir(fsys fs.FS, dir string) ([]SourceFile, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".star") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Slice(names, func(i, j int) bool {
		// The config file first whatever it sorts as; everything else by name,
		// so the order is the same on every machine.
		if (names[i] == ConfigFile) != (names[j] == ConfigFile) {
			return names[i] == ConfigFile
		}
		return names[i] < names[j]
	})

	files := make([]SourceFile, 0, len(names))
	for _, name := range names {
		src, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		files = append(files, SourceFile{Name: path.Join(dir, name), Src: src})
	}
	return files, nil
}

// Compile parses a single-file behaviour. It exists for tests and for the
// simplest possible behaviour; the loader above uses CompileFiles.
func (e *Engine) Compile(code, filename string, src []byte) error {
	return e.CompileFiles(code, []SourceFile{{Name: filename, Src: src}})
}

// CompileFiles parses one behaviour's files in order and registers it under
// code. Each file is executed with the builtins plus everything the previous
// files defined, so config.star's constants are ordinary globals to the logic
// that follows.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if len(files) == 0 {
		return fmt.Errorf("behavior %s has no scripts", code)
	}

	thread := &starlark.Thread{Name: "load:" + code}
	thread.SetMaxExecutionSteps(e.limits.MaxSteps)

	env := predeclared()
	globals := starlark.StringDict{}
	for _, file := range files {
		// Top-level code runs at compile time: it may define constants and
		// functions, and nothing else is reachable from it.
		defined, err := starlark.ExecFile(thread, file.Name, file.Src, env)
		if err != nil {
			return err
		}
		for name, value := range defined {
			globals[name] = value
			env[name] = value
		}
	}
	globals.Freeze()

	manifest, err := readManifest(code, globals)
	if err != nil {
		return err
	}

	// The sources travel with the compiled behaviour so the admin panel can show
	// a shipped script as the starting template for a new one.
	for _, file := range files {
		if path.Base(file.Name) == ConfigFile {
			manifest.ConstantsSource = string(file.Src)
		} else if manifest.Source == "" {
			manifest.Source = string(file.Src)
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.scripts[code] = &script{code: code, globals: globals, manifest: manifest}
	return nil
}

// Remove unregisters a behaviour. Used when a node's own script is deleted, so
// the node stops being special the moment the admin saves it.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.scripts, code)
}

// Library lists the behaviours that ship with the build — everything except the
// per-node scripts. This is what the service constructor offers as templates.
func (e *Engine) Library() []Manifest {
	all := e.Manifests()
	out := make([]Manifest, 0, len(all))
	for _, m := range all {
		if !IsNodeCode(m.Code) {
			out = append(out, m)
		}
	}
	return out
}

// Validate compiles a candidate script without registering it, so the admin
// panel can refuse a broken script at save time instead of taking the service
// off sale at run time. The error is the Starlark one, which names the file,
// the line and the problem.
func (e *Engine) Validate(files []SourceFile) error {
	probe := New(e.limits)
	return probe.CompileFiles("candidate", files)
}

func readManifest(code string, globals starlark.StringDict) (Manifest, error) {
	m := Manifest{Code: code, ReleaseClaimOnCancel: true, Defaults: map[string]interface{}{}}
	for _, hook := range hookNames {
		if _, ok := globals[hook].(starlark.Callable); ok {
			m.Hooks = append(m.Hooks, hook)
		}
	}

	raw, ok := globals["MANIFEST"]
	if !ok {
		return m, fmt.Errorf("script %s defines no MANIFEST", code)
	}
	dict, ok := raw.(*starlark.Dict)
	if !ok {
		return m, fmt.Errorf("script %s: MANIFEST must be a dict", code)
	}
	values, _ := starlarkToGo(dict).(map[string]interface{})
	for key, value := range values {
		switch key {
		case "name":
			m.Name, _ = value.(string)
		case "description":
			m.Description, _ = value.(string)
		case "once_per_user":
			m.OncePerUser, _ = value.(bool)
		case "release_claim_on_cancel":
			if b, ok := value.(bool); ok {
				m.ReleaseClaimOnCancel = b
			}
		case "check_fields":
			items, _ := value.([]interface{})
			for _, item := range items {
				if s, ok := item.(string); ok {
					m.CheckFields = append(m.CheckFields, s)
				}
			}
		case "hide_customer_contacts":
			m.HideCustomerContacts, _ = value.(bool)
		case "events":
			items, _ := value.([]interface{})
			for _, item := range items {
				if s, ok := item.(string); ok {
					m.Events = append(m.Events, s)
				}
			}
		case "defaults":
			if defaults, ok := value.(map[string]interface{}); ok {
				m.Defaults = defaults
			}
		}
	}
	if m.Name == "" {
		m.Name = code
	}
	sort.Strings(m.Events)
	return m, nil
}

// Has reports whether a behaviour with this code is loaded.
func (e *Engine) Has(code string) bool {
	if e == nil || code == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.scripts[code]
	return ok
}

// Manifest returns the static declaration of a behaviour.
func (e *Engine) Manifest(code string) (Manifest, bool) {
	if e == nil {
		return Manifest{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.scripts[code]
	if !ok {
		return Manifest{}, false
	}
	return s.manifest, true
}

// Manifests lists every loaded behaviour, for the admin panel's picker.
func (e *Engine) Manifests() []Manifest {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Manifest, 0, len(e.scripts))
	for _, s := range e.scripts {
		out = append(out, s.manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Visible answers whether a catalog node may be shown. A script with no
// `visible` hook shows the node, which is what a node with no behaviour does.
func (e *Engine) Visible(code string, f Facts) (bool, error) {
	result, err := e.call(code, HookVisible, f)
	if err != nil || result == nil {
		return true, err
	}
	return bool(result.Truth()), nil
}

// CanOrder answers whether the customer may place this order. A refusal comes
// back as *DeniedError carrying the message for the user.
func (e *Engine) CanOrder(code string, f Facts) error {
	return e.decide(code, HookCanOrder, f)
}

// CanViewOrTake answers whether an executor or moderator may see and accept an
// order for this service.
func (e *Engine) CanViewOrTake(code string, f Facts) error {
	return e.decide(code, HookCanViewOrTake, f)
}

// decide interprets the shared convention of the two gate hooks: None or True
// allows, False refuses without a reason, a string refuses with that message.
func (e *Engine) decide(code, hook string, f Facts) error {
	result, err := e.call(code, hook, f)
	if err != nil || result == nil {
		return err
	}
	switch v := result.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		if bool(v) {
			return nil
		}
		return Denied(code, "")
	case starlark.String:
		if string(v) == "" {
			return nil
		}
		return Denied(code, string(v))
	default:
		return fmt.Errorf("behavior %s: %s returned %s, want None, bool or string", code, hook, result.Type())
	}
}

// Price returns the price in rubles a behaviour dictates, and whether it
// dictated one at all. A script that defines no `price` hook, or returns None,
// leaves the catalog's own pricing alone.
func (e *Engine) Price(code string, f Facts) (float64, bool, error) {
	result, err := e.call(code, HookPrice, f)
	if err != nil || result == nil {
		return 0, false, err
	}
	if _, isNone := result.(starlark.NoneType); isNone {
		return 0, false, nil
	}
	price, ok := starlark.AsFloat(result)
	if !ok {
		return 0, false, fmt.Errorf("behavior %s: price returned %s, want a number", code, result.Type())
	}
	if price < 0 {
		return 0, false, fmt.Errorf("behavior %s: price returned a negative amount", code)
	}
	return price, true, nil
}

// OnEvent runs the event hook and returns the effects the script asks for. The
// effects are requests: the caller applies them, with its own guards.
func (e *Engine) OnEvent(code string, f Facts) ([]Effect, error) {
	result, err := e.call(code, HookOnEvent, f)
	if err != nil || result == nil {
		return nil, err
	}
	switch v := result.(type) {
	case starlark.NoneType:
		return nil, nil
	case *starlark.List:
		effects := make([]Effect, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item, ok := v.Index(i).(*effectValue)
			if !ok {
				return nil, fmt.Errorf("behavior %s: on_event returned %s, want effects", code, v.Index(i).Type())
			}
			effects = append(effects, item.effect)
		}
		return effects, nil
	case *effectValue:
		return []Effect{v.effect}, nil
	default:
		return nil, fmt.Errorf("behavior %s: on_event returned %s, want a list of effects", code, result.Type())
	}
}

// call runs one hook. It returns (nil, nil) when the behaviour does not define
// it, so every caller can fall back to the core's own rule.
func (e *Engine) call(code, hook string, f Facts) (starlark.Value, error) {
	if e == nil || code == "" {
		return nil, nil
	}
	e.mu.RLock()
	s, ok := e.scripts[code]
	e.mu.RUnlock()
	if !ok {
		return nil, &ErrUnknownBehavior{Code: code}
	}
	fn, ok := s.globals[hook].(starlark.Callable)
	if !ok {
		return nil, nil
	}

	f.Config = mergeConfig(s.manifest.Defaults, f.Config)
	if f.Now.IsZero() {
		f.Now = time.Now()
	}

	thread := &starlark.Thread{Name: code + ":" + hook}
	thread.SetMaxExecutionSteps(e.limits.MaxSteps)
	// The step limit stops an endless loop; the timer stops everything else,
	// including a script that is merely slow on a loaded machine.
	timer := time.AfterFunc(e.limits.Timeout, func() { thread.Cancel("timeout") })
	defer timer.Stop()

	result, err := starlark.Call(thread, fn, starlark.Tuple{factsValue(f)}, nil)
	if err != nil {
		return nil, fmt.Errorf("behavior %s: %s: %w", code, hook, err)
	}
	return result, nil
}

// mergeConfig lays the node's configuration over the script's defaults, so a
// node only has to state what it changes.
func mergeConfig(defaults, node map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(defaults)+len(node))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range node {
		merged[k] = v
	}
	return merged
}
