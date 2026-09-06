// Package script — рантайм скриптов, общий для всех предметных областей,
// которые вынесли свои правила из Go в данные.
//
// Здесь всё, что не зависит от того, о чём скрипт: загрузка каталога с
// поведениями, порядок выполнения файлов, компиляция один раз при старте,
// заморозка глобалов, лимит шагов и таймаут на вызов, чтение MANIFEST и
// проверка кандидата перед сохранением.
//
// Предметная область добавляет к этому три вещи и больше ничего: набор
// встроенных функций, в котором компилируется скрипт (конструкторы её
// эффектов), список имён хуков и конвертер своих фактов в значение Starlark.
// Разделение появилось, когда к поведениям услуг добавились ачивки: у них
// разные факты и разные эффекты, но ровно один рантайм — и лимиты, загрузчик и
// «пробный прогон» админки должны остаться в одном экземпляре, иначе они
// разойдутся.
package script

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

// Limits ограничивают один вызов хука. Они нужны потому, что хук выполняется на
// пути запроса: скрипт с зациклившимся циклом должен уронить свой вызов, а не процесс.
type Limits struct {
	MaxSteps uint64
	Timeout  time.Duration
}

// DefaultLimits щедры для решений, которые принимают эти скрипты (несколько
// сравнений), и достаточно малы, чтобы ошибка оборачивалась провалом хука.
var DefaultLimits = Limits{MaxSteps: 200_000, Timeout: 100 * time.Millisecond}

// ConfigFile выполняется раньше остальных файлов, и его глобалы видны каждому
// последующему. Именно это позволяет держать константы — суммы, роли, имена
// событий, сообщения — в одном файле, отдельно от логики, которая их читает.
const ConfigFile = "config.star"

// SourceFile — один файл скрипта, в том порядке, в котором он должен выполняться.
type SourceFile struct {
	Name string
	Src  []byte
}

// Manifest — статическая половина скрипта, общая для всех областей: то, что
// ядру нужно знать, ничего не запуская. Поля, специфичные для области, лежат в
// Raw, и разбирает их сама область.
type Manifest struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// Description показывается в админ-панели рядом с выбором скрипта.
	Description string `json:"description"`
	// Events — события, на которые реагирует скрипт. Событие вне списка не доставляется.
	Events []string `json:"events"`
	// Defaults — значения конфигурации, которые применяются, если их не задали снаружи.
	Defaults map[string]interface{} `json:"defaults"`
	// Hooks перечисляет, какие функции скрипт на самом деле определяет, — для
	// админ-панели и экрана пробного прогона.
	Hooks []string `json:"hooks"`
	// ConstantsSource и Source — собственный текст скрипта. Конструктор в
	// админ-панели показывает их: поставляемый скрипт админ читает, чтобы
	// разобраться, и копирует как стартовый шаблон для нового.
	ConstantsSource string `json:"constants_source,omitempty"`
	Source          string `json:"source,omitempty"`
	// Raw — весь словарь MANIFEST как он написан в скрипте. Предметная область
	// достаёт отсюда свои поля: once_per_user у услуги, weight у ачивки.
	Raw map[string]interface{} `json:"-"`
}

