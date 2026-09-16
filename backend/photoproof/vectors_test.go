package photoproof

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Векторы — договор между сервером и клиентом (frontend/src/modules/photo-proof):
// одни и те же входные данные обязаны давать одни и те же числа по обе стороны.
// Клиентский тест читает этот же файл. Перегенерировать:
//
//	UPDATE_PROOF_VECTORS=1 go test ./photoproof/ -run TestProofVectors
const vectorsPath = "testdata/proof_vectors.json"

type proofVectors struct {
	Input struct {
		KeyHex        string  `json:"key_hex"`
		OrderID       string  `json:"order_id"`
		SymbolCode    string  `json:"symbol_code"`
		SymbolNumber  int     `json:"symbol_number"`
		DeviceTakenAt string  `json:"device_taken_at"`
		DeviceLat     float64 `json:"device_lat"`
		DeviceLon     float64 `json:"device_lon"`
		// StrippedHex — байты «файла без подписи» для проверки сообщения подписи.
		StrippedHex string `json:"stripped_hex"`
	} `json:"input"`
	Seed        uint32             `json:"seed"`
	RandomFirst []uint32           `json:"random_first"`
	PayloadHex  string             `json:"payload_hex"`
	Assignments []vectorAssignment `json:"assignments_first"`
	SealMessage string             `json:"seal_message"`
	SealHex     string             `json:"seal_hex"`
	// Pattern — добавка к яркости блока на единицу поправки: базис (1,2) минус
	// базис (2,1), по строкам, округлено до 6 знаков.
	Pattern [64]float64 `json:"pattern"`
}

type vectorAssignment struct {
	Bit  int   `json:"bit"`
	Chip uint8 `json:"chip"`
}

func buildVectors(t *testing.T, v *proofVectors) proofVectors {
	t.Helper()
	key, err := hex.DecodeString(v.Input.KeyHex)
	if err != nil {
		t.Fatal(err)
	}
	takenAt, err := time.Parse(time.RFC3339, v.Input.DeviceTakenAt)
	if err != nil {
		t.Fatal(err)
	}
	lat, lon := v.Input.DeviceLat, v.Input.DeviceLon
	in := CheckInput{
		OrderID: uuid.MustParse(v.Input.OrderID), SymbolCode: v.Input.SymbolCode, SymbolNumber: v.Input.SymbolNumber,
		Key: key, DeviceTakenAt: takenAt, DeviceLat: &lat, DeviceLon: &lon,
	}
	stripped, err := hex.DecodeString(v.Input.StrippedHex)
	if err != nil {
		t.Fatal(err)
	}

	out := proofVectors{Input: v.Input}
	seed := markSeed(key)
	out.Seed = uint32(seed)
	rng := seed
	for i := 0; i < 5; i++ {
		out.RandomFirst = append(out.RandomFirst, rng.next())
	}
	payload := markPayload(in)
	out.PayloadHex = hex.EncodeToString([]byte{
		byte(payload >> 56), byte(payload >> 48), byte(payload >> 40), byte(payload >> 32),
		byte(payload >> 24), byte(payload >> 16), byte(payload >> 8), byte(payload),
	})
	for _, a := range assignments(key, 20) {
		out.Assignments = append(out.Assignments, vectorAssignment{Bit: a.bit, Chip: a.chip})
	}
	out.SealMessage = string(sealMessage(stripped, in))
	out.SealHex = hex.EncodeToString(sealOf(stripped, in))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			out.Pattern[y*8+x] = math.Round((dctBasis[1][2][y][x]-dctBasis[2][1][y][x])*1e6) / 1e6
		}
	}
	return out
}

func TestProofVectors(t *testing.T) {
	var stored proofVectors
	stored.Input.KeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	stored.Input.OrderID = "8f3c2b1a-4d5e-4f60-8a7b-9c0d1e2f3a4b"
	stored.Input.SymbolCode = "bunny"
	stored.Input.SymbolNumber = 3
	stored.Input.DeviceTakenAt = "2026-09-16T14:30:05+03:00"
	stored.Input.DeviceLat = 55.7558
	stored.Input.DeviceLon = 37.6173
	stored.Input.StrippedHex = hex.EncodeToString([]byte("\xff\xd8 not really a jpeg, just bytes \xff\xd9"))

	if os.Getenv("UPDATE_PROOF_VECTORS") != "" {
		built := buildVectors(t, &stored)
		data, err := json.MarshalIndent(built, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	data, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("read vectors (generate with UPDATE_PROOF_VECTORS=1): %v", err)
	}
	var want proofVectors
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	got := buildVectors(t, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the server no longer computes the agreed vectors:\n got %+v\nwant %+v", got, want)
	}
}
