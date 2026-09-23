package photoproof

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/jpeg"
	"math"
	"strconv"
	"strings"
)

// Проверка снимка из двух независимых частей: подпись файла и метка в
// изображении. Подпись ломается от любого пересохранения, метка его переживает;
// вместе они отличают снимок из приложения, пересохранённый снимок и снимок, к
// приложению не имевший отношения. Устройство и смысл статусов описаны вне
// репозитория; этот файл обязан считать ровно так же, как клиент
// (frontend/src/modules/photo-proof), что закреплено векторами в testdata.

// --- Подпись -----------------------------------------------------------------

const (
	sealMarker = 0xEB // APP11
	sealMagic  = "HLS1"
	sealLen    = len(sealMagic) + sha256.Size
)

// jpegSegment — сегмент заголовка JPEG: с маркера до конца данных.
type jpegSegment struct {
	marker     byte
	start, end int
}

// headerSegments перечисляет сегменты до начала данных изображения (SOS).
func headerSegments(data []byte) []jpegSegment {
	var segments []jpegSegment
	i := 2
	for i+4 <= len(data) && data[i] == 0xFF {
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		end := i + 2 + length
		if length < 2 || end > len(data) {
			break
		}
		segments = append(segments, jpegSegment{marker: marker, start: i, end: end})
		i = end
	}
	return segments
}

// splitSeal находит сегмент подписи и возвращает файл без него и саму подпись.
func splitSeal(data []byte) (stripped, seal []byte, ok bool) {
	for _, s := range headerSegments(data) {
		payload := data[s.start+4 : s.end]
		if s.marker == sealMarker && len(payload) == sealLen && string(payload[:len(sealMagic)]) == sealMagic {
			stripped = make([]byte, 0, len(data)-(s.end-s.start))
			stripped = append(stripped, data[:s.start]...)
			stripped = append(stripped, data[s.end:]...)
			return stripped, payload[len(sealMagic):], true
		}
	}
	return data, nil, false
}

// coordText — координата так, как её подписывает клиент: шесть знаков после
// точки, пусто — если координаты нет.
func coordText(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 6, 64)
}

// sealMessage — то, что подписывается: хеш файла без подписи и данные снимка.
func sealMessage(stripped []byte, in CheckInput) []byte {
	sum := sha256.Sum256(stripped)
	return []byte(strings.Join([]string{
		hex.EncodeToString(sum[:]),
		in.OrderID.String(),
		in.SymbolCode,
		strconv.FormatInt(in.DeviceTakenAt.Unix(), 10),
		coordText(in.DeviceLat),
		coordText(in.DeviceLon),
	}, "\n"))
}

func sealOf(stripped []byte, in CheckInput) []byte {
	mac := hmac.New(sha256.New, in.Key)
	mac.Write(sealMessage(stripped, in))
	return mac.Sum(nil)
}

// addSeal вставляет подпись после сегментов APP0/APP1, как это делает клиент.
func addSeal(data []byte, in CheckInput) []byte {
	stripped, _, _ := splitSeal(data)
	insertAt := 2
	for _, s := range headerSegments(stripped) {
		if s.marker == 0xE0 || s.marker == 0xE1 {
			insertAt = s.end
		}
	}
	payload := append([]byte(sealMagic), sealOf(stripped, in)...)
	segment := []byte{0xFF, sealMarker, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)

	out := make([]byte, 0, len(stripped)+len(segment))
	out = append(out, stripped[:insertAt]...)
	out = append(out, segment...)
	return append(out, stripped[insertAt:]...)
}

func checkSeal(data []byte, in CheckInput) string {
	stripped, seal, ok := splitSeal(data)
	if !ok {
		return SealMissing
	}
	if hmac.Equal(seal, sealOf(stripped, in)) {
		return SealValid
	}
	return SealInvalid
}

// --- Метка -------------------------------------------------------------------

const (
	markBits = 64
	// markStrength — насколько разводятся два коэффициента блока. Подобрано так,
	// чтобы метка переживала пересжатие с качеством от 70 и оставалась незаметной.
	markStrength = 12.0
	// markThreshold — средняя уверенность по битам, ниже которой метки нет.
	markThreshold = 0.3
	// minMarkBlocks — меньше блоков на бит голосование не различит шум.
	minMarkBlocksPerBit = 8
)

// mulberry32 — генератор, повторённый на клиенте один в один.
type mulberry32 uint32

func (m *mulberry32) next() uint32 {
	*m += 0x6D2B79F5
	t := uint32(*m)
	t = (t ^ (t >> 15)) * (t | 1)
	t = (t + (t^(t>>7))*(t|61)) ^ t
	return t ^ (t >> 14)
}

func markSeed(key []byte) mulberry32 {
	sum := sha256.Sum256(append(append([]byte{}, key...), []byte("mark")...))
	return mulberry32(binary.BigEndian.Uint32(sum[:4]))
}

// markPayload — 64 бита: номер жеста (8), хеш заказа (32), минуты съёмки (24).
func markPayload(in CheckInput) uint64 {
	orderHash := sha256.Sum256([]byte(in.OrderID.String()))
	minutes := uint64(in.DeviceTakenAt.Unix()/60) & 0xFFFFFF
	return uint64(in.SymbolNumber&0xFF)<<56 | uint64(binary.BigEndian.Uint32(orderHash[:4]))<<24 | minutes
}

func payloadBit(payload uint64, i int) uint8 {
	return uint8(payload>>(63-uint(i))) & 1
}

