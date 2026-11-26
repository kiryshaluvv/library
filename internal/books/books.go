// Package books — каталог книг: модель, работа с БД и HTTP-обработчики.
package books

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"library/pkg/httpx"
)

// Book — книга в каталоге.
type Book struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	Year      int       `json:"year"`
	ISBN      string    `json:"isbn"`
	Total     int       `json:"total"`     // сколько экземпляров всего
	Available int       `json:"available"` // сколько сейчас на полке
	CreatedAt time.Time `json:"created_at"`
}

// Input — данные для создания/изменения книги.
type Input struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Year   int    `json:"year"`
	ISBN   string `json:"isbn"`
	Total  int    `json:"total"`
}

// Validate проверяет обязательные поля.
func (in *Input) Validate() error {
	in.Title = strings.TrimSpace(in.Title)
	in.Author = strings.TrimSpace(in.Author)
	in.ISBN = strings.TrimSpace(in.ISBN)
	if in.Title == "" || in.Author == "" {
		return errors.New("title и author обязательны")
	}
	if in.Total <= 0 {
		in.Total = 1
	}
	return nil
}

// ErrNotFound — книга не найдена.
var ErrNotFound = errors.New("книга не найдена")

// Repository — SQL-запросы к таблице books.
type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const cols = `id, title, author, year, COALESCE(isbn, ''), total, available, created_at`

func scan(row pgx.Row) (Book, error) {
	var b Book
	err := row.Scan(&b.ID, &b.Title, &b.Author, &b.Year, &b.ISBN, &b.Total, &b.Available, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// List возвращает книги; search фильтрует по названию или автору.
func (r *Repository) List(ctx context.Context, search string) ([]Book, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+cols+` FROM books
		 WHERE $1 = '' OR title ILIKE '%' || $1 || '%' OR author ILIKE '%' || $1 || '%'
		 ORDER BY id`, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Book{}
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, b)
	}
	return list, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Book, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+cols+` FROM books WHERE id = $1`, id))
}

// Create добавляет книгу; все экземпляры сразу доступны.
func (r *Repository) Create(ctx context.Context, in Input) (Book, error) {
	return scan(r.db.QueryRow(ctx,
		`INSERT INTO books (title, author, year, isbn, total, available)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5, $5)
		 RETURNING `+cols,
		in.Title, in.Author, in.Year, in.ISBN, in.Total))
}

// Update меняет данные книги. Доступное количество сдвигается на разницу total.
func (r *Repository) Update(ctx context.Context, id int64, in Input) (Book, error) {
	return scan(r.db.QueryRow(ctx,
		`UPDATE books SET title = $2, author = $3, year = $4, isbn = NULLIF($5, ''),
		        available = GREATEST(available + ($6 - total), 0), total = $6
		 WHERE id = $1
		 RETURNING `+cols,
		id, in.Title, in.Author, in.Year, in.ISBN, in.Total))
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM books WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Handler — HTTP-обработчики /api/books.
type Handler struct{ repo *Repository }

func NewHandler(repo *Repository) *Handler { return &Handler{repo: repo} }

// Register вешает маршруты на mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/books", h.list)
	mux.HandleFunc("POST /api/books", h.create)
	mux.HandleFunc("GET /api/books/{id}", h.get)
	mux.HandleFunc("PUT /api/books/{id}", h.update)
	mux.HandleFunc("DELETE /api/books/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.List(r.Context(), r.URL.Query().Get("search"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.repo.Get(r.Context(), id)
	writeResult(w, http.StatusOK, b, err)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.repo.Create(r.Context(), in)
	writeResult(w, http.StatusCreated, b, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := in.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.repo.Update(r.Context(), id, in)
	writeResult(w, http.StatusOK, b, err)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeResult(w, 0, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeResult(w http.ResponseWriter, status int, v any, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case err != nil && strings.Contains(err.Error(), "duplicate key"):
		httpx.Error(w, http.StatusConflict, "книга с таким ISBN уже есть")
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	default:
		httpx.JSON(w, status, v)
	}
}
