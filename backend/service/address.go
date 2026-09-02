package service

import (
	"fmt"
	"strings"

	"healthlogin/backend/repository"
)

// Address — почтовый адрес, хранимый частями, а не одной строкой.
//
// Прежняя схема хранила адрес текстом и восстанавливала его составляющие
// регулярным выражением, и на клиенте, и на сервере. Тот разбор принимал только
// «Россия, Город, Улица, д. <цифры>», поэтому дом с корпусом или строением
// («12к1», «10 стр. 2»), да и любая подсказка, пришедшая без номера дома,
// отвергались уже после того, как пользователь выбрал её из списка. Хранение
// частей означает, что ничего не нужно выпарсивать обратно.
type Address struct {
	// Value — то, что читает человек: весь адрес одной строкой, включая квартиру.
	// Он выводится из частей и никогда не разбирается ради их восстановления.
	Value string `json:"value"`

	Region string `json:"region,omitempty"`
	City   string `json:"city,omitempty"`
	Street string `json:"street,omitempty"`
	// House хранит то, что дал провайдер, включая корпус и строение.
	House string `json:"house,omitempty"`
	Flat  string `json:"flat,omitempty"`

	// FiasID идентифицирует адрес в государственном адресном реестре. Это
	// устойчивый ключ: два написания одного адреса его разделяют.
	FiasID string `json:"fias_id,omitempty"`

	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`

	// Source фиксирует, какой провайдер это произвёл, чтобы вопрос в поддержку про
	// неверный адрес можно было проследить до его источника.
	Source string `json:"source,omitempty"`
}

// Источники адресов.
const (
	SourceDaData     = "dadata"
	SourceLegacyText = "legacy"
)

// IsDeliverable сообщает, называет ли адрес конкретное здание. Адрес,
// заканчивающийся улицей, нормально показывать, пока человек печатает, но
// доставить по нему нельзя, поэтому как адрес подачи он не принимается.
func (a Address) IsDeliverable() bool {
	return strings.TrimSpace(a.City) != "" &&
		strings.TrimSpace(a.Street) != "" &&
		strings.TrimSpace(a.House) != ""
}

// HasCoordinates сообщает, может ли адрес участвовать в подборе по расстоянию.
// Заказы сопоставляются с исполнителями по координатам, поэтому адрес без них
// для диспетчера невидим.
func (a Address) HasCoordinates() bool {
	return a.Lat != nil && a.Lon != nil
}

// WithFlat возвращает копию с квартирой и с перестроенным Value, включающим её.
// Провайдеры возвращают квартиру отдельно, когда пользователь выбирает здание,
// а не квартиру, и где-то эти двое должны соединиться.
func (a Address) WithFlat(flat string) Address {
	flat = strings.TrimSpace(flat)
	a.Flat = flat
	a.Value = a.Compose()
	return a
}

// Compose собирает однострочную форму из частей. Это единственное место, где
// строится отображаемая строка, поэтому каждый экран показывает адрес одинаково.
func (a Address) Compose() string {
	parts := make([]string, 0, 5)
	for _, p := range []string{a.City, a.Street} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if house := strings.TrimSpace(a.House); house != "" {
		parts = append(parts, "д. "+house)
	}
	if flat := strings.TrimSpace(a.Flat); flat != "" {
		parts = append(parts, "кв. "+flat)
	}

	if len(parts) == 0 {
		// Ничего структурного не уцелело; откатываемся к тому тексту, что есть, чтобы
		// легаси-адрес всё же показался, а не превратился в пустоту.
		return strings.TrimSpace(a.Value)
	}
	return strings.Join(parts, ", ")
}

// Validate сообщает, почему адрес нельзя использовать для подачи, словами, по
// которым человек может действовать.
func (a Address) Validate() error {
	if strings.TrimSpace(a.Value) == "" && !a.IsDeliverable() {
		return fmt.Errorf("укажите адрес")
	}
	if !a.IsDeliverable() {
		return fmt.Errorf("выберите адрес с номером дома из списка подсказок")
	}
	return nil
}

// ParseAddressLine восстанавливает части адреса, пришедшего одной строкой.
// Такие строки до сих пор порождают лишь двое: уже установленные мобильные
// сборки и строки, сохранённые до того, как части стали храниться. Всё
// выбранное из списка подсказок приходит уже разделённым.
func ParseAddressLine(line string) Address {
	return parseLegacyCanonical(line)
}

// ToRecord превращает адрес в строку, которую хранит репозиторий. Value
// пересобирается, а не принимается на веру, поэтому показываемая человеку
// строка всегда соответствует частям рядом — включая квартиру, которую раньше
// дописывал клиент и которая могла расходиться с сохранённым.
func (a Address) ToRecord() repository.Address {
	return repository.Address{
		Address: a.Compose(),
		Region:  strings.TrimSpace(a.Region),
		City:    strings.TrimSpace(a.City),
		Street:  strings.TrimSpace(a.Street),
		House:   strings.TrimSpace(a.House),
		Flat:    strings.TrimSpace(a.Flat),
		FiasID:  strings.TrimSpace(a.FiasID),
		Lat:     a.Lat,
		Lon:     a.Lon,
		Source:  a.Source,
	}
}

// AddressFromRecord восстанавливает рабочий адрес из сохранённой строки,
// заполняя части из отображаемой строки для записей старше их хранения.
func AddressFromRecord(rec repository.Address) Address {
	addr := Address{
		Value:  rec.Address,
		Region: rec.Region,
		City:   rec.City,
		Street: rec.Street,
		House:  rec.House,
		Flat:   rec.Flat,
		FiasID: rec.FiasID,
		Lat:    rec.Lat,
		Lon:    rec.Lon,
		Source: rec.Source,
	}
	if addr.City == "" && addr.Street == "" && addr.House == "" {
		parsed := parseLegacyCanonical(rec.Address)
		addr.Region, addr.City, addr.Street = parsed.Region, parsed.City, parsed.Street
		addr.House, addr.Flat = parsed.House, parsed.Flat
		if addr.Source == "" {
			addr.Source = SourceLegacyText
		}
	}
	return addr
}
