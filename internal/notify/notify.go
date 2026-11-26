// Package notify — логика сервиса уведомлений: превращает событие в текст и пишет его в файл.
package notify

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"library/internal/events"
)

// Notifier пишет уведомления в текстовый файл (по строке на событие).
type Notifier struct {
	mu   sync.Mutex // защищает файл, если Handle вызовут из нескольких горутин
	file *os.File
	log  *slog.Logger
}

// New открывает (или создаёт) файл уведомлений в режиме дописывания.
func New(path string, log *slog.Logger) (*Notifier, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Notifier{file: f, log: log}, nil
}

// Message формирует текст уведомления для читателя.
func Message(e events.Event) string {
	due := e.DueAt.Format("02.01.2006")
	switch e.Type {
	case events.LoanIssued:
		return fmt.Sprintf("%s, вы взяли книгу «%s». Вернуть до %s.", e.ReaderName, e.BookTitle, due)
	case events.LoanReturned:
		return fmt.Sprintf("%s, спасибо! Книга «%s» возвращена.", e.ReaderName, e.BookTitle)
	case events.LoanOverdue:
		return fmt.Sprintf("%s, срок возврата книги «%s» истёк %s. Пожалуйста, верните её.", e.ReaderName, e.BookTitle, due)
	default:
		return fmt.Sprintf("неизвестное событие %q", e.Type)
	}
}

// Handle обрабатывает одно событие из очереди.
func (n *Notifier) Handle(e events.Event) error {
	msg := Message(e)
	line := fmt.Sprintf("%s [%s] reader=%d loan=%d: %s\n",
		time.Now().Format(time.RFC3339), e.Type, e.ReaderID, e.LoanID, msg)

	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := n.file.WriteString(line); err != nil {
		return err
	}
	n.log.Info("уведомление", "type", e.Type, "reader_id", e.ReaderID, "text", msg)
	return nil
}

func (n *Notifier) Close() error { return n.file.Close() }
