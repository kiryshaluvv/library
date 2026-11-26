// Package files — работа с файлами: импорт книг из CSV/JSON и выгрузка текстовых отчётов.
package files

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"library/internal/books"
)

// ImportResult — итог импорта.
type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

// BookCreator — то, что умеет сохранять книгу (реализует books.Repository).
type BookCreator interface {
	Create(ctx context.Context, in books.Input) (books.Book, error)
}

// Importer загружает книги из файлов.
type Importer struct{ repo BookCreator }

func NewImporter(repo BookCreator) *Importer { return &Importer{repo: repo} }

// ParseCSV читает CSV с заголовком: title,author,year,isbn,total.
func ParseCSV(r io.Reader) ([]books.Input, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ошибка CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, errors.New("в CSV нет данных (нужен заголовок и хотя бы одна строка)")
	}

	// Находим колонки по заголовку, чтобы порядок колонок был любым.
	idx := map[string]int{}
	for i, h := range records[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, need := range []string{"title", "author"} {
		if _, ok := idx[need]; !ok {
			return nil, fmt.Errorf("в CSV нет колонки %q", need)
		}
	}
	get := func(row []string, name string) string {
		if i, ok := idx[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	list := make([]books.Input, 0, len(records)-1)
	for _, row := range records[1:] {
		year, _ := strconv.Atoi(get(row, "year"))
		total, _ := strconv.Atoi(get(row, "total"))
		list = append(list, books.Input{
			Title: get(row, "title"), Author: get(row, "author"),
			Year: year, ISBN: get(row, "isbn"), Total: total,
		})
	}
	return list, nil
}

// ParseJSON читает JSON-массив книг.
func ParseJSON(r io.Reader) ([]books.Input, error) {
	var list []books.Input
	if err := json.NewDecoder(r).Decode(&list); err != nil {
		return nil, fmt.Errorf("ошибка JSON: %w", err)
	}
	return list, nil
}

// Parse выбирает парсер по формату ("csv" или "json").
func Parse(format string, r io.Reader) ([]books.Input, error) {
	switch strings.ToLower(format) {
	case "csv":
		return ParseCSV(r)
	case "json":
		return ParseJSON(r)
	default:
		return nil, fmt.Errorf("неизвестный формат %q (нужен csv или json)", format)
	}
}

// Save сохраняет книги в базу; плохие строки и дубликаты пропускаются.
func (im *Importer) Save(ctx context.Context, list []books.Input) ImportResult {
	res := ImportResult{Errors: []string{}}
	for i, in := range list {
		if err := in.Validate(); err != nil {
			res.Skipped++
			res.Errors = append(res.Errors, fmt.Sprintf("запись %d: %v", i+1, err))
			continue
		}
		if _, err := im.repo.Create(ctx, in); err != nil {
			res.Skipped++
			res.Errors = append(res.Errors, fmt.Sprintf("запись %d (%s): %v", i+1, in.Title, err))
			continue
		}
		res.Imported++
	}
	return res
}

// ImportFile импортирует книги из файла на диске; формат берётся из расширения.
func (im *Importer) ImportFile(ctx context.Context, path string) (ImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return ImportResult{}, err
	}
	defer f.Close()
	format := strings.TrimPrefix(filepath.Ext(path), ".")
	list, err := Parse(format, f)
	if err != nil {
		return ImportResult{}, err
	}
	return im.Save(ctx, list), nil
}