// Handles сообщает, запрашивал ли скрипт это событие.
func (m Manifest) Handles(event string) bool {
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// String достаёт строковое поле манифеста, специфичное для предметной области.
func (m Manifest) String(key string) string {
	s, _ := m.Raw[key].(string)
	return s
}

// Bool достаёт булево поле манифеста; отсутствующее считается fallback.
func (m Manifest) Bool(key string, fallback bool) bool {
	if b, ok := m.Raw[key].(bool); ok {
		return b
	}
	return fallback
}

// Int достаёт числовое поле манифеста. Числа приходят из Starlark как float64,
// поэтому разбираются оба варианта.
func (m Manifest) Int(key string, fallback int) int {
	switch v := m.Raw[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

// Strings достаёт список строк — например, поля, которые объявила услуга.
func (m Manifest) Strings(key string) []string {
	items, _ := m.Raw[key].([]interface{})
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Options — то, чем предметная область достраивает рантайм под себя.
type Options struct {
	// Builtins возвращает окружение, в котором компилируется скрипт:
	// конструкторы эффектов этой области и больше ничего — ни print, ни load,
	// ни ввода-вывода. Вызывается на каждую компиляцию, чтобы значения не
	// разделялись между скриптами.
	Builtins func() starlark.StringDict
	// Hooks — имена функций, которые область считает хуками. Скрипт определяет
	// те, что ему нужны; неопределённый хук означает «нет мнения».
	Hooks []string
}

type compiled struct {
	globals  starlark.StringDict
	manifest Manifest
}

// Engine хранит скомпилированные скрипты. Компиляция происходит один раз при
// старте; после этого глобалы замораживаются — именно это делает безопасными
// параллельные вызовы из обработчиков запросов.
type Engine struct {
	limits Limits
	opts   Options

	mu      sync.RWMutex
	scripts map[string]*compiled
}

// New создаёт пустой движок.
func New(limits Limits, opts Options) *Engine {
	if limits.MaxSteps == 0 {
		limits.MaxSteps = DefaultLimits.MaxSteps
	}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultLimits.Timeout
	}
	if opts.Builtins == nil {
		opts.Builtins = func() starlark.StringDict { return starlark.StringDict{} }
	}
	return &Engine{limits: limits, opts: opts, scripts: map[string]*compiled{}}
}

// Limits отдаёт ограничения движка — обёртке, которая создаёт по ним пробный экземпляр.
func (e *Engine) Limits() Limits { return e.limits }

// Options отдаёт настройки области — для той же цели.
func (e *Engine) Options() Options { return e.opts }

// Compiler — то, что умеет принять один каталог скриптов. Load принимает его
// вместо самого движка, чтобы предметная обёртка могла разобрать манифест
// по-своему и всё равно пользоваться общим загрузчиком.
type Compiler interface {
	CompileFiles(code string, files []SourceFile) error
}

// Load компилирует каждый каталог в корне fsys. Имя каталога — код скрипта, а
// лежащие внутри файлы *.star и составляют его: сперва ConfigFile, затем
// остальные в порядке имён, каждый видит глобалы предыдущих.
//
// Повторная загрузка того же кода заменяет предыдущую — так каталог на диске
// перекрывает копию, встроенную в бинарник.
//
// Скрипт, который не компилируется, логируется и пропускается, а не роняет
// процесс: один сломанный скрипт не должен мешать сервису стартовать. Тот, кто
// на него ссылается, откатывается к встроенным правилам — это безопасное
// направление.
func Load(fsys fs.FS, label string, c Compiler) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read scripts from %s: %w", label, err)
	}
	var failed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if strings.HasSuffix(entry.Name(), ".star") {
				// Скрипт вне каталога не принадлежит ни одному коду и молча
				// никогда бы не выполнился.
				log.Printf("[script] %s/%s: ignored, a script must live in its own directory", label, entry.Name())
			}
			continue
		}
		code := entry.Name()
		files, err := ReadDir(fsys, code)
		if err != nil {
			failed = append(failed, code)
			log.Printf("[script] %s/%s: %v", label, code, err)
			continue
		}
		if len(files) == 0 {
			continue
		}
		if err := c.CompileFiles(code, files); err != nil {
			failed = append(failed, code)
			log.Printf("[script] %s/%s: %v", label, code, err)
			continue
		}
		log.Printf("[script] loaded %q from %s", code, label)
	}
	if len(failed) > 0 {
		return fmt.Errorf("scripts failed to load: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ReadDir собирает файлы одного скрипта в порядке выполнения.
func ReadDir(fsys fs.FS, dir string) ([]SourceFile, error) {
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

// Compile разбирает однофайловый скрипт. Существует для тестов и для самого
// простого возможного скрипта; загрузчик выше использует CompileFiles.
func (e *Engine) Compile(code, filename string, src []byte) error {
	return e.CompileFiles(code, []SourceFile{{Name: filename, Src: src}})
}

// CompileFiles разбирает файлы одного скрипта по порядку и регистрирует его под
// кодом code. Каждый файл выполняется со встроенными функциями плюс всем, что
// определили предыдущие файлы, поэтому константы config.star для идущей следом
// логики — обычные глобалы.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if len(files) == 0 {
		return fmt.Errorf("script %s has no files", code)
	}

	thread := &starlark.Thread{Name: "load:" + code}
	thread.SetMaxExecutionSteps(e.limits.MaxSteps)

	env := e.opts.Builtins()
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

	manifest, err := e.readManifest(code, globals)
	if err != nil {
		return err
	}

	// Исходники путешествуют вместе со скомпилированным скриптом, чтобы
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
	e.scripts[code] = &compiled{globals: globals, manifest: manifest}
	return nil
}

// Remove снимает регистрацию скрипта.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.scripts, code)
}

