// Package config читает настройки сервисов из переменных окружения.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config — общие настройки для api и notifier.
type Config struct {
	HTTPAddr        string        // адрес HTTP-сервера, например ":8080"
	DatabaseURL     string        // строка подключения к PostgreSQL
	RabbitURL       string        // строка подключения к RabbitMQ
	QueueName       string        // имя очереди событий
	OverdueInterval time.Duration // как часто проверять просрочки
	OverdueWorkers  int           // сколько горутин-воркеров обрабатывают просрочки
	ReportsDir      string        // папка для отчётов (текстовые файлы)
	NotifyLogFile   string        // файл, куда notifier пишет уведомления
}

// Load собирает конфиг из окружения, подставляя значения по умолчанию.
func Load() Config {
	return Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://library:library@localhost:5432/library?sslmode=disable"),
		RabbitURL:       getEnv("RABBIT_URL", "amqp://guest:guest@localhost:5672/"),
		QueueName:       getEnv("QUEUE_NAME", "library.events"),
		OverdueInterval: getDuration("OVERDUE_INTERVAL", 30*time.Second),
		OverdueWorkers:  getInt("OVERDUE_WORKERS", 3),
		ReportsDir:      getEnv("REPORTS_DIR", "./reports"),
		NotifyLogFile:   getEnv("NOTIFY_LOG_FILE", "./logs/notifications.log"),
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}
