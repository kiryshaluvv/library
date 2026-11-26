package files

import (
	"net/http"
	"path/filepath"

	"library/pkg/httpx"
)

// Handler — HTTP-обработчики импорта и отчётов.
type Handler struct {
	importer *Importer
	reporter *Reporter
}

func NewHandler(im *Importer, rp *Reporter) *Handler { return &Handler{importer: im, reporter: rp} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/import/books", h.importBooks)
	mux.HandleFunc("GET /api/reports/loans", h.loansReport)
}

// importBooks: тело запроса — содержимое CSV или JSON файла, формат в ?format=csv|json.
func (h *Handler) importBooks(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	list, err := Parse(format, http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, h.importer.Save(r.Context(), list))
}

// loansReport создаёт текстовый отчёт на диске и отдаёт его как файл.
func (h *Handler) loansReport(w http.ResponseWriter, r *http.Request) {
	path, err := h.reporter.WriteLoansReport(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	http.ServeFile(w, r, path)
}
