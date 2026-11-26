// Сервис notifier: читает события из RabbitMQ и пишет уведомления читателям в файл.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"library/internal/broker"
	"library/internal/notify"
	"library/pkg/config"
	"library/pkg/logger"
)

func main() {
	log := logger.New("notifier")
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	n, err := notify.New(cfg.NotifyLogFile, log)
	if err != nil {
		log.Error("файл уведомлений", "err", err)
		os.Exit(1)
	}
	defer n.Close()

	rabbit, err := broker.Connect(ctx, cfg.RabbitURL, cfg.QueueName, log)
	if err != nil {
		log.Error("брокер", "err", err)
		os.Exit(1)
	}
	defer rabbit.Close()

	log.Info("notifier запущен", "queue", cfg.QueueName, "file", cfg.NotifyLogFile)
	if err := rabbit.Consume(ctx, n.Handle); err != nil {
		log.Error("consume", "err", err)
		os.Exit(1)
	}
	log.Info("notifier остановлен")
}
