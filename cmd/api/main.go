// Сервис api: REST API библиотеки + фоновая проверка просрочек.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"library/internal/books"
	"library/internal/broker"
	"library/internal/db"
	"library/internal/files"
	"library/internal/loans"
	"library/internal/overdue"
	"library/internal/readers"
	"library/internal/server"
	"library/pkg/config"
	"library/pkg/logger"
)

func main() {
	log := logger.New("api")
	cfg := config.Load()

	// ctx отменится по Ctrl+C или по сигналу остановки от Docker.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Error("база", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("миграция", "err", err)
		os.Exit(1)
	}

	rabbit, err := broker.Connect(ctx, cfg.RabbitURL, cfg.QueueName, log)
	if err != nil {
		log.Error("брокер", "err", err)
		os.Exit(1)
	}
	defer rabbit.Close()

	// Собираем модули.
	bookRepo := books.NewRepository(pool)
	readerRepo := readers.NewRepository(pool)
	loanRepo := loans.NewRepository(pool)
	importer := files.NewImporter(bookRepo)
	reporter := files.NewReporter(loanRepo, cfg.ReportsDir)

	// Если задан SEED_FILE и каталог пуст — заполняем его книгами из файла.
	if seed := os.Getenv("SEED_FILE"); seed != "" {
		if list, err := bookRepo.List(ctx, ""); err == nil && len(list) == 0 {
			res, err := importer.ImportFile(ctx, seed)
			log.Info("начальная загрузка книг", "file", seed, "imported", res.Imported, "skipped", res.Skipped, "err", err)
		}
	}

	srv := server.New(cfg.HTTPAddr, log,
		books.NewHandler(bookRepo),
		readers.NewHandler(readerRepo),
		loans.NewHandler(loanRepo, rabbit, log),
		files.NewHandler(importer, reporter),
	)

	// Фоновая горутина: проверка просрочек.
	checker := overdue.NewChecker(pool, rabbit, log, cfg.OverdueInterval, cfg.OverdueWorkers)
	go checker.Run(ctx)

	// HTTP-сервер в отдельной горутине, чтобы main мог ждать сигнала остановки.
	go func() {
		log.Info("сервер запущен", "addr", cfg.HTTPAddr, "swagger", "http://localhost"+cfg.HTTPAddr+"/swagger/")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("сервер", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("остановка...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
