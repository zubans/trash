package achievement

import (
	"fmt"
	"io/fs"
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
//
// Всё состояние живёт в рантайме: манифест ачивки выводится из общего
// манифеста при каждом обращении, а не хранится второй картой под вторым
// мьютексом, которая могла бы разойтись с первой.
type Engine struct {
	runtime *script.Engine
}

// New создаёт пустой движок.
func New(limits Limits) *Engine {
	return &Engine{
		runtime: script.New(limits, script.Options{Builtins: predeclared, Hooks: hookNames}),
	}
}

// Load компилирует каждый каталог ачивки в корне fsys. Имя каталога — код, на
// который ссылается строка таблицы achievements.
func (e *Engine) Load(fsys fs.FS, label string) error {
	return script.Load(fsys, label, e)
}

// CompileFiles разбирает файлы одной ачивки по порядку и регистрирует её.
//
// Ачивка без аудитории или без событий не сломана как скрипт, но никогда не
// сработает, поэтому такая регистрация отменяется: в рантайме её не остаётся,
// а вызывающий получает ошибку, которую видно на сохранении.
func (e *Engine) CompileFiles(code string, files []SourceFile) error {
	if err := e.runtime.CompileFiles(code, files); err != nil {
		return err
	}
	raw, _ := e.runtime.Manifest(code)
	if err := validateManifest(manifestFrom(raw)); err != nil {
		e.runtime.Remove(code)
		return err
	}
	return nil
}

// validateManifest проверяет поля, без которых ачивка молча не срабатывала бы.
func validateManifest(m Manifest) error {
	if m.Audience != AudienceExecutor && m.Audience != AudienceCustomer {
		// Аудитория решает, чьим именем подставляется User, поэтому её опечатка
		// означала бы ачивку, которая молча никогда не срабатывает.
		return fmt.Errorf("achievement %s: audience %q must be %s or %s", m.Code, m.Audience, AudienceExecutor, AudienceCustomer)
	}
	if len(m.Events) == 0 {
		return fmt.Errorf("achievement %s declares no events and would never be evaluated", m.Code)
	}
	return nil
}

// Remove снимает регистрацию ачивки.
func (e *Engine) Remove(code string) {
	if e == nil {
		return
	}
	e.runtime.Remove(code)
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
	raw, ok := e.runtime.Manifest(code)
	if !ok {
		return Manifest{}, false
	}
	return manifestFrom(raw), true
}

// Manifests перечисляет загруженные ачивки — для админ-панели.
func (e *Engine) Manifests() []Manifest {
	if e == nil {
		return nil
	}
	raws := e.runtime.Manifests()
	out := make([]Manifest, 0, len(raws))
	for _, raw := range raws {
		out = append(out, manifestFrom(raw))
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
