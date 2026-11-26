// Package loans — выдача и возврат книг. После каждой операции отправляет событие в брокер.
package loans

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"library/internal/events"
	"library/pkg/httpx"
)

// Loan — факт выдачи книги читателю.
type Loan struct {
	ID         int64      `json:"id"`
	BookID     int64      `json:"book_id"`
	BookTitle  string     `json:"book_title"`
	ReaderID   int64      `json:"reader_id"`
	ReaderName string     `json:"reader_name"`
	IssuedAt   time.Time  `json:"issued_at"`
	DueAt      time.Time  `json:"due_at"`
	ReturnedAt *time.Time `json:"returned_at"` // null, пока книга не возвращена
}

// IssueInput — запрос на выдачу книги.
type IssueInput struct {
	BookID   int64 `json:"book_id"`
	ReaderID int64 `json:"reader_id"`
	Days     *int  `json:"days"` // на сколько дней (по умолчанию 14; 0 — для демонстрации просрочки)
}

var (
	ErrNotFound       = errors.New("выдача не найдена")
	ErrBookNotFound   = errors.New("книга не найдена")
	ErrReaderNotFound = errors.New("читатель не найден")
	ErrNoCopies       = errors.New("нет свободных экземпляров")
	ErrAlreadyClosed  = errors.New("книга уже возвращена")
)

// Repository — SQL-запросы к таблице loans.
type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const selectLoan = `
	SELECT l.id, l.book_id, b.title, l.reader_id, r.name, l.issued_at, l.due_at, l.returned_at
	FROM loans l
	JOIN books b ON b.id = l.book_id
	JOIN readers r ON r.id = l.reader_id`

func scan(row pgx.Row) (Loan, error) {
	var l Loan
	err := row.Scan(&l.ID, &l.BookID, &l.BookTitle, &l.ReaderID, &l.ReaderName, &l.IssuedAt, &l.DueAt, &l.ReturnedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

// List возвращает выдачи. status: "" — все, "active" — на руках, "overdue" — просроченные.
func (r *Repository) List(ctx context.Context, status string) ([]Loan, error) {
	where := ""
	switch status {
	case "active":
		where = " WHERE l.returned_at IS NULL"
	case "overdue":
		where = " WHERE l.returned_at IS NULL AND l.due_at < now()"
	}
	rows, err := r.db.Query(ctx, selectLoan+where+" ORDER BY l.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Loan{}
	for rows.Next() {
		l, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, l)
	}
	return list, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Loan, error) {
	return scan(r.db.QueryRow(ctx, selectLoan+" WHERE l.id = $1", id))
}

// Issue выдаёт книгу в транзакции: уменьшает available и создаёт запись о выдаче.
func (r *Repository) Issue(ctx context.Context, bookID, readerID int64, days int) (Loan, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Loan{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM readers WHERE id = $1)`, readerID).Scan(&exists); err != nil {
		return Loan{}, err
	}
	if !exists {
		return Loan{}, ErrReaderNotFound
	}

	// FOR UPDATE блокирует строку книги, чтобы два параллельных запроса не выдали последний экземпляр дважды.
	var available int
	err = tx.QueryRow(ctx, `SELECT available FROM books WHERE id = $1 FOR UPDATE`, bookID).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return Loan{}, ErrBookNotFound
	}
	if err != nil {
		return Loan{}, err
	}
	if available <= 0 {
		return Loan{}, ErrNoCopies
	}

	if _, err := tx.Exec(ctx, `UPDATE books SET available = available - 1 WHERE id = $1`, bookID); err != nil {
		return Loan{}, err
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO loans (book_id, reader_id, due_at) VALUES ($1, $2, now() + make_interval(days => $3)) RETURNING id`,
		bookID, readerID, days).Scan(&id)
	if err != nil {
		return Loan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Loan{}, err
	}
	return r.Get(ctx, id)
}

// Return отмечает возврат и возвращает экземпляр на полку.
func (r *Repository) Return(ctx context.Context, id int64) (Loan, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Loan{}, err
	}
	defer tx.Rollback(ctx)

	var bookID int64
	var returnedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT book_id, returned_at FROM loans WHERE id = $1 FOR UPDATE`, id).Scan(&bookID, &returnedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Loan{}, ErrNotFound
	}
	if err != nil {
		return Loan{}, err
	}
	if returnedAt != nil {
		return Loan{}, ErrAlreadyClosed
	}
	if _, err := tx.Exec(ctx, `UPDATE loans SET returned_at = now() WHERE id = $1`, id); err != nil {
		return Loan{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE books SET available = available + 1 WHERE id = $1`, bookID); err != nil {
		return Loan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Loan{}, err
	}
	return r.Get(ctx, id)
}

// Handler — HTTP-обработчики /api/loans.
type Handler struct {
	repo *Repository
	pub  events.Publisher
	log  *slog.Logger
}

func NewHandler(repo *Repository, pub events.Publisher, log *slog.Logger) *Handler {
	return &Handler{repo: repo, pub: pub, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/loans", h.list)
	mux.HandleFunc("POST /api/loans", h.issue)
	mux.HandleFunc("GET /api/loans/{id}", h.get)
	mux.HandleFunc("POST /api/loans/{id}/return", h.returnBook)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.List(r.Context(), r.URL.Query().Get("status"))
	writeResult(w, http.StatusOK, list, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err := h.repo.Get(r.Context(), id)
	writeResult(w, http.StatusOK, l, err)
}

func (h *Handler) issue(w http.ResponseWriter, r *http.Request) {
	var in IssueInput
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	days := 14
	if in.Days != nil {
		days = *in.Days
	}
	if in.BookID <= 0 || in.ReaderID <= 0 || days < 0 || days > 365 {
		httpx.Error(w, http.StatusBadRequest, "нужны book_id, reader_id и days от 0 до 365")
		return
	}
	l, err := h.repo.Issue(r.Context(), in.BookID, in.ReaderID, days)
	if err == nil {
		h.publish(r.Context(), events.LoanIssued, l)
	}
	writeResult(w, http.StatusCreated, l, err)
}

func (h *Handler) returnBook(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	l, err := h.repo.Return(r.Context(), id)
	if err == nil {
		h.publish(r.Context(), events.LoanReturned, l)
	}
	writeResult(w, http.StatusOK, l, err)
}

// publish отправляет событие. Ошибка брокера не ломает запрос — только пишется в лог.
func (h *Handler) publish(ctx context.Context, typ string, l Loan) {
	err := h.pub.Publish(ctx, ToEvent(typ, l))
	if err != nil {
		h.log.Error("не удалось отправить событие", "type", typ, "loan_id", l.ID, "err", err)
	}
}

// ToEvent превращает выдачу в событие для брокера.
func ToEvent(typ string, l Loan) events.Event {
	return events.Event{
		Type: typ, LoanID: l.ID,
		BookID: l.BookID, BookTitle: l.BookTitle,
		ReaderID: l.ReaderID, ReaderName: l.ReaderName,
		DueAt: l.DueAt, CreatedAt: time.Now(),
	}
}

func writeResult(w http.ResponseWriter, status int, v any, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrBookNotFound), errors.Is(err, ErrReaderNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNoCopies), errors.Is(err, ErrAlreadyClosed):
		httpx.Error(w, http.StatusConflict, err.Error())
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	default:
		httpx.JSON(w, status, v)
	}
}
