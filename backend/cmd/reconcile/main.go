// Command reconcile проверяет, что каждый сохранённый баланс всё ещё равен
// сумме проводок этого пользователя, и сообщает о заказах, чья удержанная
// сумма противоречит их статусу.
//
// Завершается с кодом 1, когда книги не сходятся, — чтобы её можно было
// встроить в cron или в проверку деплоя.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"healthlogin/backend/dbconn"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

func main() {
	asJSON := flag.Bool("json", false, "print the full report as JSON")
	tolerance := flag.Float64("tolerance", 0.01, "difference to ignore, in rubles")
	flag.Parse()

	db, err := dbconn.OpenFromEnv(context.Background())
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	report, err := repository.NewReconciliationRepository(db).Reconcile(context.Background(), money.FromRubles(*tolerance))
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

	fmt.Printf("  users hold %s, platform accounts hold %s, difference %s\n",
		report.Books.UserTotal, report.Books.AccountTotal, report.Books.Difference)
	for _, a := range report.Books.Accounts {
		fmt.Printf("    %-10s %12s  %s\n", a.Code, a.Balance, a.Name)
	}
	if report.EscrowMismatch {
		fmt.Printf("  escrow holds %s but live orders account for %s (%s)\n",
			report.Books.EscrowHeld, report.Books.LiveOrderSum, report.Books.EscrowDrift)
	}

	for _, t := range report.UnknownTypes {
		fmt.Printf("  unknown transaction type %q: the sign convention in repository.ledgerSigns needs updating\n", t)
	}
	for _, d := range report.Discrepancies {
		fmt.Printf("  user %s (%s, %s): balance %s, ledger %s, difference %s over %d entries\n",
			d.UserID, d.Phone, d.Role, d.Balance, d.Ledger, d.Difference, d.Entries)
	}
	for _, a := range report.HoldAnomalies {
		fmt.Printf("  order %s (%s, hold %s): %s\n", a.OrderID, a.Status, a.HoldAmount, a.Reason)
	}
}
