// Package events описывает сообщения, которые api отправляет в брокер, а notifier читает.
package events

import (
	"context"
	"time"
)

// Типы событий.
const (
	LoanIssued   = "loan.issued"
	LoanReturned = "loan.returned"
	LoanOverdue  = "loan.overdue"
)

// Event — сообщение в очереди (сериализуется в JSON).
type Event struct {
	Type       string    `json:"type"`
	LoanID     int64     `json:"loan_id"`
	BookID     int64     `json:"book_id"`
	BookTitle  string    `json:"book_title"`
	ReaderID   int64     `json:"reader_id"`
	ReaderName string    `json:"reader_name"`
	DueAt      time.Time `json:"due_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Publisher — то, что умеет отправлять события. Реализуется брокером (RabbitMQ).
// Благодаря интерфейсу модули выдачи и просрочек не зависят от конкретного брокера.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}
