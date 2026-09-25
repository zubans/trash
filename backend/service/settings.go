package service

import (
	"context"
	"strconv"
	"strings"
)

// Чтение system_settings.
//
// Правило одно на все типы и все места. Значение берётся, когда ключ есть и
// разбирается; отсутствующий, пустой или негодный ключ даёт умолчание. Ноль и
// отрицательное число — такие же значения, как любые другие: записанный «0» —
// это решение администратора, а не отсутствие настройки. Настройка, для
// которой ноль или минус бессмысленны (радиус, порог, длительность), читается
// через positive*-вариант, который такие значения считает незаданными.
//
// Раньше у каждого сервиса был свой ридер, и они расходились ровно в этом: где-то
// ноль означал «не задано», где-то — ноль.

// settingsGetter — срез SettingsRepository, которого хватает чтению.
type settingsGetter interface {
	GetSettings(ctx context.Context) (map[string]string, error)
}

// settingsMap — снимок настроек, прочитанный один раз на операцию. Операция,
// которой нужно несколько ключей (взятие заказа читает шесть), читает базу
// однажды, а не по разу на ключ.
type settingsMap map[string]string

// loadSettingsMap читает настройки. Без хранилища или при сбое чтения — пустая
// карта: каждое чтение по ключу тогда даёт умолчание, как если бы строки не было.
func loadSettingsMap(ctx context.Context, repo settingsGetter) settingsMap {
	if repo == nil {
		return settingsMap{}
	}
	settings, err := repo.GetSettings(ctx)
	if err != nil || settings == nil {
		return settingsMap{}
	}
	return settingsMap(settings)
}

func (m settingsMap) float(key string, def float64) float64 {
	v, ok := m[key]
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def
	}
	return f
}

// int принимает и «3», и «3.0»: настройки правятся руками, и дробная запись
// целого — не повод выключить лимит.
func (m settingsMap) int(key string, def int) int {
	v, ok := m[key]
	if !ok {
		return def
	}
	v = strings.TrimSpace(v)
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return int(f)
}

// bool понимает «1/0», «true/false», «yes/no», «on/off» без учёта регистра;
// всё остальное — умолчание.
func (m settingsMap) bool(key string, def bool) bool {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// positiveFloat — для величин, у которых ноль и минус означают «не задано».
func (m settingsMap) positiveFloat(key string, def float64) float64 {
	if v := m.float(key, 0); v > 0 {
		return v
	}
	return def
}

func (m settingsMap) positiveInt(key string, def int) int {
	if v := m.int(key, 0); v > 0 {
		return v
	}
	return def
}

// Одноразовые обёртки для мест, где нужен один ключ.

func settingFloat(ctx context.Context, repo settingsGetter, key string, def float64) float64 {
	return loadSettingsMap(ctx, repo).float(key, def)
}

func settingInt(ctx context.Context, repo settingsGetter, key string, def int) int {
	return loadSettingsMap(ctx, repo).int(key, def)
}

func settingBool(ctx context.Context, repo settingsGetter, key string, def bool) bool {
	return loadSettingsMap(ctx, repo).bool(key, def)
}
