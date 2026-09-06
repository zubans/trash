package achievement

import (
	"fmt"
	"io/fs"
	"sync"
	"time"

	"go.starlark.net/starlark"

	"healthlogin/backend/script"
)

// Имена хуков. Скрипт определяет те, что ему нужны; неопределённый хук означает
// «нет мнения», и ядро остаётся при своём умолчании.
const (
	// HookCheck — единственный обязательный: он и решает, заслужена ли ачивка.
	HookCheck = "check"
	// HookProgress рисует полосу «ещё не получено» и на выдачу не влияет.
	HookProgress = "progress"
	// HookVisible прячет ачивку из списка, пока она человеку не адресована.
	HookVisible = "visible"
)

var hookNames = []string{HookCheck, HookProgress, HookVisible}

type (
	// Limits ограничивают один вызов хука.
	Limits = script.Limits
	// SourceFile — один файл ачивки.
	SourceFile = script.SourceFile
)

// ConfigFile выполняется раньше остальной ачивки, и его глобалы видны её
// логике: константы и правило, которое их читает, правятся порознь.
const ConfigFile = script.ConfigFile

// DefaultLimits наследуются у общего рантайма: решения здесь той же природы —
// несколько сравнений над готовыми фактами.
var DefaultLimits = script.DefaultLimits

// Engine — рантайм скриптов, настроенный под ачивки.
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

// Load компилирует каждый каталог ачивки в корне fsys. Имя каталога — код, на
// который ссылается строка таблицы achievements.
func (e *Engine) Load(fsys fs.FS, label string) error {
	return script.Load(fsys, label, e)
}

// Compile разбирает однофайловую ачивку — для тестов.
func (e *Engine) Compile(code, filename string, src []byte) error {
	return e.CompileFiles(code, []SourceFile{{Name: filename, Src: src}})
}

// CompileFiles разбирает файлы одной ачивки по порядку и регистрирует её.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if err := e.runtime.CompileFiles(code, files); err != nil {
		return err
	}
	raw, ok := e.runtime.Manifest(code)
	if !ok {
		return fmt.Errorf("achievement %s compiled but has no manifest", code)
	}
	m := manifestFrom(raw)
	if m.Audience != AudienceExecutor && m.Audience != AudienceCustomer {
		// Аудитория решает, чьим именем подставляется User, поэтому её опечатка
		// означала бы ачивку, которая молча никогда не срабатывает.
		return fmt.Errorf("achievement %s: audience %q must be %s or %s", code, m.Audience, AudienceExecutor, AudienceCustomer)
	}
	if len(m.Events) == 0 {
		return fmt.Errorf("achievement %s declares no events and would never be evaluated", code)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.manifests[code] = m
	return nil
}

// Remove снимает регистрацию ачивки.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.runtime.Remove(code)
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.manifests, code)
}

// Validate компилирует кандидата, не регистрируя его: админ-панель отклоняет
// сломанный скрипт при сохранении, а не выдаёт по нему ачивки.
//
// Кандидат прогоняется через собственную компиляцию этого пакета, а не через
// общий рантайм. Разница существенна: только здесь проверяются аудитория и
// объявленные события, а ачивка без них — это не сломанный скрипт, а строка,
// которая молча никогда не сработает. Именно такую ошибку и надо поймать до
// сохранения, а не через неделю по отсутствию выдач.
func (e *Engine) Validate(files []SourceFile) error {
	probe := New(e.runtime.Limits())
	return probe.CompileFiles("candidate", files)
}

// Has сообщает, загружена ли ачивка с таким кодом.
func (e *Engine) Has(code string) bool {
	if e == nil {
		return false
	}
	return e.runtime.Has(code)
}

// Manifest возвращает статическое объявление ачивки.
func (e *Engine) Manifest(code string) (Manifest, bool) {
	if e == nil {
		return Manifest{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	m, ok := e.manifests[code]
	return m, ok
}

// Manifests перечисляет загруженные ачивки — для админ-панели.
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

// manifestFrom достраивает общий манифест рантайма полями, которые есть только
// у ачивки.
func manifestFrom(raw script.Manifest) Manifest {
	return Manifest{
		Code:            raw.Code,
		Title:           raw.Name,
		Description:     raw.Description,
		Icon:            raw.String("icon"),
		Audience:        raw.String("audience"),
		Events:          raw.Events,
		OncePerUser:     raw.Bool("once_per_user", true),
		Weight:          raw.Int("weight", 0),
		LifetimeDays:    raw.Int("lifetime_days", 0),
		Defaults:        raw.Defaults,
		Hooks:           raw.Hooks,
		ConstantsSource: raw.ConstantsSource,
		Source:          raw.Source,
	}
}

// Check спрашивает скрипт, заслужена ли ачивка прямо сейчас. nil означает «нет»
// — обычный и самый частый исход, а не ошибка.
func (e *Engine) Check(code string, f Facts) (*Grant, error) {
	result, err := e.call(code, HookCheck, f)
	if err != nil || result == nil {
		return nil, err
	}
	switch v := result.(type) {
	case starlark.NoneType:
		return nil, nil
	case *grantValue:
		grant := v.grant
		return &grant, nil
	default:
		return nil, fmt.Errorf("achievement %s: check returned %s, want grant() or None", code, result.Type())
	}
}

// Progress возвращает долю выполнения 0..1 и признак того, назвал ли её скрипт.
// Значение только показывается: на выдачу оно не влияет никак.
func (e *Engine) Progress(code string, f Facts) (float64, bool, error) {
	result, err := e.call(code, HookProgress, f)
	if err != nil || result == nil {
		return 0, false, err
	}
	if _, isNone := result.(starlark.NoneType); isNone {
		return 0, false, nil
	}
	value, ok := starlark.AsFloat(result)
	if !ok {
		return 0, false, fmt.Errorf("achievement %s: progress returned %s, want a number", code, result.Type())
	}
	// Зажим здесь, а не у вызывающего: полоса прогресса рисуется в трёх местах,
	// и ни одно из них не должно помнить, что скрипт мог вернуть 1.5.
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return value, true, nil
}

// Visible отвечает, показывать ли ачивку этому человеку. Скрипт без хука
// показывает её — так же, как ачивка вовсе без скриптовой видимости.
func (e *Engine) Visible(code string, f Facts) (bool, error) {
	result, err := e.call(code, HookVisible, f)
	if err != nil || result == nil {
		return true, err
	}
	return bool(result.Truth()), nil
}

// call готовит факты и выполняет один хук.
func (e *Engine) call(code, hook string, f Facts) (starlark.Value, error) {
	if e == nil || code == "" {
		return nil, nil
	}
	if !e.runtime.Has(code) {
		return nil, &ErrUnknownAchievement{Code: code}
	}
	f.Config = e.runtime.MergeConfig(code, f.Config)
	if f.Now.IsZero() {
		f.Now = time.Now()
	}
	return e.runtime.CallHook(code, hook, factsValue(f))
}
