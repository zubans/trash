package behavior

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"go.starlark.net/starlark"

	"healthlogin/backend/script"
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

// Рантайм у поведений общий с остальными скриптовыми областями (см.
// backend/script): здесь остаётся только то, что знает про услуги — факты
// заказа, эффекты и хуки.
type (
	// Limits ограничивают один вызов хука.
	Limits = script.Limits
	// SourceFile — один файл поведения.
	SourceFile = script.SourceFile
)

// DefaultLimits щедры для решений, которые принимают эти скрипты, и достаточно
// малы, чтобы ошибка оборачивалась провалом хука.
var DefaultLimits = script.DefaultLimits

// ConfigFile выполняется раньше остального поведения, и его глобалы видны
// каждому последующему файлу.
const ConfigFile = script.ConfigFile

// NodeCodePrefix помечает поведение, принадлежащее одному узлу каталога, —
// скрипт, написанный в админ-панели, а не поставленный файлом. Префикс разводит
// два пространства имён: узел никогда не перекроет библиотечное поведение, а
// библиотечное поведение никогда не будет затёрто правкой узла.
const NodeCodePrefix = "node:"

// NodeCode — код поведения, под которым регистрируется собственный скрипт узла.
func NodeCode(nodeID string) string { return NodeCodePrefix + nodeID }

// IsNodeCode сообщает, принадлежит ли код собственному скрипту узла.
func IsNodeCode(code string) bool { return strings.HasPrefix(code, NodeCodePrefix) }

// Engine — рантайм скриптов, настроенный под услуги: его встроенные функции
// строят эффекты заказа, а манифесты разбираются в Manifest этого пакета.
type Engine struct {
	runtime *script.Engine

	mu        sync.RWMutex
	manifests map[string]Manifest
}

// New создаёт пустой движок.
func New(limits Limits) *Engine {
	return &Engine{
		runtime:   script.New(limits, script.Options{Builtins: predeclared, Hooks: hookNames}),
		manifests: map[string]Manifest{},
	}
}

// Load компилирует каждый каталог поведения в корне fsys. Имя каталога — это код
// поведения, на который ссылается service_nodes.behavior_code.
func (e *Engine) Load(fsys fs.FS, label string) error {
	return script.Load(fsys, label, e)
}

// Compile разбирает однофайловое поведение. Существует для тестов и для самого
// простого возможного поведения; загрузчик выше использует CompileFiles.
func (e *Engine) Compile(code, filename string, src []byte) error {
	return e.CompileFiles(code, []SourceFile{{Name: filename, Src: src}})
}

// CompileFiles разбирает файлы одного поведения по порядку и регистрирует его
// под кодом code, разбирая заодно поля манифеста, которые есть только у услуг.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if err := e.runtime.CompileFiles(code, files); err != nil {
		return err
	}
	raw, ok := e.runtime.Manifest(code)
	if !ok {
		return fmt.Errorf("behavior %s compiled but has no manifest", code)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.manifests[code] = manifestFrom(raw)
	return nil
}

// Remove снимает регистрацию поведения. Используется при удалении собственного
// скрипта узла, чтобы узел переставал быть особым сразу после сохранения.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.runtime.Remove(code)
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.manifests, code)
}

// Validate компилирует кандидата, не регистрируя его, чтобы админ-панель могла
// отклонить сломанный скрипт при сохранении, а не снимать услугу с продажи во
// время работы.
func (e *Engine) Validate(files []SourceFile) error {
	return e.runtime.Validate(files)
}

// Has сообщает, загружено ли поведение с таким кодом.
func (e *Engine) Has(code string) bool {
	if e == nil {
		return false
	}
	return e.runtime.Has(code)
}

// Manifest возвращает статическое объявление поведения.
func (e *Engine) Manifest(code string) (Manifest, bool) {
	if e == nil {
		return Manifest{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	m, ok := e.manifests[code]
	return m, ok
}

// Manifests перечисляет все загруженные поведения — для выбора в админ-панели.
func (e *Engine) Manifests() []Manifest {
	if e == nil {
		return nil
	}
	out := make([]Manifest, 0)
	for _, raw := range e.runtime.Manifests() {
		if m, ok := e.Manifest(raw.Code); ok {
			out = append(out, m)
		}
	}
	return out
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

// manifestFrom достраивает общий манифест рантайма полями, которые есть только
// у услуги: однократностью, полями проверки и сокрытием контактов заказчика.
func manifestFrom(raw script.Manifest) Manifest {
	return Manifest{
		Code:                 raw.Code,
		Name:                 raw.Name,
		Description:          raw.Description,
		Events:               raw.Events,
		Defaults:             raw.Defaults,
		Hooks:                raw.Hooks,
		ConstantsSource:      raw.ConstantsSource,
		Source:               raw.Source,
		OncePerUser:          raw.Bool("once_per_user", false),
		ReleaseClaimOnCancel: raw.Bool("release_claim_on_cancel", true),
		CheckFields:          raw.Strings("check_fields"),
		HideCustomerContacts: raw.Bool("hide_customer_contacts", false),
	}
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

// call готовит факты и выполняет один хук. Ошибка о незагруженном поведении
// переводится в собственный тип пакета: вызывающие уже различают его.
func (e *Engine) call(code, hook string, f Facts) (starlark.Value, error) {
	if e == nil || code == "" {
		return nil, nil
	}
	if !e.runtime.Has(code) {
		return nil, &ErrUnknownBehavior{Code: code}
	}
	f.Config = e.runtime.MergeConfig(code, f.Config)
	if f.Now.IsZero() {
		f.Now = time.Now()
	}
	return e.runtime.CallHook(code, hook, factsValue(f))
}
