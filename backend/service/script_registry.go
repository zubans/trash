package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"

	"healthlogin/backend/script"
)

// scriptRegistry держит движок скриптов в согласии с базой. Раньше поведения и
// ачивки держали по копии Sync/SyncAll — с одинаковой логикой «скомпилировать
// живые, снять исчезнувшие, не трогать библиотеку» — и обе безусловно
// перекомпилировали каждый скрипт раз в минуту на каждом процессе. Здесь
// логика одна, и скрипт, чей текст не менялся, не компилируется повторно:
// реестр помнит хеш последней компиляции.
type scriptRegistry struct {
	// tag — префикс в журнале.
	tag string
	// compile регистрирует скрипт в движке; remove снимает его.
	compile func(code string, files []script.SourceFile) error
	remove  func(code string)
	// codes перечисляет всё, что сейчас есть в движке.
	codes func() []string
	// owned сообщает, ведает ли реестр этим кодом. Чужие — библиотека из
	// бинарника: их скрипт базе не подчиняется и с регистрации не снимается.
	owned func(code string) bool

	mu    sync.Mutex
	state map[string]scriptState
}

// scriptState — что реестр помнит о скрипте: хеш последнего виденного текста
// и удалась ли его компиляция. Сломанный текст тоже запоминается — иначе он
// компилировался бы заново и падал каждый проход.
type scriptState struct {
	hash string
	ok   bool
}

// scriptSource — один скрипт из базы, как его видит реестр.
type scriptSource struct {
	code string
	// label — как назвать скрипт в журнале и ошибке: код узла или ачивки.
	label string
	files []script.SourceFile
}

func newScriptRegistry(tag string, compile func(string, []script.SourceFile) error, remove func(string),
	codes func() []string, owned func(string) bool) *scriptRegistry {
	return &scriptRegistry{tag: tag, compile: compile, remove: remove, codes: codes, owned: owned,
		state: map[string]scriptState{}}
}

// hashFiles — отпечаток текста скрипта: константы и код вместе, потому что
// правка любого из них — новый скрипт.
func hashFiles(files []script.SourceFile) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write(f.Src)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sync компилирует один скрипт или снимает его, когда present ложно.
// Вызывается сразу после сохранения, чтобы правка применилась к следующему
// запросу на этом процессе.
func (r *scriptRegistry) sync(code string, files []script.SourceFile, present bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !present {
		r.forget(code)
		return nil
	}
	err := r.compile(code, files)
	r.state[code] = scriptState{hash: hashFiles(files), ok: err == nil}
	return err
}

// forget снимает скрипт с регистрации, если он реестру подведомствен, и
// забывает его состояние. Под r.mu.
func (r *scriptRegistry) forget(code string) {
	delete(r.state, code)
	if r.owned(code) {
		r.remove(code)
	}
}

// syncAll приводит движок к переданному списку: компилирует новые и изменённые
// скрипты, снимает исчезнувшие, не трогает неизменившиеся. Выполняется при
// старте и по таймеру: правка на другой реплике или изменение, сделанное прямо
// в базе, должны дойти и до этого процесса.
func (r *scriptRegistry) syncAll(sources []scriptSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	live := make(map[string]struct{}, len(sources))
	var failed []string
	for _, src := range sources {
		live[src.code] = struct{}{}
		hash := hashFiles(src.files)
		if seen, ok := r.state[src.code]; ok && seen.hash == hash {
			// Текст тот же, что и в прошлый раз: компилировать нечего, а
			// сломанный тогда сломан и сейчас.
			if !seen.ok {
				failed = append(failed, src.label)
			}
			continue
		}
		if err := r.compile(src.code, src.files); err != nil {
			// Сообщается, но не фатально, и намеренно не регистрируется:
			// сломанный скрипт перестаёт действовать, а не действует неправильно.
			r.remove(src.code)
			r.state[src.code] = scriptState{hash: hash, ok: false}
			failed = append(failed, src.label)
			log.Printf("[%s] %s: %v", r.tag, src.label, err)
			continue
		}
		r.state[src.code] = scriptState{hash: hash, ok: true}
	}
	// Удалённый скрипт должен исчезнуть и из движка, иначе он продолжит
	// действовать на этом процессе до перезапуска. Библиотеку не трогаем: её
	// скрипт живёт в бинарнике и базе не подчиняется.
	for _, code := range r.codes() {
		if !r.owned(code) {
			continue
		}
		if _, ok := live[code]; !ok {
			r.forget(code)
		}
	}
	for code := range r.state {
		if _, ok := live[code]; !ok {
			delete(r.state, code)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%s scripts failed to compile: %s", r.tag, strings.Join(failed, ", "))
	}
	return nil
}
