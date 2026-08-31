package worker

import (
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func leaderTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("skipping database test: DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("cannot ping test db: %v", err)
	}
	return db
}

// Without a database the guard is a pass-through, so single-process
// deployments and unit tests are unaffected.
func TestLeaderWithoutDatabaseRunsTheJob(t *testing.T) {
	ran := false
	guard := NewLeader(nil).Guard("test")
	if err := guard(func() error { ran = true; return nil }); err != nil {
		t.Fatalf("guard returned %v", err)
	}
	if !ran {
		t.Error("job did not run without a database")
	}
}

func TestLeaderRunsTheJobAndPropagatesItsError(t *testing.T) {
	db := leaderTestDB(t)
	defer db.Close()

	guard := NewLeader(db).Guard("leader-test-runs")
	ran := false
	if err := guard(func() error { ran = true; return nil }); err != nil {
		t.Fatalf("guard returned %v", err)
	}
	if !ran {
		t.Fatal("job did not run while the lock was free")
	}

	// A job's own failure must reach the caller, which is what logs it.
	wantErr := sql.ErrNoRows
	if err := guard(func() error { return wantErr }); err != wantErr {
		t.Errorf("guard returned %v, want the job's error", err)
	}
}

// The point of the lock: while one process is inside the job, another must
// skip it rather than run the same work twice.
func TestLeaderExcludesConcurrentRuns(t *testing.T) {
	db := leaderTestDB(t)
	defer db.Close()

	leader := NewLeader(db)
	const job = "leader-test-exclusion"

	inside := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = leader.Guard(job)(func() error {
			close(inside)
			<-release
			return nil
		})
	}()

	select {
	case <-inside:
	case <-time.After(5 * time.Second):
		t.Fatal("the first job never started")
	}

	secondRan := false
	if err := leader.Guard(job)(func() error { secondRan = true; return nil }); err != nil {
		t.Fatalf("second guard returned %v", err)
	}
	if secondRan {
		t.Error("a second run went ahead while the lock was held")
	}

	close(release)
	wg.Wait()

	// And once the holder is done, the lock is free again — a lock that is
	// taken but never released would stop the job forever.
	afterRan := false
	if err := leader.Guard(job)(func() error { afterRan = true; return nil }); err != nil {
		t.Fatalf("third guard returned %v", err)
	}
	if !afterRan {
		t.Error("the lock was not released after the job finished")
	}
}

// Different jobs must not block each other.
func TestLeaderJobsAreIndependent(t *testing.T) {
	db := leaderTestDB(t)
	defer db.Close()

	leader := NewLeader(db)
	inside := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = leader.Guard("leader-test-job-a")(func() error {
			close(inside)
			<-release
			return nil
		})
	}()

	select {
	case <-inside:
	case <-time.After(5 * time.Second):
		t.Fatal("the first job never started")
	}

	otherRan := false
	if err := leader.Guard("leader-test-job-b")(func() error { otherRan = true; return nil }); err != nil {
		t.Fatalf("guard returned %v", err)
	}
	if !otherRan {
		t.Error("an unrelated job was blocked by another job's lock")
	}

	close(release)
	wg.Wait()
}
