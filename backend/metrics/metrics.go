// Package metrics — единственное место, где процесс описывает себя для
// Prometheus.
//
// Всё живёт в приватном реестре, а не в дефолтном: дефолтный реестр — это
// глобальное изменяемое состояние, в которое может писать любая подключённая
// библиотека, а /metrics — почти публичная поверхность, которую хочется
// держать предсказуемой. Реестр открыт только на выделенном слушателе,
// который запускает Serve, поэтому сбор метрик никогда не делит порт, цепочку
// middleware или ограничитель частоты с API.
//
// Пакет намеренно принимает только примитивы (строки, float, bool), чтобы
// каждый слой — обработчики, сервисы, воркеры, репозитории — мог импортировать
// его, не таща за собой цикл зависимостей.
package metrics

import (
	"database/sql"
	"math"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Namespace предваряет каждую метрику, чтобы ряды легко отделялись от
// экспортеров, которые собирает тот же Prometheus.
const Namespace = "healthlogin"

// Registry хранит все коллекторы, которые экспортирует этот процесс.
var Registry = prometheus.NewRegistry()

// Бакеты подобраны под API, отвечающий за миллисекунды, но иногда ждущий
// DaData или базу. Дефолтные клиентские бакеты заканчиваются на 10 с, что
// скрывает ровно тот хвост, который нам интересен.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

var (
	// ---- HTTP -----------------------------------------------------------

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "http_requests_total",
		Help:      "HTTP requests served, by route pattern and status class.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "http_request_duration_seconds",
		Help:      "Time to serve an HTTP request, by route pattern.",
		Buckets:   latencyBuckets,
	}, []string{"method", "route"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "http_requests_in_flight",
		Help:      "HTTP requests currently being served.",
	})

	// ---- Аутентификация --------------------------------------------------

	authEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "auth_events_total",
		Help:      "Authentication events by kind and outcome (login, register, refresh, password reset).",
	}, []string{"event", "result"})

	// ---- Бизнес ----------------------------------------------------------

	orderEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "order_events_total",
		Help:      "Order lifecycle transitions.",
	}, []string{"event"})

	bidEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "bid_events_total",
		Help:      "Auction events (bid placed, bid accepted).",
	}, []string{"event"})

	shiftEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "shift_events_total",
		Help:      "Executor shift events (started, ended, ended early, auto-closed, geofence violation).",
	}, []string{"event"})

	matchingAssignments = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "matching_assignments_total",
		Help:      "Assignment attempts by the background matcher, by outcome.",
	}, []string{"result"})

	// Бесплатные услуги поддерживаются, и заказ такой услуги вообще не двигает
	// денег: ни удержания, ни проводки, ничего в счётчиках сумм. Без собственного
	// счётчика такой заказ невидим, и «сегодня нет выручки» становится неотличимо
	// от «сегодня все заказы были бесплатными» — а это разница между тихим днём и
	// неверно назначенной ценой.
	ordersFree = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "orders_free_created_total",
		Help:      "Orders created for a service that costs nothing, so no money was held.",
	})

	// Глубина маркетплейса, обновляемая подборщиком на каждом проходе. Заказы в
	// очереди при отсутствии людей на смене — тот режим отказа, который в счётчиках
	// запросов выглядит нормально: всё отвечает 200, а работа не делается.
	ordersSearching = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "orders_searching",
		Help:      "Orders currently waiting for an executor.",
	})

	shiftsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "shifts_active",
		Help:      "Executors currently on shift.",
	})

	// ---- Деньги ----------------------------------------------------------
	//
	// Суммы считаются в рублях, а не в копейках, чтобы дашборд показывал их без
	// деления, и это счётчики, потому что каждая проводка — абсолютное движение:
	// направление уже закодировано в типе.

	ledgerEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_entries_total",
		Help:      "Ledger entries written, by transaction type and system account.",
	}, []string{"type", "account"})

	ledgerAmount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_amount_rubles_total",
		Help:      "Absolute amount moved through the ledger, in rubles.",
	}, []string{"type", "account"})

	ledgerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ledger_errors_total",
		Help:      "Ledger operations that failed, by operation.",
	}, []string{"op"})

	// ---- Сверка ----------------------------------------------------------
	//
	// Ночная проверка книг и так громко пишет в лог; эти датчики делают те же
	// факты пригодными для алертов, а не только для grep.

	reconcileOK = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_ok",
		Help:      "1 when the last reconciliation pass found no drift, 0 when it found drift, NaN before any pass has completed.",
	})

	reconcileLastRun = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_last_run_timestamp_seconds",
		Help:      "Unix time of the last completed reconciliation pass.",
	})

	reconcileFindings = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_findings",
		Help:      "Number of findings in the last reconciliation pass, by kind.",
	}, []string{"kind"})

	reconcileDrift = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "reconcile_drift_rubles",
		Help:      "Signed drift found by the last reconciliation pass, in rubles.",
	}, []string{"kind"})

	reconcileFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "reconcile_failures_total",
		Help:      "Reconciliation passes that could not complete.",
	})

	// ---- Фоновые воркеры -------------------------------------------------

	workerRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "worker_runs_total",
		Help:      "Background worker passes, by worker and outcome.",
	}, []string{"worker", "result"})

	workerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "worker_run_duration_seconds",
		Help:      "Duration of one background worker pass.",
		Buckets:   latencyBuckets,
	}, []string{"worker"})

	workerLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "worker_last_success_timestamp_seconds",
		Help:      "Unix time of the last successful pass of a background worker.",
	}, []string{"worker"})

	// ---- Внешние зависимости ---------------------------------------------

	upstreamRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "upstream_requests_total",
		Help:      "Calls to an external dependency (DaData, geocoder), by outcome.",
	}, []string{"upstream", "operation", "result"})

	upstreamDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "upstream_request_duration_seconds",
		Help:      "Latency of a call to an external dependency.",
		Buckets:   latencyBuckets,
	}, []string{"upstream", "operation"})

	mailSends = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "mail_sends_total",
		Help:      "System mail handed to the SMTP submission server, by kind and outcome.",
	}, []string{"kind", "result"})

	mailDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "mail_send_duration_seconds",
		Help:      "Time spent submitting one message to the mail server.",
		Buckets:   latencyBuckets,
	}, []string{"kind"})

	// ---- Чат --------------------------------------------------------------

	chatConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "chat_websocket_connections",
		Help:      "Open chat WebSocket connections.",
	}, []string{"kind"})

	chatMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "chat_messages_total",
		Help:      "Chat messages accepted by the server, by conversation kind.",
	}, []string{"kind"})

	// ---- Поведения услуг ---------------------------------------------------

	behaviorHookErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "behavior_hook_errors_total",
		Help:      "Behaviour script hooks that failed to run, by behaviour and hook. Every one of these is a service gate that failed closed.",
	}, []string{"behavior", "hook"})

	behaviorEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "behavior_events_total",
		Help:      "Domain events handed to the behaviour dispatcher, by outcome.",
	}, []string{"event", "result"})

	behaviorEffects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "behavior_effects_total",
		Help:      "Effects a behaviour asked for, by kind and outcome (applied, duplicate, refused).",
	}, []string{"kind", "result"})

	behaviorBacklog = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "behavior_events_pending",
		Help:      "Unprocessed domain events. A backlog that grows means rewards and auto-completions are not happening.",
	})
)

