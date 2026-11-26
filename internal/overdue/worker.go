// Package overdue — фоновая проверка просроченных выдач.
// Демонстрирует параллелизм: тикер + пул горутин-воркеров + каналы + WaitGroup + context.
package overdue

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"library/internal/events"
	"library/internal/loans"
)

// Checker раз в interval ищет просрочки и рассылает события через пул воркеров.
type Checker struct {
	db       *pgxpool.Pool
	pub      events.Publisher
	log      *slog.Logger
	interval time.Duration
	workers  int
}

func NewChecker(db *pgxpool.Pool, pub events.Publisher, log *slog.Logger, interval time.Duration, workers int) *Checker {
	return &Checker{db: db, pub: pub, log: log.With("component", "overdue"), interval: interval, workers: workers}
}

// Run работает, пока не отменён ctx. Запускать в отдельной горутине: go checker.Run(ctx).
func (c *Checker) Run(ctx context.Context) {
	c.log.Info("проверка просрочек запущена", "interval", c.interval.String(), "workers", c.workers)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.log.Info("проверка просрочек остановлена")
			return
		case <-ticker.C:
			if err := c.CheckOnce(ctx); err != nil {
				c.log.Error("ошибка проверки", "err", err)
			}
		}
	}
}

// CheckOnce — один проход: находит новые просрочки и обрабатывает их параллельно.
func (c *Checker) CheckOnce(ctx context.Context) error {
	found, err := c.findOverdue(ctx)
	if err != nil || len(found) == 0 {
		return err
	}

	jobs := make(chan loans.Loan)
	var wg sync.WaitGroup
	var sent atomic.Int64 // счётчик, который безопасно увеличивать из разных горутин

	for i := 1; i <= c.workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for l := range jobs {
				if err := c.handle(ctx, l); err != nil {
					c.log.Error("не удалось обработать просрочку", "worker", workerID, "loan_id", l.ID, "err", err)
					continue
				}
				sent.Add(1)
				c.log.Info("просрочка обработана", "worker", workerID, "loan_id", l.ID)
			}
		}(i)
	}

	// Раздаём задачи воркерам; закрытие канала сообщает им, что задач больше нет.
	for _, l := range found {
		select {
		case jobs <- l:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()

	c.log.Info("проход завершён", "found", len(found), "sent", sent.Load())
	return nil
}

// handle отправляет событие и помечает выдачу, чтобы не уведомлять повторно.
func (c *Checker) handle(ctx context.Context, l loans.Loan) error {
	if err := c.pub.Publish(ctx, loans.ToEvent(events.LoanOverdue, l)); err != nil {
		return err
	}
	_, err := c.db.Exec(ctx, `UPDATE loans SET overdue_notified = TRUE WHERE id = $1`, l.ID)
	return err
}

func (c *Checker) findOverdue(ctx context.Context) ([]loans.Loan, error) {
	rows, err := c.db.Query(ctx, `
		SELECT l.id, l.book_id, b.title, l.reader_id, r.name, l.issued_at, l.due_at
		FROM loans l
		JOIN books b ON b.id = l.book_id
		JOIN readers r ON r.id = l.reader_id
		WHERE l.returned_at IS NULL AND l.due_at < now() AND NOT l.overdue_notified
		ORDER BY l.id
		LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []loans.Loan
	for rows.Next() {
		var l loans.Loan
		if err := rows.Scan(&l.ID, &l.BookID, &l.BookTitle, &l.ReaderID, &l.ReaderName, &l.IssuedAt, &l.DueAt); err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}
