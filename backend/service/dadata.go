package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"healthlogin/backend/metrics"
)

// DaData подсказывает российские адреса из государственного адресного реестра.
//
// Он заменил Nominatim при вводе адреса по трём причинам, которые старая
// схема решить не могла: он знает квартиры, которых в OpenStreetMap нет вовсе;
// он возвращает координаты вместе с подсказкой, поэтому выбор адреса больше не
// требует второго похода за геокодированием; и он рассчитан на набор текста —
// в отличие от публичного экземпляра Nominatim, чья политика использования
// запрещает автодополнение и который поэтому был здесь ограничен одним запросом
// в секунду на всю платформу.
type DaData struct {
	client  *http.Client
	apiKey  string
	baseURL string
	// inflight ограничивает, сколько запросов может быть у провайдера одновременно.
	//
	// Таймаут клиента ограничивает один запрос, а не их количество. Когда
	// провайдер замедляется, ввод адреса — нет: каждое нажатие клавиши каждого
	// пользователя запускает новый вызов, каждый держит горутину и соединение
	// вплоть до таймаута, и куча растёт всё время, пока провайдер тормозит. Это
	// ограничивает кучу; вызывающие сверх предела быстро падают с
	// ErrAddressProviderBusy, о которой API уже умеет сообщать.
	inflight chan struct{}
}

// defaultDaDataConcurrency — потолок одновременных вызовов провайдера. Он
// намеренно мал: ввод адреса обслуживается из кэша геокодирования для всего уже
// виденного, а очередь, живущая дольше терпения вызывающего, помогает
// никому.
const defaultDaDataConcurrency = 8

const daDataSuggestURL = "https://suggestions.dadata.ru/suggestions/api/4_1/rs/suggest/address"

// ErrNoAddressProvider возвращается, когда провайдер подсказок не настроен.
var ErrNoAddressProvider = errors.New("address suggestions are not configured")

// ErrAddressProviderBusy сообщает, что провайдер ограничил запрос по частоте,
// поэтому вызывающему стоит отступить и повторить, а не считать это «не найдено».
var ErrAddressProviderBusy = errors.New("address provider is busy, try again")

