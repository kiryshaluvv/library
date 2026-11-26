// Package broker — работа с RabbitMQ: отправка и чтение событий.
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"library/internal/events"
)

// Rabbit держит соединение и канал к RabbitMQ и одну durable-очередь.
type Rabbit struct {
	conn  *amqp.Connection
	ch    *amqp.Channel
	queue string
	log   *slog.Logger
}

// Connect подключается к RabbitMQ (с повторами, пока брокер стартует) и объявляет очередь.
func Connect(ctx context.Context, url, queue string, log *slog.Logger) (*Rabbit, error) {
	var conn *amqp.Connection
	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			break
		}
		log.Warn("RabbitMQ недоступен, повтор", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось подключиться к RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	// durable=true: очередь переживёт перезапуск брокера.
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		conn.Close()
		return nil, err
	}
	return &Rabbit{conn: conn, ch: ch, queue: queue, log: log}, nil
}

// Publish сериализует событие в JSON и кладёт в очередь.
func (r *Rabbit) Publish(ctx context.Context, e events.Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return r.ch.PublishWithContext(ctx, "", r.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

// Consume читает события из очереди и вызывает handle для каждого, пока не отменён ctx.
// Если handle вернул ошибку, сообщение возвращается в очередь.
func (r *Rabbit) Consume(ctx context.Context, handle func(events.Event) error) error {
	msgs, err := r.ch.Consume(r.queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-msgs:
			if !ok {
				return fmt.Errorf("канал RabbitMQ закрыт")
			}
			var e events.Event
			if err := json.Unmarshal(m.Body, &e); err != nil {
				r.log.Error("битое сообщение, пропускаю", "err", err)
				_ = m.Nack(false, false)
				continue
			}
			if err := handle(e); err != nil {
				r.log.Error("ошибка обработки", "err", err)
				_ = m.Nack(false, true)
				continue
			}
			_ = m.Ack(false)
		}
	}
}

// Close закрывает канал и соединение.
func (r *Rabbit) Close() {
	_ = r.ch.Close()
	_ = r.conn.Close()
}
