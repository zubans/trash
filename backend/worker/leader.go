package worker

import (
	"context"
	"database/sql"
	"hash/fnv"
	"log"
)

// Leader выдаёт защиту по задачам поверх advisory-блокировок PostgreSQL, чтобы
// периодическая задача выполнялась на одном процессе за раз.
//
// Каждая фоновая задача здесь меняет состояние, которое должно измениться один
// раз: воркер SLA возвращает часть удержания, воркер смен закрывает смены и
// списывает штрафы за ранний уход, воркер подбора назначает заказы. Два
// процесса, выполняющие один тик, сделали бы каждое из этого дважды — два
// возврата, два штрафа, — из-за чего сервис сейчас нельзя запускать более чем в
// одной реплике. Защита заставляет второй процесс тик пропустить.
//
// Пропущенный тик — не ошибка и не повтор: задача идёт по таймеру, а процесс,
// держащий блокировку, только что сделал работу.
type Leader struct {
	db *sql.DB
}

// NewLeader создаёт Leader поверх заданной базы. Nil-база даёт Leader, чьи
// защиты просто выполняют свою задачу, — именно этого хотят тесты и любой
// однопроцессный деплой.
func NewLeader(db *sql.DB) *Leader {
	return &Leader{db: db}
}

// Guard возвращает функцию, которая выполняет задачу, держа advisory-блокировку
// для name, и пропускает её, когда блокировку держит другой процесс.
func (l *Leader) Guard(name string) func(func() error) error {
	if l == nil || l.db == nil {
		return func(job func() error) error { return job() }
	}
	key := lockKey(name)
	return func(job func() error) error {
		return l.run(name, key, job)
	}
}

func (l *Leader) run(name string, key int64, job func() error) error {
	ctx := context.Background()

	// Блокировку держит сессия, поэтому соединение надо закрепить на всю её
	// жизнь: взятые и отпущенные на случайном соединении из пула, разблокировка
	// могла бы попасть не в ту сессию, что и блокировка, и оставить задачу
	// заблокированной для всех процессов навсегда.
	conn, err := l.db.Conn(ctx)
	if err != nil {
		// Без соединения невозможно понять, выполняет ли эту задачу другой процесс,
		// а выполнение без защиты — ровно тот риск, ради устранения которого
		// блокировка и существует.
		log.Printf("[leader] %s: cannot acquire a connection, skipping this tick: %v", name, err)
		return nil
	}
	defer conn.Close()

	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		log.Printf("[leader] %s: cannot take the lock, skipping this tick: %v", name, err)
		return nil
	}
	if !acquired {
		// Прямо сейчас эту задачу выполняет другой процесс. Это нормально и
		// намеренно молча: с несколькими репликами иначе логировался бы каждый тик.
		return nil
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			// Закрытие соединения завершает сессию и отпускает блокировку, поэтому
			// это отчёт, а не утечка.
			log.Printf("[leader] %s: failed to release the lock: %v", name, err)
		}
	}()

	return job()
}

// lockKey выводит ключ advisory-блокировки из имени задачи, чтобы ключи не
// могли разъехаться с задачами, которые они защищают, как разъехался бы
// поддерживаемый вручную список магических чисел.
func lockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("healthlogin.worker." + name))
	// Ключи advisory-блокировок — знаковые 64-битные; знак хеша не важен,
	// пока одно имя всегда отображается в один ключ.
	return int64(h.Sum64())
}