// blockAssignment — какому биту служит блок и с каким знаком.
type blockAssignment struct {
	bit  int
	chip uint8
}

func assignments(key []byte, blocks int) []blockAssignment {
	rng := markSeed(key)
	out := make([]blockAssignment, blocks)
	for b := range out {
		out[b] = blockAssignment{bit: int(rng.next() % markBits), chip: uint8(rng.next() & 1)}
	}
	return out
}

// dctBasis[u][v][y][x] — ортонормированный базис DCT-II 8×8; u — по горизонтали.
var dctBasis = func() (basis [3][3][8][8]float64) {
	a := func(k int) float64 {
		if k == 0 {
			return math.Sqrt(1.0 / 8)
		}
		return 0.5
	}
	for u := 0; u < 3; u++ {
		for v := 0; v < 3; v++ {
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					basis[u][v][y][x] = a(u) * a(v) *
						math.Cos(float64(2*x+1)*float64(u)*math.Pi/16) *
						math.Cos(float64(2*y+1)*float64(v)*math.Pi/16)
				}
			}
		}
	}
	return
}()

// blockDiff — разность коэффициентов (1,2) и (2,1) блока яркости.
func blockDiff(lum func(x, y int) float64, bx, by int) float64 {
	var c12, c21 float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			f := lum(bx*8+x, by*8+y)
			c12 += f * dctBasis[1][2][y][x]
			c21 += f * dctBasis[2][1][y][x]
		}
	}
	return c12 - c21
}

// embedMark ставит метку в изображение: разводит коэффициенты каждого блока
// в сторону бита. Меняется только яркость — одинаковой добавкой к R, G и B,
// которая цветоразностные составляющие не трогает. Используется тестами и
// векторами; в продукте метку ставит клиент.
func embedMark(img *image.RGBA, in CheckInput) {
	w, h := img.Bounds().Dx()/8, img.Bounds().Dy()/8
	payload := markPayload(in)
	lum := func(x, y int) float64 {
		p := img.PixOffset(x, y)
		return 0.299*float64(img.Pix[p]) + 0.587*float64(img.Pix[p+1]) + 0.114*float64(img.Pix[p+2])
	}
	for b, a := range assignments(in.Key, w*h) {
		bx, by := b%w, b/w
		want := payloadBit(payload, a.bit) ^ a.chip
		d := blockDiff(lum, bx, by)
		var delta float64
		if want == 1 && d < markStrength {
			delta = (markStrength - d) / 2
		} else if want == 0 && d > -markStrength {
			delta = (-markStrength - d) / 2
		}
		if delta == 0 {
			continue
		}
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				change := delta * (dctBasis[1][2][y][x] - dctBasis[2][1][y][x])
				p := img.PixOffset(bx*8+x, by*8+y)
				for c := 0; c < 3; c++ {
					img.Pix[p+c] = clampByte(float64(img.Pix[p+c]) + change)
				}
			}
		}
	}
}

func clampByte(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(math.Round(v))
}

// checkMark ищет метку заказа в изображении.
func checkMark(data []byte, in CheckInput) string {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return MarkNotFound
	}
	lum := luminance(img)
	w, h := img.Bounds().Dx()/8, img.Bounds().Dy()/8
	if w*h < markBits*minMarkBlocksPerBit {
		return MarkNotFound
	}

	var votes, counts [markBits]float64
	for b, a := range assignments(in.Key, w*h) {
		observed := uint8(0)
		if blockDiff(lum, b%w, b/w) > 0 {
			observed = 1
		}
		if observed^a.chip == 1 {
			votes[a.bit]++
		} else {
			votes[a.bit]--
		}
		counts[a.bit]++
	}

	var strength float64
	var decoded uint64
	for i := 0; i < markBits; i++ {
		if counts[i] == 0 {
			return MarkNotFound
		}
		strength += math.Abs(votes[i]) / counts[i]
		if votes[i] > 0 {
			decoded |= 1 << uint(63-i)
		}
	}
	if strength/markBits < markThreshold {
		return MarkNotFound
	}
	if decoded != markPayload(in) {
		return MarkMismatch
	}
	return MarkFound
}

// luminance отдаёт яркость пикселя по той же формуле, что клиент считает из RGB.
func luminance(img image.Image) func(x, y int) float64 {
	if ycc, ok := img.(*image.YCbCr); ok {
		return func(x, y int) float64 { return float64(ycc.Y[ycc.YOffset(x, y)]) }
	}
	return func(x, y int) float64 {
		r, g, b, _ := img.At(x, y).RGBA()
		return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
	}
}

// --- Проверка ----------------------------------------------------------------

// ProofChecker — проверка снимка подписью и меткой.
type ProofChecker struct{}

// NewChecker создаёт проверку снимка.
func NewChecker() ProofChecker { return ProofChecker{} }

// Sign вставляет в снимок подпись — то же, что делает приложение перед
// отправкой. Нужна тестам и проверочным инструментам: своя реализация подписи
// рядом с этой разошлась бы с ней при первой же правке.
func Sign(data []byte, in CheckInput) []byte { return addSeal(data, in) }

// Check проверяет снимок. Без ключа заказа проверять нечем.
func (ProofChecker) Check(data []byte, in CheckInput) (string, string) {
	if len(in.Key) == 0 {
		return SealMissing, MarkNotFound
	}
	return checkSeal(data, in), checkMark(data, in)
}

var _ Checker = ProofChecker{}
