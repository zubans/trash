package service

import (
	"context"
	"fmt"
	"log"
	"strings"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
)

// Achievements — сторона ядра в скрипте ачивки: она держит движок и следит,
// чтобы в нём было ровно то, что лежит в базе.
//
// Ачивки бывают двух происхождений, и различие между ними — где лежит скрипт.
// Поставляемая приезжает со сборкой: её правило прошло ревью, и править его из
// админки нельзя. Собственную пишет администратор, и она компилируется из базы
// при старте, по таймеру и сразу после сохранения.
//
// Разделение здесь ровно то же, что у поведений услуг, и по той же причине: код
// поставляемой ачивки нельзя перехватить строкой в базе, а собственную нельзя
// затереть новой сборкой.
type Achievements struct {
	engine *achievement.Engine
	repo   repository.AchievementRepository
	// library — коды, занятые скриптами из бинарника. Снимок делается один раз,
	// сразу после загрузки: содержимое каталога за время работы не меняется.
	library map[string]struct{}
}

// NewAchievements подключает движок к каталогу. Вызывается после загрузки
// поставляемых скриптов: всё, что уже в движке, и есть библиотека.
func NewAchievements(engine *achievement.Engine, repo repository.AchievementRepository) *Achievements {
	if engine == nil {
		return nil
	}
	library := map[string]struct{}{}
	for _, m := range engine.Manifests() {
		library[m.Code] = struct{}{}
	}
	return &Achievements{engine: engine, repo: repo, library: library}
}

// Engine отдаёт движок — диспетчеру и обработчикам, которые вызывают хуки.
func (a *Achievements) Engine() *achievement.Engine {
	if a == nil {
		return nil
	}
	return a.engine
}

// IsLibrary сообщает, поставляется ли ачивка с этим кодом со сборкой. Такую
// нельзя ни удалить, ни переписать её скрипт: строка в базе исчезнет, а скрипт
// в бинарнике останется, и код будет означать одно, а выполняться другое.
func (a *Achievements) IsLibrary(code string) bool {
	if a == nil {
		return false
	}
	_, ok := a.library[code]
	return ok
}

// LibraryCodes перечисляет поставляемые ачивки — админ-панель предлагает их как
// стартовый шаблон для новой.
func (a *Achievements) LibraryCodes() []string {
	if a == nil {
		return nil
	}
	codes := make([]string, 0, len(a.library))
	for code := range a.library {
		codes = append(codes, code)
	}
	return codes
}

// sources отдаёт собственный скрипт ачивки как файлы, которые компилирует
// движок: константы первыми, как и у поставляемой.
func sources(row *repository.Achievement) []achievement.SourceFile {
	files := make([]achievement.SourceFile, 0, 2)
	if strings.TrimSpace(row.Constants) != "" {
		files = append(files, achievement.SourceFile{Name: achievement.ConfigFile, Src: []byte(row.Constants)})
	}
	return append(files, achievement.SourceFile{Name: "achievement.star", Src: []byte(row.Source)})
}

// Validate компилирует кандидата, не регистрируя его. Админ-панель вызывает его
// перед сохранением: скрипт, который не компилируется, молча перестал бы
// выдавать ачивку, и узнали бы об этом по её отсутствию через неделю.
func (a *Achievements) Validate(row *repository.Achievement) error {
	if a == nil || !row.HasOwnScript() {
		return nil
	}
	return a.engine.Validate(sources(row))
}

// Sync компилирует собственный скрипт одной ачивки или снимает его с
// регистрации, когда скрипт убрали. Вызывается сразу после сохранения, чтобы
// правка применилась к следующему событию на этом процессе.
func (a *Achievements) Sync(row *repository.Achievement) error {
	if a == nil || row == nil {
		return nil
	}
	if !row.HasOwnScript() || row.DeletedAt != nil {
		if !a.IsLibrary(row.Code) {
			a.engine.Remove(row.Code)
		}
		return nil
	}
	return a.engine.CompileFiles(row.Code, sources(row))
}

// SyncAll компилирует каждый собственный скрипт и убирает исчезнувшие. Он
// выполняется при старте и по таймеру: правка на другой реплике или изменение,
// сделанное прямо в базе, должны дойти и до этого процесса.
func (a *Achievements) SyncAll(ctx context.Context) error {
	if a == nil || a.repo == nil {
		return nil
	}
	rows, err := a.repo.ListWithScript(ctx)
	if err != nil {
		return err
	}

	live := make(map[string]struct{}, len(rows))
	var failed []string
	for _, row := range rows {
		live[row.Code] = struct{}{}
		if err := a.engine.CompileFiles(row.Code, sources(row)); err != nil {
			// Сообщается, но не фатально, и намеренно не регистрируется:
			// сломанная ачивка перестаёт выдаваться, а не выдаётся неправильно.
			a.engine.Remove(row.Code)
			failed = append(failed, row.Code)
			log.Printf("[achievement] %s: %v", row.Code, err)
		}
	}
	// Удалённая ачивка должна исчезнуть и из движка, иначе она продолжит
	// срабатывать на этом процессе до перезапуска. Поставляемые не трогаем: их
	// скрипт живёт в бинарнике и базе не подчиняется.
	for _, m := range a.engine.Manifests() {
		if a.IsLibrary(m.Code) {
			continue
		}
		if _, ok := live[m.Code]; !ok {
			a.engine.Remove(m.Code)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("achievement scripts failed to compile: %s", strings.Join(failed, ", "))
	}
	return nil
}
