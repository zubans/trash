package service

import "context"

// GeocodingResult — разрешённый адрес: координаты плюс каноническая строка,
// которой они принадлежат.
type GeocodingResult struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address"`
}

// AutocompleteResult — одна подсказка в легаси-форме автодополнения. Уже
// установленные мобильные сборки читают её из /geo/autocomplete и перепроверяют
// строку по собственному формату перед отправкой, поэтому форма обязана
// пережить то, что провайдером за ней теперь стала DaData.
type AutocompleteResult struct {
	Address string  `json:"address"`
	Display string  `json:"display"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

// AddressResolver превращает произвольную строку адреса в координаты. Это
// запасной путь для сценариев вне интерактивного поля подсказок: создание
// заказа и регистрация, когда клиент не прислал координат, эндпоинт
// /geo/geocode, оставленный для установленных клиентов, и воркер дозаполнения.
// Интерактивный путь его никогда не использует — выбранная подсказка уже несёт
// свои координаты.
type AddressResolver interface {
	Resolve(ctx context.Context, query string) (*GeocodingResult, error)
}
