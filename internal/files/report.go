package files

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"library/internal/loans"
)

// LoanLister — источник данных для отчёта (реализует loans.Repository).
type LoanLister interface {
	List(ctx context.Context, status string) ([]loans.Loan, error)
}

// Reporter пишет текстовые отчёты о выдачах в папку dir.
type Reporter struct {
	loans LoanLister
	dir   string
}

func NewReporter(l LoanLister, dir string) *Reporter { return &Reporter{loans: l, dir: dir} }

// WriteLoansReport создаёт файл отчёта и возвращает путь к нему.
func (rp *Reporter) WriteLoansReport(ctx context.Context) (string, error) {
	list, err := rp.loans.List(ctx, "active")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(rp.dir, 0o755); err != nil {
		return "", err
	}

	now := time.Now()
	path := filepath.Join(rp.dir, "loans-"+now.Format("20060102-150405")+".txt")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// bufio.Writer копит текст в памяти и пишет на диск крупными кусками.
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "ОТЧЁТ О КНИГАХ НА РУКАХ\nСформирован: %s\n\n", now.Format("02.01.2006 15:04:05"))
	fmt.Fprintf(w, "%-5s %-30s %-25s %-12s %s\n", "ID", "Книга", "Читатель", "Вернуть до", "Статус")
	overdue := 0
	for _, l := range list {
		status := "в срок"
		if l.DueAt.Before(now) {
			status = "ПРОСРОЧЕНО"
			overdue++
		}
		fmt.Fprintf(w, "%-5d %-30s %-25s %-12s %s\n",
			l.ID, cut(l.BookTitle, 30), cut(l.ReaderName, 25), l.DueAt.Format("02.01.2006"), status)
	}
	fmt.Fprintf(w, "\nВсего на руках: %d, из них просрочено: %d\n", len(list), overdue)
	if err := w.Flush(); err != nil {
		return "", err
	}
	return path, nil
}

// cut обрезает строку по символам (а не байтам), чтобы не ломать кириллицу.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
