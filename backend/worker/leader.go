package worker

import (
	"context"
	"database/sql"
	"hash/fnv"
	"log"
)

// Leader hands out per-job guards backed by PostgreSQL advisory locks, so that
// a periodic job runs on one process at a time.
//
// Every background job here changes state that must happen once: the SLA worker
// refunds part of a hold, the shift worker closes shifts and charges early-exit
// penalties, the matching worker assigns orders. Two processes running the same
// tick would do each of those twice — two refunds, two penalties — which is why
// the service cannot currently be run with more than one replica. A guard makes
// the second process skip the tick instead.
//
// A skipped tick is not an error and not a retry: the job runs on a timer, and
// whichever process holds the lock has just done the work.
type Leader struct {
	db *sql.DB
}

// NewLeader creates a Leader over the given database. A nil database yields a
// Leader whose guards simply run their job, which is what the tests and any
// single-process deployment want.
func NewLeader(db *sql.DB) *Leader {
	return &Leader{db: db}
}

// Guard returns a function that runs a job while holding the advisory lock for
// name, and skips it when another process holds it.
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

	// The lock is held by a session, so the connection must be pinned for its
	// whole life: taken and released on a pooled connection at random, the
	// unlock could land on a different session than the lock did and leave the
	// job blocked for every process, forever.
	conn, err := l.db.Conn(ctx)
	if err != nil {
		// Without a connection there is no way to tell whether another process
		// is running this job, and running it unguarded is exactly the risk the
		// lock exists to remove.
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
		// Another process is running this job right now. Normal, and silent by
		// design: with several replicas this would otherwise log every tick.
		return nil
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			// Closing the connection ends the session and releases the lock, so
			// this is a report, not a leak.
			log.Printf("[leader] %s: failed to release the lock: %v", name, err)
		}
	}()

	return job()
}

// lockKey derives the advisory lock key from the job name, so the keys cannot
// drift apart from the jobs they protect the way a hand-maintained list of
// magic numbers would.
func lockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("healthlogin.worker." + name))
	// Advisory lock keys are signed 64-bit; the sign of the hash is irrelevant
	// as long as one name always maps to one key.
	return int64(h.Sum64())
}
