// Command reconcile checks that every stored balance still equals the sum of
// that user's ledger entries, and reports orders whose held amount contradicts
// their status.
//
// It exits with status 1 when the books do not balance, so it can be wired into
// a cron job or a deployment check.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

func main() {
	asJSON := flag.Bool("json", false, "print the full report as JSON")
	tolerance := flag.Float64("tolerance", 0.01, "difference to ignore, in currency units")
	flag.Parse()

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		env("DB_HOST", "localhost"), env("DB_PORT", "5432"),
		env("DB_USER", "healthlogin"), env("DB_PASSWORD", "healthlogin"),
		env("DB_NAME", "healthlogin"), env("DB_SSLMODE", "disable"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	for i := 0; i < 10; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	report, err := repository.NewReconciliationRepository(db).Reconcile(*tolerance)
	if err != nil {
		log.Fatalf("reconcile: %v", err)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatalf("encode report: %v", err)
		}
	} else {
		printReport(report)
	}

	if !report.OK() {
		os.Exit(1)
	}
}

func printReport(report *repository.ReconciliationReport) {
	fmt.Println(report.Summary())

	for _, t := range report.UnknownTypes {
		fmt.Printf("  unknown transaction type %q: the sign convention in repository.ledgerSigns needs updating\n", t)
	}
	for _, d := range report.Discrepancies {
		fmt.Printf("  user %s (%s, %s): balance %.2f, ledger %.2f, difference %+.2f over %d entries\n",
			d.UserID, d.Phone, d.Role, d.Balance, d.Ledger, d.Difference, d.Entries)
	}
	for _, a := range report.HoldAnomalies {
		fmt.Printf("  order %s (%s, hold %.2f): %s\n", a.OrderID, a.Status, a.HoldAmount, a.Reason)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
