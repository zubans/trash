package photoproof

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/jpeg"
	"math"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
)

// sceneImage — изображение с фактурой, похожей на снимок: плавные переходы,
// детали и шум. Метку проверяем не на однотонной заливке, а на том, что снимает
// камера.
func sceneImage(w, h int, seed int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := mrand.New(mrand.NewSource(seed))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := 90 + 60*math.Sin(float64(x)/37) + 40*math.Cos(float64(y)/23)
			detail := 25 * math.Sin(float64(x*y)/900)
			noise := rng.Float64()*16 - 8
			p := img.PixOffset(x, y)
			img.Pix[p] = clampByte(base + detail + noise)
			img.Pix[p+1] = clampByte(base*0.9 + noise)
			img.Pix[p+2] = clampByte(base*0.7 - detail + noise)
			img.Pix[p+3] = 255
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func recompress(t *testing.T, data []byte, quality int) []byte {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return encodeJPEG(t, img, quality)
}

func checkInput(t *testing.T) CheckInput {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	lat, lon := 55.755800, 37.617300
	return CheckInput{
		OrderID: uuid.New(), SymbolCode: "bunny", SymbolNumber: 3, Key: key,
		DeviceTakenAt: time.Date(2026, 9, 16, 14, 30, 5, 0, time.UTC), DeviceLat: &lat, DeviceLon: &lon,
	}
}

// markedPhoto делает то, что делает приложение: метка, кодирование, подпись.
func markedPhoto(t *testing.T, in CheckInput) []byte {
	t.Helper()
	img := sceneImage(960, 720, 1)
	embedMark(img, in)
	return addSeal(encodeJPEG(t, img, 92), in)
}

func TestCheckerOnAppPhoto(t *testing.T) {
	in := checkInput(t)
	photo := markedPhoto(t, in)

	seal, mark := NewChecker().Check(photo, in)
	if seal != SealValid || mark != MarkFound {
		t.Fatalf("photo from the app: %s %s", seal, mark)
	}
}

func TestCheckerOnResavedPhoto(t *testing.T) {
	in := checkInput(t)
	photo := markedPhoto(t, in)

	for _, quality := range []int{90, 80, 70} {
		seal, mark := NewChecker().Check(recompress(t, photo, quality), in)
		if seal != SealMissing || mark != MarkFound {
			t.Errorf("resaved at quality %d: %s %s", quality, seal, mark)
		}
	}
}

func TestCheckerOnForeignPhoto(t *testing.T) {
	in := checkInput(t)

	// Снимок не из приложения.
	plain := encodeJPEG(t, sceneImage(960, 720, 7), 92)
	if seal, mark := NewChecker().Check(plain, in); seal != SealMissing || mark != MarkNotFound {
		t.Errorf("plain photo: %s %s", seal, mark)
	}

	// Снимок другого заказа: чужой ключ.
	other := checkInput(t)
	if seal, mark := NewChecker().Check(markedPhoto(t, other), in); seal != SealInvalid || mark != MarkNotFound {
		t.Errorf("photo of another order: %s %s", seal, mark)
	}
}

func TestCheckerOnTamperedPhoto(t *testing.T) {
	in := checkInput(t)
	photo := markedPhoto(t, in)

	// Подмена данных при целом файле: другой жест в запросе.
	swapped := in
	swapped.SymbolCode, swapped.SymbolNumber = "ok", 4
	if seal, mark := NewChecker().Check(photo, swapped); seal != SealInvalid || mark != MarkMismatch {
		t.Errorf("other gesture claimed: %s %s", seal, mark)
	}

	// Подмена координат в запросе.
	moved := in
	lat := 59.9386
	moved.DeviceLat = &lat
	if seal, _ := NewChecker().Check(photo, moved); seal != SealInvalid {
		t.Errorf("other coordinates claimed: %s", seal)
	}

	// Изменённый байт изображения.
	broken := append([]byte{}, photo...)
	broken[len(broken)-100] ^= 0xFF
	if seal, _ := NewChecker().Check(broken, in); seal != SealInvalid {
		t.Errorf("changed image byte: %s", seal)
	}
}

// Метка невидима: средняя поправка яркости — доли уровня, максимальная —
// несколько уровней из 255.
func TestMarkIsInvisible(t *testing.T) {
	in := checkInput(t)
	original := sceneImage(960, 720, 1)
	marked := image.NewRGBA(original.Bounds())
	copy(marked.Pix, original.Pix)
	embedMark(marked, in)

	var sum, max float64
	for i := 0; i < len(original.Pix); i += 4 {
		d := math.Abs(float64(marked.Pix[i]) - float64(original.Pix[i]))
		sum += d
		if d > max {
			max = d
		}
	}
	mean := sum / float64(len(original.Pix)/4)
	if mean > 3 || max > 12 {
		t.Fatalf("mark is visible: mean change %.2f, max %.0f", mean, max)
	}
}
