package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// AddressSuggester отвечает на поиск адресов. DaData — единственный провайдер:
// откат к более грубому молча возвращал бы адреса без квартиры и без надёжного
// номера дома, а именно этот отказ данная схема и устраняет. Когда он не
// настроен, ввод адреса прямо об этом сообщает, а не деградирует во что-то
// похожее на работающее.
//
// Он же разрешает произвольный адрес в координаты (см. Resolve) — это
// единственный путь геокодирования в системе; кэш координат не даёт этому
// запасному пути платить провайдеру за один и тот же адрес дважды.
type AddressSuggester struct {
	dadata *DaData
	cache  repository.GeocodeCacheRepository
}

// NewAddressSuggester подключает провайдера и кэш разрешения. cache может быть
// nil (тогда разрешение всегда идёт к провайдеру), что делает тесты тривиальными.
func NewAddressSuggester(dadata *DaData, cache repository.GeocodeCacheRepository) *AddressSuggester {
	return &AddressSuggester{dadata: dadata, cache: cache}
}

// Configured сообщает, можно ли вообще отдавать подсказки.
func (s *AddressSuggester) Configured() bool { return s.dadata != nil }

// Suggest возвращает структурированные подсказки по частичному запросу.
func (s *AddressSuggester) Suggest(ctx context.Context, query string, count int) ([]Address, error) {
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
	}
	return s.dadata.Suggest(ctx, query, count)
}

// Resolve превращает произвольную строку адреса в координаты. Это запасной путь
// для неинтерактивных сценариев — создание заказа и регистрация, когда клиент
// не прислал координат, эндпоинт /geo/geocode, оставленный для установленных
// клиентов, и воркер дозаполнения. Выбранная подсказка уже несёт свои
// координаты и сюда попадать не должна.
//
// Он отвечает сперва из кэша, а иначе берёт лучшее совпадение провайдера —
// лучшую догадку реестра по набранной строке.
func (s *AddressSuggester) Resolve(ctx context.Context, query string) (*GeocodingResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("address is empty")
	}
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
	}

	if s.cache != nil {
		if hit, err := s.cache.Lookup(ctx, query); err == nil && hit != nil {
			metrics.UpstreamResult("dadata", "resolve", "cache_hit", 0)
			return &GeocodingResult{Lat: hit.Lat, Lon: hit.Lon, Address: hit.Address}, nil
		}
	}

	suggestions, err := s.dadata.Suggest(ctx, query, 1)
	if err != nil {
		return nil, err
	}
	for _, a := range suggestions {
		if !a.HasCoordinates() {
			continue
		}
		result := &GeocodingResult{Lat: *a.Lat, Lon: *a.Lon, Address: a.Compose()}
		if s.cache != nil {
			_ = s.cache.Save(ctx, query, result.Address, result.Lat, result.Lon)
		}
		return result, nil
	}
	return nil, errors.New("address not found")
}

// LegacySuggest возвращает форму, какой ждут установленные мобильные клиенты:
// «Россия, Город, Улица, д. N». Те сборки перепроверяют строку собственным
// регулярным выражением перед отправкой, поэтому формат, с которым они вышли,
// обязан пережить смену провайдера за ним.
func (s *AddressSuggester) LegacySuggest(ctx context.Context, query string) ([]AutocompleteResult, error) {
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
	}

	suggestions, err := s.dadata.Suggest(ctx, query, 7)
	if err != nil {
		return nil, err
	}

	out := make([]AutocompleteResult, 0, len(suggestions))
	for _, a := range suggestions {
		// Старый клиент не может использовать адрес без номера дома: его
		// собственная проверка отвергает строку при отправке, поэтому предложение
		// такого адреса ведёт только в тупик.
		if !a.IsDeliverable() {
			continue
		}
		line := fmt.Sprintf("Россия, %s, %s, д. %s",
			strings.TrimSpace(a.City), strings.TrimSpace(a.Street), strings.TrimSpace(a.House))

		result := AutocompleteResult{Address: line, Display: line}
		if a.Lat != nil {
			result.Lat = *a.Lat
		}
		if a.Lon != nil {
			result.Lon = *a.Lon
		}
		out = append(out, result)
	}
	return out, nil
}

// parseLegacyCanonical восстанавливает части адреса, сохранённого одной
// строкой: уже установленные мобильные сборки всё ещё её шлют, а строки,
// сохранённые до появления структурных колонок, всё ещё её хранят. Всё
// выбранное из списка подсказок приходит уже разделённым, с частями от провайдера.
func parseLegacyCanonical(line string) Address {
	addr := Address{Value: strings.TrimSpace(line), Source: SourceLegacyText}

	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// Отбрасываем ведущую страну, которую старая каноническая форма всегда несла.
	if len(parts) > 0 && strings.EqualFold(parts[0], "Россия") {
		parts = parts[1:]
	}

	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "д. "):
			addr.House = strings.TrimSpace(strings.TrimPrefix(p, "д. "))
		case strings.HasPrefix(p, "кв. "):
			addr.Flat = strings.TrimSpace(strings.TrimPrefix(p, "кв. "))
		case addr.City == "":
			addr.City = p
		case addr.Street == "":
			addr.Street = p
		}
	}

	// «д. 12 кв. 5» приходит одним компонентом, когда квартиру дописали без
	// запятой, — именно так её собирала регистрация.
	if house, flat, found := strings.Cut(addr.House, " кв. "); found {
		addr.House = strings.TrimSpace(house)
		addr.Flat = strings.TrimSpace(flat)
	}
	return addr
}
