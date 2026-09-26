// Package upload сохраняет файл из multipart-запроса на диск — одним способом
// для вложений чата, изображений витрины и APK релизов.
//
// Раньше у каждого из четырёх путей загрузки были свои правила: только магазин
// удалял недописанный файл и проверял тип по содержимому, чат определял тип
// двумя способами, потолок размера у каждого считался по-своему. Здесь всё это
// в одном месте: MaxBytesReader на тело запроса, выбор расширения по
// содержимому или по имени клиента, запись во временный файл и переименование —
// файл либо лежит целиком, либо не лежит вовсе.
package upload

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Ошибки, по которым обработчик выбирает ответ. Все — ошибки запроса, а не
// сервера: слишком большой, без файла, не того типа.
var (
	// ErrTooLarge — тело запроса больше потолка.
	ErrTooLarge = errors.New("file too large")
	// ErrNoFile — в форме нет файла в ожидаемом поле.
	ErrNoFile = errors.New("file is required")
	// ErrUnsupported — тип файла не из белого списка; текст уточняет, почему.
	ErrUnsupported = errors.New("unsupported file type")
)

// Acceptor решает, под каким расширением сохранить файл, по имени, которое
// прислал клиент, и по типу содержимого (http.DetectContentType). Отказ —
// ошибка, обёрнутая в ErrUnsupported.
type Acceptor func(clientName, contentType string) (ext string, err error)

// activeContent — типы, которые браузер выполнил бы в источнике приложения.
// Что бы ни говорило расширение, такое содержимое не сохраняется: вложение
// превратилось бы в хранимую XSS.
func activeContent(contentType string) bool {
	return strings.HasPrefix(contentType, "text/html") ||
		strings.HasPrefix(contentType, "text/xml") ||
		strings.HasPrefix(contentType, "application/xml") ||
		strings.HasPrefix(contentType, "image/svg") ||
		strings.HasPrefix(contentType, "text/javascript") ||
		strings.HasPrefix(contentType, "application/javascript")
}

// ByContent выбирает расширение по типу содержимого: имя клиента не читается
// вовсе, сервер называет файл сам. types — MIME → расширение с точкой.
func ByContent(types map[string]string, reason string) Acceptor {
	return func(_, contentType string) (string, error) {
		mime := contentType
		if i := strings.IndexByte(mime, ';'); i >= 0 {
			mime = strings.TrimSpace(mime[:i])
		}
		ext, ok := types[mime]
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrUnsupported, reason)
		}
		return ext, nil
	}
}

// ByExtension принимает файл по расширению из имени клиента и проверяет
// содержимое: для расширения с перечисленными типами содержимое обязано быть
// одним из них (картинка с расширением .jpg обязана быть картинкой), для
// расширения без списка — любым, кроме активного.
func ByExtension(exts map[string][]string) Acceptor {
	return func(clientName, contentType string) (string, error) {
		ext := strings.ToLower(filepath.Ext(clientName))
		if ext == "" {
			return "", fmt.Errorf("%w: файл без расширения не поддерживается", ErrUnsupported)
		}
		allowed, ok := exts[ext]
		if !ok {
			return "", fmt.Errorf("%w: недопустимый тип файла", ErrUnsupported)
		}
		if len(allowed) == 0 {
			if activeContent(contentType) {
				return "", fmt.Errorf("%w: недопустимое содержимое файла", ErrUnsupported)
			}
			return ext, nil
		}
		for _, want := range allowed {
			if strings.HasPrefix(contentType, want) {
				return ext, nil
			}
		}
		return "", fmt.Errorf("%w: содержимое файла не соответствует расширению %s", ErrUnsupported, ext)
	}
}

// Options — как принимать файл.
type Options struct {
	// Field — имя поля формы с файлом.
	Field string
	// MaxBytes — потолок тела запроса целиком (файл плюс остальные поля).
	MaxBytes int64
	// Dir — каталог назначения; создаётся, если его нет.
	Dir string
	// Name — имя файла без расширения. Пусто — uuid: имя даёт сервер, а не клиент.
	Name string
	// Accept решает, принимать ли файл и под каким расширением.
	Accept Acceptor
}

// File — что сохранили.
type File struct {
	// Path — полный путь на диске; Name — имя с расширением внутри Dir.
	Path string
	Name string
	Size int64
	// ContentType — тип по содержимому, ClientName — имя, присланное клиентом.
	ContentType string
	ClientName  string
}

// memoryBuffer — сколько multipart держит в памяти; остальное уходит во
// временные файлы net/http. Потолок размера обеспечивает MaxBytesReader.
const memoryBuffer = 8 << 20

// sniffLen — сколько байт читает http.DetectContentType.
const sniffLen = 512

// Save читает файл из формы и кладёт его в opts.Dir. Форма разбирается здесь,
// поэтому остальные поля (r.FormValue) доступны вызывающему после возврата.
func Save(w http.ResponseWriter, r *http.Request, opts Options) (*File, error) {
	if opts.MaxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, opts.MaxBytes)
	}
	if err := r.ParseMultipartForm(memoryBuffer); err != nil {
		if tooLarge(err) {
			return nil, ErrTooLarge
		}
		return nil, fmt.Errorf("%w: %v", ErrNoFile, err)
	}
	file, header, err := r.FormFile(opts.Field)
	if err != nil {
		return nil, ErrNoFile
	}
	defer file.Close()
	return save(file, header, opts)
}

func save(file multipart.File, header *multipart.FileHeader, opts Options) (*File, error) {
	// Тип определяется по содержимому, а не по имени и заголовку клиента:
	// «картинка.jpg» с HTML внутри не должна лечь рядом с изображениями.
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	head = head[:n]
	contentType := http.DetectContentType(head)
	ext, err := opts.Accept(header.Filename, contentType)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, err
	}
	name := opts.Name
	if name == "" {
		name = uuid.New().String()
	}
	name += ext
	final := filepath.Join(opts.Dir, name)

	// Пишем во временный файл рядом и переименовываем: читатель каталога
	// никогда не увидит недописанный файл, а сбой не оставит после себя обрезка.
	tmp, err := os.CreateTemp(opts.Dir, ".upload-*")
	if err != nil {
		return nil, err
	}
	size, err := io.Copy(tmp, io.MultiReader(bytes.NewReader(head), file))
	if err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err == nil {
		err = os.Rename(tmp.Name(), final)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return &File{Path: final, Name: name, Size: size, ContentType: contentType, ClientName: header.Filename}, nil
}

// tooLarge узнаёт отказ MaxBytesReader, как бы его ни обернул разбор формы.
func tooLarge(err error) bool {
	var maxBytes *http.MaxBytesError
	return errors.As(err, &maxBytes) || strings.Contains(err.Error(), "request body too large")
}
