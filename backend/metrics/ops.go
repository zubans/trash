package metrics

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
)

// OpsHandlers — привилегированные действия, которые слушатель метрик откроет,
// если их передать. Они живут здесь, а не на роутере API, по той же причине,
// что и /metrics: этот слушатель достижим только из сети compose, nginx его не
// проксирует, а порт не публикуется, поэтому ничто на нём не отделено от
// интернета одним неверно настроенным маршрутом.
type OpsHandlers struct {
	// Secret закрывает каждый ops-маршрут сравнением за константное время. Пустое
	// значение полностью выключает маршруты — общий секрет, который так и не
	// настроили, не должен превращаться в открытую дверь.
	Secret string

	// Reconcile выполняет проверку книг по требованию и возвращает сводку,
	// пригодную для сериализации в JSON. Ожидается, что он сам публикует датчики сверки.
	Reconcile func() (any, error)
}

func (o OpsHandlers) enabled() bool { return o.Secret != "" && o.Reconcile != nil }

// authorize сравнивает общий секрет за константное время. Заголовок проверяется
// раньше всего прочего, и ответ никогда не говорит, какая половина неверна.
func (o OpsHandlers) authorize(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	provided := r.Header.Get("X-Ops-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(o.Secret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (o OpsHandlers) register(mux *http.ServeMux) {
	if !o.enabled() {
		log.Printf("[metrics] ops routes disabled: OPS_KEY is not set")
		return
	}

	mux.HandleFunc("/internal/reconcile", func(w http.ResponseWriter, r *http.Request) {
		if !o.authorize(w, r) {
			return
		}
		log.Printf("[ops] reconciliation requested")
		report, err := o.Reconcile()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	})

	log.Printf("[metrics] ops routes enabled at /internal/*")
}
