package photoproof

import (
	"bytes"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// exifFacts — то, что сверка берёт из EXIF снимка.
type exifFacts struct {
	TakenAt *time.Time
	Lat     *float64
	Lon     *float64
}

// readExif достаёт из JPEG время съёмки и координаты. Снимок без EXIF — не
// ошибка: поля остаются пустыми, а сверка показывает их отсутствие.
//
// В EXIF время хранится без часового пояса, по часам телефона. Оно
// истолковывается в поясе, который телефон назвал вместе с временем съёмки
// (loc): иначе снимок из Москвы сдвинулся бы на три часа от собственной
// отметки времени.
func readExif(data []byte, loc *time.Location) exifFacts {
	var facts exifFacts
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return facts
	}
	if tag, err := x.Get(exif.DateTimeOriginal); err == nil {
		if raw, err := tag.StringVal(); err == nil {
			if loc == nil {
				loc = time.UTC
			}
			if t, err := time.ParseInLocation("2006:01:02 15:04:05", raw, loc); err == nil {
				facts.TakenAt = &t
			}
		}
	}
	if lat, lon, err := x.LatLong(); err == nil && validCoordinates(lat, lon) && !(lat == 0 && lon == 0) {
		facts.Lat, facts.Lon = &lat, &lon
	}
	return facts
}

func validCoordinates(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
