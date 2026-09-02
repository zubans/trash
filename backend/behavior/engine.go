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

// Имена хуков. Скрипт определяет те, что ему нужны, и не больше; неопределённый
// хук означает «нет мнения», и ядро остаётся при своём умолчании.
const (
	HookVisible       = "visible"
	HookCanOrder      = "can_order"
	HookCanViewOrTake = "can_view_or_take"
	HookPrice         = "price"
	HookOnEvent       = "on_event"
)

var hookNames = []string{HookVisible, HookCanOrder, HookCanViewOrTake, HookPrice, HookOnEvent}

// Limits ограничивают один вызов хука. Они нужны потому, что хук выполняется на
// пути запроса: скрипт с зациклившимся циклом должен уронить свой вызов, а не процесс.
type Limits struct {
	MaxSteps uint64
	Timeout  time.Duration
}

// DefaultLimits щедры для решений, которые принимают эти скрипты (несколько
// сравнений), и достаточно малы, чтобы ошибка оборачивалась провалом хука.
var DefaultLimits = Limits{MaxSteps: 200_000, Timeout: 100 * time.Millisecond}

type script struct {
	code     string
	globals  starlark.StringDict
	manifest Manifest
}

// NodeCodePrefix помечает поведение, принадлежащее одному узлу каталога, —
// скрипт, написанный в админ-панели, а не поставленный файлом. Префикс разводит
// два пространства имён: узел никогда не перекроет библиотечное поведение, а
// библиотечное поведение никогда не будет затёрто правкой узла.
const NodeCodePrefix = "node:"

// NodeCode — код поведения, под которым регистрируется собственный скрипт узла.
func NodeCode(nodeID string) string { return NodeCodePrefix + nodeID }

// IsNodeCode сообщает, принадлежит ли код собственному скрипту узла.
func IsNodeCode(code string) bool { return strings.HasPrefix(code, NodeCodePrefix) }

// Engine хранит скомпилированные скрипты поведений. Компиляция происходит один
// раз при старте; после этого глобалы замораживаются — именно это делает
// безопасными параллельные вызовы из обработчиков запросов.
type Engine struct {
	limits Limits

	mu      sync.RWMutex
	scripts map[string]*script
}

// New создаёт пустой движок.
func New(limits Limits) *Engine {
	if limits.MaxSteps == 0 {
		limits.MaxSteps = DefaultLimits.MaxSteps
	}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultLimits.Timeout
	}
	return &Engine{limits: limits, scripts: map[string]*script{}}
}

// ConfigFile выполняется раньше остального поведения, и его глобалы видны
// каждому последующему файлу. Именно это позволяет поведению держать свои
// константы — суммы, роли, имена событий, сообщения — в одном файле, отдельно
// от логики, которая их читает.
const ConfigFile = "config.star"

// SourceFile — один файл поведения, в том порядке, в котором он должен выполняться.
type SourceFile struct {
	Name string
	Src  []byte
}

// Load компилирует каждый каталог поведения в корне fsys. Имя каталога — это код
// поведения, на который ссылается service_nodes.behavior_code, а лежащие внутри
// файлы *.star и составляют поведение: сперва ConfigFile, затем остальные в
// порядке имён, каждый видит глобалы предыдущих.
//
// Повторная загрузка того же кода заменяет предыдущую — так каталог на диске
// перекрывает копию, встроенную в бинарник.
//
// Поведение, которое не компилируется, логируется и пропускается, а не роняет
// процесс: одно сломанное поведение не должно мешать сервису стартовать. Узлы,
// ссылающиеся на него, откатываются к встроенным правилам и отклоняют заказы
// (см. слой сервисов) — это безопасное направление.
func (e *Engine) Load(fsys fs.FS, label string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read behaviors from %s: %w", label, err)
	}
	var failed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".star") {
				// Скрипт вне каталога поведения не принадлежит ни одному
				// поведению и молча никогда бы не выполнился.
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

// readBehaviorDir собирает файлы одного поведения в порядке выполнения.
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
		// Файл конфигурации первым, как бы он ни сортировался; всё остальное по имени,
		// чтобы порядок был одинаковым на любой машине.
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

// Compile разбирает однофайловое поведение. Существует для тестов и для самого
// простого возможного поведения; загрузчик выше использует CompileFiles.
func (e *Engine) Compile(code, filename string, src []byte) error {
	return e.CompileFiles(code, []SourceFile{{Name: filename, Src: src}})
}

// CompileFiles разбирает файлы одного поведения по порядку и регистрирует его
// под кодом code. Каждый файл выполняется со встроенными функциями плюс всем,
// что определили предыдущие файлы, поэтому константы config.star для идущей
// следом логики — обычные глобалы.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if len(files) == 0 {
		return fmt.Errorf("behavior %s has no scripts", code)
	}

	thread := &starlark.Thread{Name: "load:" + code}
	thread.SetMaxExecutionSteps(e.limits.MaxSteps)

	env := predeclared()
	globals := starlark.StringDict{}
	for _, file := range files {
		// Код верхнего уровня выполняется во время компиляции: он может определять
		// константы и функции, и больше ничего ему недоступно.
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

	// Исходники путешествуют вместе со скомпилированным поведением, чтобы
	// админ-панель показывала поставляемый скрипт как стартовый шаблон для нового.
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

// Remove снимает регистрацию поведения. Используется при удалении собственного
// скрипта узла, чтобы узел переставал быть особым сразу после сохранения.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.scripts, code)
}

