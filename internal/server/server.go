// Package server — HTTP-сервер: роутер, middleware, health-check и Swagger UI.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"library/api"
	"library/pkg/httpx"
)

// Module — любой модуль, который умеет регистрировать свои маршруты.
type Module interface {
	Register(mux *http.ServeMux)
}

// New собирает http.Server со всеми модулями.
func New(addr string, log *slog.Logger, modules ...Module) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Swagger: спецификация + страница Swagger UI.
	mux.HandleFunc("GET /swagger/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(api.OpenAPI)
	})
	mux.HandleFunc("GET /swagger/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(swaggerHTML))
	})
	mux.Handle("GET /{$}", http.RedirectHandler("/swagger/", http.StatusFound))

	for _, m := range modules {
		m.Register(mux)
	}

	return &http.Server{
		Addr:              addr,
		Handler:           recoverer(log, logging(log, mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// statusRecorder запоминает код ответа для логов.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// logging пишет в лог каждый запрос: метод, путь, статус, время.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).String())
	})
}

// recoverer не даёт серверу упасть из-за паники в обработчике.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "value", v, "path", r.URL.Path)
				httpx.Error(w, http.StatusInternalServerError, "внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

const swaggerHTML = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <title>Library API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "/swagger/openapi.yaml", dom_id: "#swagger-ui" });
  </script>
</body>
</html>`