func init() {
	// Датчик стартует с нуля, а для reconcile_ok ноль — это конкретное громкое
	// утверждение: книги не сходятся. Пока проход по-настоящему не завершился,
	// такого свидетельства нет — проход, который не смог выполниться, ничего не
	// доказывает, — поэтому стартовое значение NaN, с которым не совпадает ни одно
	// сравнение. Без этого сверка, упавшая с первой попытки, будила человека из-за
	// пропавших денег, которые никуда не пропадали, а канал алертов, кричащий
	// «волки» про деньги, хуже, чем полное его отсутствие.
	reconcileOK.Set(math.NaN())

	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, httpInFlight,
		authEvents,
		orderEvents, bidEvents, shiftEvents, matchingAssignments,
		ordersSearching, shiftsActive, ordersFree,
		ledgerEntries, ledgerAmount, ledgerErrors,
		reconcileOK, reconcileLastRun, reconcileFindings, reconcileDrift, reconcileFailures,
		workerRuns, workerDuration, workerLastSuccess,
		upstreamRequests, upstreamDuration,
		mailSends, mailDuration,
		chatConnections, chatMessages,
		behaviorHookErrors, behaviorEvents, behaviorEffects, behaviorBacklog,
	)
}

// BehaviorHookError считает хук, который не удалось вычислить. Проверка,
// которую он охраняет, закрылась, поэтому это авария услуги на этом узле, а не
// диагностическая мелочь.
func BehaviorHookError(behavior, hook string) {
	behaviorHookErrors.WithLabelValues(behavior, hook).Inc()
}

