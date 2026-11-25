// Package httpx — общие помощники для HTTP-обработчиков (JSON-ответы, ошибки, парсинг id).
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// ErrorResponse — формат ошибки в ответах API.
type ErrorResponse struct {
	Error string `json:"error"`
}

// JSON пишет ответ в формате JSON с нужным статусом.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Error пишет ошибку в формате {"error": "..."}.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, ErrorResponse{Error: msg})
}

// Decode читает JSON из тела запроса в v. Неизвестные поля считаются ошибкой.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("некорректный JSON: " + err.Error())
	}
	return nil
}

// PathID достаёт целочисленный параметр {id} из пути запроса.
func PathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("некорректный id")
	}
	return id, nil
}
