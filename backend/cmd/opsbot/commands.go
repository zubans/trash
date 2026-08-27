package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// runReconcile asks the backend to run the books check. The bot deliberately
// does not run it against the database itself: the reconciliation gauges live
// in the backend process, and a pass that did not update them would leave the
// screen and the alert disagreeing about the same money.
func (b *bot) runReconcile(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.reconcileURL, nil)
	if err != nil {
		return "❌ " + escape(err.Error())
	}
	req.Header.Set("X-Ops-Key", b.cfg.opsKey)

	resp, err := b.http.Do(req)
	if err != nil {
		return "❌ Бэкенд недоступен: " + escape(err.Error())
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("❌ Сверка не отработала (HTTP %d)\n<pre>%s</pre>", resp.StatusCode, escape(string(bytes.TrimSpace(body))))
	}

	var report struct {
		OK            bool   `json:"ok"`
		Summary       string `json:"summary"`
		UsersChecked  int    `json:"users_checked"`
		Discrepancies []struct {
			Phone      string `json:"phone"`
			Difference string `json:"difference"`
		} `json:"discrepancies"`
		HoldAnomalies []struct {
			OrderID    string `json:"order_id"`
			Status     string `json:"status"`
			HoldAmount string `json:"hold_amount"`
		} `json:"hold_anomalies"`
		Books struct {
			UserTotal    string `json:"user_total"`
			AccountTotal string `json:"account_total"`
			Difference   string `json:"difference"`
			EscrowDrift  string `json:"escrow_drift"`
		} `json:"books"`
		BooksOpen      bool `json:"books_open"`
		EscrowMismatch bool `json:"escrow_mismatch"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return "❌ Ответ сверки не разобран: " + escape(err.Error())
	}

	var out strings.Builder
	if report.OK {
		out.WriteString("✅ <b>Сверка: книги сходятся</b>\n")
	} else {
		out.WriteString("🔴 <b>Сверка: найдено расхождение</b>\n")
	}
	fmt.Fprintf(&out, "<i>%s</i>\n\n", escape(report.Summary))
	fmt.Fprintf(&out, "У пользователей: <b>%s</b>\nНа счетах: <b>%s</b>\nРазница: <b>%s</b>\n",
		escape(report.Books.UserTotal), escape(report.Books.AccountTotal), escape(report.Books.Difference))
	if report.EscrowMismatch {
		fmt.Fprintf(&out, "Расхождение эскроу: <b>%s</b>\n", escape(report.Books.EscrowDrift))
	}

	if n := len(report.Discrepancies); n > 0 {
		fmt.Fprintf(&out, "\n<b>Балансы вне журнала (%d):</b>\n", n)
		for i, d := range report.Discrepancies {
			if i == 5 {
				fmt.Fprintf(&out, "…и ещё %d\n", n-i)
				break
			}
			fmt.Fprintf(&out, "• %s: %s\n", escape(d.Phone), escape(d.Difference))
		}
	}
	if n := len(report.HoldAnomalies); n > 0 {
		fmt.Fprintf(&out, "\n<b>Зависшие удержания: %d</b>\n", n)
	}
	return out.String()
}

// checkMetrics scrapes the targets and reports the handful of numbers worth
// waking up for, rather than the whole exposition.
func (b *bot) checkMetrics(ctx context.Context) string {
	var out strings.Builder
	out.WriteString("<b>Метрики</b>\n\n")

	for _, target := range b.cfg.metricTargets {
		samples, err := scrape(ctx, b.http, target.url)
		if err != nil {
			fmt.Fprintf(&out, "🔴 <b>%s</b> — не отвечает\n<i>%s</i>\n\n", escape(target.name), escape(err.Error()))
			continue
		}
		fmt.Fprintf(&out, "🟢 <b>%s</b>\n", escape(target.name))

		names := make([]string, 0, len(target.watch))
		names = append(names, target.watch...)
		sort.Strings(names)
		for _, name := range names {
			value, ok := samples[name]
			if !ok {
				fmt.Fprintf(&out, "  • %s: <i>нет данных</i>\n", escape(prettyMetric(name)))
				continue
			}
			fmt.Fprintf(&out, "  • %s: <b>%s</b>\n", escape(prettyMetric(name)), escape(formatValue(name, value)))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// restart runs the project's own restart target. Deliberately a fixed argument
// list with no shell and nothing taken from the message: the only thing a
// command can choose here is whether to run it, never what runs.
func (b *bot) restart(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.restartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "make", "-C", b.cfg.projectDir, "restart")
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	started := time.Now()
	err := cmd.Run()
	took := time.Since(started).Round(time.Second)

	tail := lastLines(combined.String(), 25)
	if err != nil {
		return fmt.Sprintf("🔴 <b>make restart упал</b> за %s\n<pre>%s</pre>", took, escape(tail))
	}
	return fmt.Sprintf("✅ <b>Сервисы перезапущены</b> за %s\n<pre>%s</pre>", took, escape(tail))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