// BehaviorEvent считает одно отправленное доменное событие: «processed»,
// «failed» или «skipped» (ни одному поведению оно не было интересно).
func BehaviorEvent(event, result string) { behaviorEvents.WithLabelValues(event, result).Inc() }

// BehaviorEffect считает один эффект: «applied», «duplicate» (ключ
// идемпотентности уже использован) или «refused» (проверка в ядре отказала).
func BehaviorEffect(kind, result string) { behaviorEffects.WithLabelValues(kind, result).Inc() }

// SetBehaviorBacklog публикует число необработанных доменных событий.
func SetBehaviorBacklog(pending int) { behaviorBacklog.Set(float64(pending)) }

// SetBuildInfo публикует работающую версию как датчик с константным значением 1
// — привычный способ сделать вопрос «что за сборка?» решаемым с дашборда.
func SetBuildInfo(version, commit string) {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "build_info",
		Help:      "Build metadata of the running backend; always 1.",
	}, []string{"version", "commit"})
	if err := Registry.Register(g); err != nil {
		return
	}
	g.WithLabelValues(version, commit).Set(1)
}

// RegisterDB экспортирует счётчики пула соединений. Пул, постоянно упирающийся
// в MaxOpenConnections с очередью запросов позади, снаружи выглядит ровно как
// медленная база.
func RegisterDB(db *sql.DB, name string) {
	_ = Registry.Register(collectors.NewDBStatsCollector(db, name))
}

// ---- Помощники записи --------------------------------------------------
//
// Каждый помощник безопасен как no-op: инструментирование никогда не должно
// быть причиной падения запроса, поэтому ничего здесь не возвращает ошибку.

// ObserveHTTP записывает один обслуженный запрос. route — это шаблон маршрута
// chi, а не сырой путь, чтобы персональные URL не взрывали кардинальность лейблов.
func ObserveHTTP(method, route string, status int, d time.Duration) {
	code := strconv.Itoa(status)
	httpRequests.WithLabelValues(method, route, code).Inc()
	httpDuration.WithLabelValues(method, route).Observe(d.Seconds())
}

// IncInFlight и DecInFlight обрамляют запрос.
func IncInFlight() { httpInFlight.Inc() }
func DecInFlight() { httpInFlight.Dec() }

// AuthEvent записывает попытку аутентификации. result — «ok» или «denied».
func AuthEvent(event, result string) { authEvents.WithLabelValues(event, result).Inc() }

// OrderEvent записывает переход в жизненном цикле заказа.
func OrderEvent(event string) { orderEvents.WithLabelValues(event).Inc() }

// OrderCreatedFree записывает заказ, ничего не удержавший, потому что услуга
// бесплатна. Он считается в дополнение к обычному событию «created», а не
// вместо него: заказ настоящий, просто он не двигает денег.
func OrderCreatedFree() { ordersFree.Inc() }

// BidEvent записывает событие аукциона.
func BidEvent(event string) { bidEvents.WithLabelValues(event).Inc() }

// ShiftEvent записывает событие смены.
func ShiftEvent(event string) { shiftEvents.WithLabelValues(event).Inc() }

// MatchingAssignment записывает одну попытку назначения подборщиком.
// result — «assigned» или «error».
func MatchingAssignment(result string) { matchingAssignments.WithLabelValues(result).Inc() }

