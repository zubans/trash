package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// scrape читает выкладку Prometheus и возвращает сумму по каждому семейству
// метрик, игнорируя лейблы. Бот сообщает ключевые числа — «сколько эндпоинтов
// работает», «сходятся ли книги», — и для них ответ это итог; всё, чему нужны
// лейблы, место на дашборде.
func scrape(ctx context.Context, client *http.Client, url string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	samples := make(map[string]float64)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := parseSample(line)
		if !ok {
			continue
		}
		samples[name] += value
	}
	return samples, scanner.Err()
}

func parseSample(line string) (string, float64, bool) {
	// name{labels} value  |  name value
	sep := strings.LastIndex(line, " ")
	if sep < 0 {
		return "", 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(line[sep+1:]), 64)
	if err != nil {
		return "", 0, false
	}
	name := strings.TrimSpace(line[:sep])
	if brace := strings.Index(name, "{"); brace >= 0 {
		name = name[:brace]
	}
	if name == "" {
		return "", 0, false
	}
	return name, value, true
}

// prettyMetric и formatValue превращают имя метрики в нечто читаемое в окне
// чата. Бот, отвечающий сырыми именами метрик, — это худший дашборд, а не
// лучший.
var metricLabels = map[string]string{
	"healthlogin_reconcile_ok":                         "Книги сходятся",
	"healthlogin_reconcile_drift_rubles":               "Расхождение, ₽",
	"healthlogin_orders_searching":                     "Заказов в поиске",
	"healthlogin_shifts_active":                        "Исполнителей на смене",
	"healthlogin_http_requests_in_flight":              "Запросов в обработке",
	"healthlogin_chat_websocket_connections":           "Открытых чатов",
	"healthlogin_reconcile_last_run_timestamp_seconds": "Последняя сверка",
}

func prettyMetric(name string) string {
	if label, ok := metricLabels[name]; ok {
		return label
	}
	return name
}

func formatValue(name string, value float64) string {
	switch {
	case strings.HasSuffix(name, "_timestamp_seconds"):
		if value <= 0 {
			return "не отрабатывала"
		}
		return time.Since(time.Unix(int64(value), 0)).Round(time.Second).String() + " назад"
	case name == "healthlogin_reconcile_ok":
		if value == 1 {
			return "да"
		}
		// NaN означает «ни один проход ещё не завершился», а это не то же самое, что «нет».
		if value != value {
			return "ещё не проверялось"
		}
		return "НЕТ"
	default:
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
}