// Library перечисляет поведения, поставляемые со сборкой, — всё, кроме скриптов
// отдельных узлов. Именно это конструктор услуг предлагает как шаблоны.
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

// Validate компилирует кандидата, не регистрируя его, чтобы админ-панель могла
// отклонить сломанный скрипт при сохранении, а не снимать услугу с продажи во
// время работы. Ошибка — это ошибка Starlark, называющая файл, строку и суть
// проблемы.
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

// Has сообщает, загружено ли поведение с таким кодом.
func (e *Engine) Has(code string) bool {
	if e == nil || code == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.scripts[code]
	return ok
}

// Manifest возвращает статическое объявление поведения.
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

// Manifests перечисляет все загруженные поведения — для выбора в админ-панели.
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

// Visible отвечает, можно ли показывать узел каталога. Скрипт без хука
// `visible` показывает узел — так же, как узел вовсе без поведения.
func (e *Engine) Visible(code string, f Facts) (bool, error) {
	result, err := e.call(code, HookVisible, f)
	if err != nil || result == nil {
		return true, err
	}
	return bool(result.Truth()), nil
}

// CanOrder отвечает, может ли заказчик разместить этот заказ. Отказ возвращается
// как *DeniedError с сообщением для пользователя.
func (e *Engine) CanOrder(code string, f Facts) error {
	return e.decide(code, HookCanOrder, f)
}

// CanViewOrTake отвечает, может ли исполнитель или модератор видеть и принимать
// заказ по этой услуге.
func (e *Engine) CanViewOrTake(code string, f Facts) error {
	return e.decide(code, HookCanViewOrTake, f)
}

// decide трактует общее соглашение двух хуков-«ворот»: None или True разрешают,
// False отказывает без причины, строка отказывает с этим сообщением.
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

// Price возвращает цену в рублях, которую диктует поведение, и признак того,
// диктовало ли оно её вообще. Скрипт без хука `price` или вернувший None
// оставляет ценообразование каталога нетронутым.
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

// OnEvent выполняет хук события и возвращает эффекты, которые просит скрипт.
// Эффекты — это просьбы: применяет их вызывающий, со своими проверками.
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

// call выполняет один хук. Возвращает (nil, nil), когда поведение его не
// определяет, чтобы любой вызывающий мог откатиться к собственному правилу ядра.
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
	// Лимит шагов останавливает бесконечный цикл; таймер останавливает всё
	// остальное, включая скрипт, который просто медлит на нагруженной машине.
	timer := time.AfterFunc(e.limits.Timeout, func() { thread.Cancel("timeout") })
	defer timer.Stop()

	result, err := starlark.Call(thread, fn, starlark.Tuple{factsValue(f)}, nil)
	if err != nil {
		return nil, fmt.Errorf("behavior %s: %s: %w", code, hook, err)
	}
	return result, nil
}

// mergeConfig накладывает конфигурацию узла поверх умолчаний скрипта, чтобы узлу
// достаточно было указать только то, что он меняет.
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