// SetMarketplaceDepth публикует сторону спроса и предложения такой, какой её
// увидел последний проход подборщика.
func SetMarketplaceDepth(searchingOrders, activeShifts int) {
	ordersSearching.Set(float64(searchingOrders))
	shiftsActive.Set(float64(activeShifts))
}

// LedgerEntry записывает одну сделанную проводку. rubles — абсолютная сумма.
func LedgerEntry(kind, account string, rubles float64) {
	ledgerEntries.WithLabelValues(kind, account).Inc()
	if rubles < 0 {
		rubles = -rubles
	}
	ledgerAmount.WithLabelValues(kind, account).Add(rubles)
}

// LedgerError записывает неудавшуюся операцию реестра.
func LedgerError(op string) { ledgerErrors.WithLabelValues(op).Inc() }

// ReconcileReport публикует исход прохода сверки.
func ReconcileReport(ok bool, discrepancies, holdAnomalies, unknownTypes int, booksDiffRubles, escrowDriftRubles float64) {
	if ok {
		reconcileOK.Set(1)
	} else {
		reconcileOK.Set(0)
	}
	reconcileFindings.WithLabelValues("balance_discrepancies").Set(float64(discrepancies))
	reconcileFindings.WithLabelValues("hold_anomalies").Set(float64(holdAnomalies))
	reconcileFindings.WithLabelValues("unknown_transaction_types").Set(float64(unknownTypes))
	reconcileDrift.WithLabelValues("books").Set(booksDiffRubles)
	reconcileDrift.WithLabelValues("escrow").Set(escrowDriftRubles)
	reconcileLastRun.SetToCurrentTime()
}

// ReconcileFailed записывает проход, который не смог завершиться. Датчики
// намеренно сохраняют прежние значения: неудавшийся проход не доказывает, что с
// книгами всё хорошо, а обнуление читалось бы ровно так.
func ReconcileFailed() { reconcileFailures.Inc() }

// WorkerRun записывает один фоновый проход. Вызывайте его через defer, с
// ошибкой, которую вернул проход.
func WorkerRun(worker string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	} else {
		workerLastSuccess.WithLabelValues(worker).SetToCurrentTime()
	}
	workerRuns.WithLabelValues(worker, result).Inc()
	workerDuration.WithLabelValues(worker).Observe(d.Seconds())
}

// UpstreamCall записывает вызов внешней зависимости.
func UpstreamCall(upstream, operation string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	upstreamRequests.WithLabelValues(upstream, operation, result).Inc()
	upstreamDuration.WithLabelValues(upstream, operation).Observe(d.Seconds())
}

// UpstreamResult записывает вызов, исход которого детальнее, чем ok/error, —
// например попадание в кэш или отказ по квоте.
func UpstreamResult(upstream, operation, result string, d time.Duration) {
	upstreamRequests.WithLabelValues(upstream, operation, result).Inc()
	upstreamDuration.WithLabelValues(upstream, operation).Observe(d.Seconds())
}

// MailSend записывает одну попытку отправки.
func MailSend(kind string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	mailSends.WithLabelValues(kind, result).Inc()
	mailDuration.WithLabelValues(kind).Observe(d.Seconds())
}

// ChatConnected и ChatDisconnected обрамляют сессию WebSocket.
func ChatConnected(kind string)    { chatConnections.WithLabelValues(kind).Inc() }
func ChatDisconnected(kind string) { chatConnections.WithLabelValues(kind).Dec() }

// ChatMessage записывает принятое сообщение.
func ChatMessage(kind string) { chatMessages.WithLabelValues(kind).Inc() }

// TrackWorker выполняет один проход воркера и записывает его длительность и
// исход. Ошибка прохода возвращается нетронутой, поэтому цикл читается как прежде:
//
//	if err := metrics.TrackWorker("auction", w.CheckExpiredAuctions); err != nil {
func TrackWorker(worker string, fn func() error) error {
	started := time.Now()
	err := fn()
	WorkerRun(worker, time.Since(started), err)
	return err
}