// Validate компилирует кандидата, не регистрируя его, чтобы админ-панель могла
// отклонить сломанный скрипт при сохранении, а не снимать что-то с продажи во
// время работы. Ошибка — это ошибка Starlark, называющая файл, строку и суть
// проблемы.
func (e *Engine) Validate(files []SourceFile) error {
	probe := New(e.limits, e.opts)
	return probe.CompileFiles("candidate", files)
}

func (e *Engine) readManifest(code string, globals starlark.StringDict) (Manifest, error) {
	m := Manifest{Code: code, Defaults: map[string]interface{}{}, Raw: map[string]interface{}{}}
	for _, hook := range e.opts.Hooks {
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
	values, _ := ToGo(dict).(map[string]interface{})
	m.Raw = values
	for key, value := range values {
		switch key {
		case "name", "title":
			// Услуга называет себя name, ачивка — title: для рантайма это одно поле.
			if s, ok := value.(string); ok && m.Name == "" {
				m.Name = s
			}
		case "description":
			m.Description, _ = value.(string)
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

// Has сообщает, загружен ли скрипт с таким кодом.
func (e *Engine) Has(code string) bool {
	if e == nil || code == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.scripts[code]
	return ok
}

// Manifest возвращает статическое объявление скрипта.
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

// Manifests перечисляет все загруженные скрипты — для выбора в админ-панели.
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

// MergeConfig накладывает внешнюю конфигурацию поверх умолчаний скрипта, чтобы
// снаружи достаточно было указать только то, что меняется.
func (e *Engine) MergeConfig(code string, outer map[string]interface{}) map[string]interface{} {
	var defaults map[string]interface{}
	if m, ok := e.Manifest(code); ok {
		defaults = m.Defaults
	}
	merged := make(map[string]interface{}, len(defaults)+len(outer))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range outer {
		merged[k] = v
	}
	return merged
}

// ErrUnknownScript сообщает, что кто-то ссылается на незагруженный скрипт —
// такой, который не скомпилировался, или код, оставшийся после отката.
type ErrUnknownScript struct{ Code string }

func (e *ErrUnknownScript) Error() string {
	return fmt.Sprintf("unknown script %q", e.Code)
}

// CallHook выполняет один хук с уже построенным значением фактов. Возвращает
// (nil, nil), когда скрипт этого хука не определяет, чтобы любой вызывающий мог
// откатиться к собственному правилу ядра.
func (e *Engine) CallHook(code, hook string, facts starlark.Value) (starlark.Value, error) {
	if e == nil || code == "" {
		return nil, nil
	}
	e.mu.RLock()
	s, ok := e.scripts[code]
	e.mu.RUnlock()
	if !ok {
		return nil, &ErrUnknownScript{Code: code}
	}
	fn, ok := s.globals[hook].(starlark.Callable)
	if !ok {
		return nil, nil
	}

	thread := &starlark.Thread{Name: code + ":" + hook}
	thread.SetMaxExecutionSteps(e.limits.MaxSteps)
	// Лимит шагов останавливает бесконечный цикл; таймер останавливает всё
	// остальное, включая скрипт, который просто медлит на нагруженной машине.
	timer := time.AfterFunc(e.limits.Timeout, func() { thread.Cancel("timeout") })
	defer timer.Stop()

	result, err := starlark.Call(thread, fn, starlark.Tuple{facts}, nil)
	if err != nil {
		return nil, fmt.Errorf("script %s: %s: %w", code, hook, err)
	}
	return result, nil
}
