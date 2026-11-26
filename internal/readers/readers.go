// Package readers — читатели библиотеки: модель, работа с БД и HTTP-обработчики.
package readers

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

// Reader — читатель.
type Reader struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	CreatedAt time.Time `json:"created_at"`
}

// Input — данные для создания/изменения читателя.
type Input struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func (in *Input) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Name == "" {
		return errors.New("name обязателен")
	}
	if !strings.Contains(in.Email, "@") {
		return errors.New("некорректный email")
	}
	return nil
}

var ErrNotFound = errors.New("читатель не найден")

// Repository — SQL-запросы к таблице readers.
type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const cols = `id, name, email, phone, created_at`

func scan(row pgx.Row) (Reader, error) {
	var rd Reader
	err := row.Scan(&rd.ID, &rd.Name, &rd.Email, &rd.Phone, &rd.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rd, ErrNotFound
	}
	return rd, err
}

func (r *Repository) List(ctx context.Context) ([]Reader, error) {
	rows, err := r.db.Query(ctx, `SELECT `+cols+` FROM readers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Reader{}
	for rows.Next() {
		rd, err := scan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, rd)
	}
	return list, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Reader, error) {
	return scan(r.db.QueryRow(ctx, `SELECT `+cols+` FROM readers WHERE id = $1`, id))
}

func (r *Repository) Create(ctx context.Context, in Input) (Reader, error) {
	return scan(r.db.QueryRow(ctx,
		`INSERT INTO readers (name, email, phone) VALUES ($1, $2, $3) RETURNING `+cols,
		in.Name, in.Email, in.Phone))
}

func (r *Repository) Update(ctx context.Context, id int64, in Input) (Reader, error) {
	return scan(r.db.QueryRow(ctx,
		`UPDATE readers SET name = $2, email = $3, phone = $4 WHERE id = $1 RETURNING `+cols,
		id, in.Name, in.Email, in.Phone))
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM readers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Handler — HTTP-обработчики /api/readers.
type Handler struct{ repo *Repository }

func NewHandler(repo *Repository) *Handler { return &Handler{repo: repo} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/readers", h.list)
	mux.HandleFunc("POST /api/readers", h.create)
	mux.HandleFunc("GET /api/readers/{id}", h.get)
	mux.HandleFunc("PUT /api/readers/{id}", h.update)
	mux.HandleFunc("DELETE /api/readers/{id}", h.delete)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.List(r.Context())
	writeResult(w, http.StatusOK, list, err)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	rd, err := h.repo.Get(r.Context(), id)
	writeResult(w, http.StatusOK, rd, err)
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
	rd, err := h.repo.Create(r.Context(), in)
	writeResult(w, http.StatusCreated, rd, err)
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
	rd, err := h.repo.Update(r.Context(), id, in)
	writeResult(w, http.StatusOK, rd, err)
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
		httpx.Error(w, http.StatusConflict, "читатель с таким email уже есть")
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	default:
		httpx.JSON(w, status, v)
	}
}