// NewDaData собирает подсказчик из DADATA_API_KEY. Он возвращает nil, когда
// ключа нет, чтобы процесс всё равно стартовал; ввод и разрешение адреса тогда
// сообщают ErrNoAddressProvider, пока ключ не задан, — вместо того чтобы весь
// сервис не смог подняться.
func NewDaData() *DaData {
	key := strings.TrimSpace(os.Getenv("DADATA_API_KEY"))
	if key == "" {
		return nil
	}
	concurrency := defaultDaDataConcurrency
	if v := strings.TrimSpace(os.Getenv("DADATA_MAX_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}
	return &DaData{
		// Ввод адреса интерактивен: подсказка, пришедшая после следующего
		// нажатия клавиши, бесполезна, поэтому таймаут короткий.
		client:   &http.Client{Timeout: 4 * time.Second},
		apiKey:   key,
		baseURL:  daDataSuggestURL,
		inflight: make(chan struct{}, concurrency),
	}
}

// daDataRequest — документированное тело запроса.
type daDataRequest struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	// Locations ограничивает результаты Россией. Без этого реестр предлагает и
	// адреса соседних стран, которые он тоже содержит.
	Locations []map[string]string `json:"locations,omitempty"`
	Language  string              `json:"language,omitempty"`
}

type daDataResponse struct {
	Suggestions []struct {
		Value             string `json:"value"`
		UnrestrictedValue string `json:"unrestricted_value"`
		Data              struct {
			Region     string `json:"region_with_type"`
			City       string `json:"city_with_type"`
			Settlement string `json:"settlement_with_type"`
			Street     string `json:"street_with_type"`
			House      string `json:"house"`
			HouseType  string `json:"house_type"`
			Block      string `json:"block"`
			BlockType  string `json:"block_type"`
			Flat       string `json:"flat"`
			FiasID     string `json:"fias_id"`
			GeoLat     string `json:"geo_lat"`
			GeoLon     string `json:"geo_lon"`
		} `json:"data"`
	} `json:"suggestions"`
}

// Suggest возвращает подсказки адресов по частичному запросу.
func (d *DaData) Suggest(ctx context.Context, query string, count int) ([]Address, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is empty")
	}
	if len([]rune(query)) < 3 {
		return []Address{}, nil
	}
	if count <= 0 || count > 20 {
		count = 7
	}

	body, err := json.Marshal(daDataRequest{
		Query:     query,
		Count:     count,
		Locations: []map[string]string{{"country": "Россия"}},
		Language:  "ru",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+d.apiKey)

	// Занимаем слот или сообщаем, что провайдер занят. Ожидание слота лишь
	// перенесло бы очередь от провайдера сюда, а вызывающий — это человек,
	// набирающий адрес.
	if d.inflight != nil {
		select {
		case d.inflight <- struct{}{}:
			defer func() { <-d.inflight }()
		default:
			metrics.UpstreamResult("dadata", "suggest", "throttled", 0)
			return nil, ErrAddressProviderBusy
		}
	}

	started := time.Now()
	resp, err := d.client.Do(req)
	if err != nil {
		metrics.UpstreamCall("dadata", "suggest", time.Since(started), err)
		return nil, err
	}
	defer resp.Body.Close()
	// Отвергнутый ключ и исчерпанная квота для пользователя оба выглядят как «нет
	// подсказок», поэтому это отдельные значения лейбла, а не одна корзина ошибок.
	metrics.UpstreamResult("dadata", "suggest", daDataOutcome(resp.StatusCode), time.Since(started))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// Сам ключ никогда не включается в ошибку: он оказался бы в логах и,
		// через обработчик, в сообщении об ошибке у клиента.
		return nil, errors.New("address provider rejected the credentials")
	case http.StatusTooManyRequests:
		return nil, ErrAddressProviderBusy
	default:
		return nil, fmt.Errorf("address provider returned status %d", resp.StatusCode)
	}

	var parsed daDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := make([]Address, 0, len(parsed.Suggestions))
	for _, s := range parsed.Suggestions {
		city := s.Data.City
		if city == "" {
			// У сёл и посёлков нет города, и их отбрасывание сделало бы любой адрес
			// вне города непригодным.
			city = s.Data.Settlement
		}

		addr := Address{
			Region: s.Data.Region,
			City:   city,
			Street: s.Data.Street,
			House:  composeHouse(s.Data.House, s.Data.HouseType, s.Data.Block, s.Data.BlockType),
			Flat:   s.Data.Flat,
			FiasID: s.Data.FiasID,
			Source: SourceDaData,
		}
		if lat, err := strconv.ParseFloat(s.Data.GeoLat, 64); err == nil {
			addr.Lat = &lat
		}
		if lon, err := strconv.ParseFloat(s.Data.GeoLon, 64); err == nil {
			addr.Lon = &lon
		}

		// Value приходит от провайдера, чтобы список читался так, как пишет реестр;
		// Compose закрывает редкую подсказку, у которой вообще нет частей.
		addr.Value = strings.TrimSpace(s.Value)
		if addr.Value == "" {
			addr.Value = addr.Compose()
		}
		out = append(out, addr)
	}
	return out, nil
}

// composeHouse сохраняет идентификатор здания целиком. Дом — не всегда число:
// «12 к. 1» и «10 стр. 2» — обычные российские адреса, а старое регулярное
// выражение отвергало каждый из них.
func composeHouse(house, houseType, block, blockType string) string {
	house = strings.TrimSpace(house)
	if house == "" {
		return ""
	}
	// Префикс типа отбрасывается для обычных домов («д. 12» добавляется обратно
	// при сборке строки), но сохраняется для всего прочего, например «влд. 5».
	if t := strings.TrimSpace(houseType); t != "" && t != "д" {
		house = t + ". " + house
	}
	if b := strings.TrimSpace(block); b != "" {
		bt := strings.TrimSpace(blockType)
		if bt == "" {
			bt = "к"
		}
		house += " " + bt + ". " + b
	}
	return house
}

// daDataOutcome сопоставляет HTTP-статус с лейблом исхода, используемым в метриках.
func daDataOutcome(status int) string {
	switch status {
	case http.StatusOK:
		return "ok"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "http_error"
	}
}
