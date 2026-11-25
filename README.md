# Library — сервис библиотеки на Go

Проектная работа по дисциплине «Программирование на языке Go».

Сервис ведёт каталог книг и читателей, выдаёт и принимает книги, следит за просрочками
и отправляет уведомления читателям через брокер сообщений RabbitMQ.

## Архитектура

```
                ┌────────────────────────── api (Go) ──────────────────────────┐
  клиент ─HTTP─▶│ server (роутер, middleware, Swagger UI)                       │
  (Swagger UI)  │   ├─ books    ─┐                                              │
                │   ├─ readers  ─┼──▶ PostgreSQL                                │
                │   ├─ loans    ─┘        ▲                                     │
                │   ├─ files  (импорт CSV/JSON, отчёты в .txt)                  │
                │   └─ overdue (тикер + пул горутин) ─┘                         │
                └──────────────┬────────────────────────────────────────────────┘
                               │ события loan.issued / loan.returned / loan.overdue (JSON)
                               ▼
                           RabbitMQ  ──▶  notifier (Go) ──▶ logs/notifications.log
```

| Тема из методички | Где в проекте |
|---|---|
| Модули и пакеты | `go.mod`, разбиение на `cmd/`, `internal/`, `pkg/` |
| Текстовые файлы | `internal/files` (импорт CSV, отчёт .txt), `internal/notify` (лог уведомлений) |
| JSON | REST API, импорт JSON, события в брокере (`internal/events`) |
| Параллелизм | `internal/overdue` (горутины, каналы, WaitGroup, atomic, context), graceful shutdown |
| База данных | PostgreSQL + pgx, транзакции и `SELECT ... FOR UPDATE` в `internal/loans` |
| Сетевое программирование | HTTP-сервер `internal/server`, клиент AMQP `internal/broker` |
| Swagger UI | `api/openapi.yaml`, страница `/swagger/` |
| Docker | `Dockerfile` (multi-stage), `docker-compose.yml` |
| CI/CD | `.github/workflows/ci.yml` (проверка, сборка, публикация образов в ghcr.io) |
| Брокер сообщений | RabbitMQ: `internal/broker`, сервис `cmd/notifier` |

## Структура

```
library/
├── cmd/
│   ├── api/main.go          # точка входа REST API
│   └── notifier/main.go     # точка входа сервиса уведомлений
├── internal/
│   ├── books/               # каталог книг (CRUD)
│   ├── readers/             # читатели (CRUD)
│   ├── loans/               # выдача и возврат книг, отправка событий
│   ├── files/               # импорт CSV/JSON, текстовые отчёты
│   ├── overdue/             # фоновая проверка просрочек (пул горутин)
│   ├── notify/              # формирование уведомлений и запись в файл
│   ├── broker/              # RabbitMQ: publish / consume
│   ├── events/              # формат событий и интерфейс Publisher
│   ├── server/              # HTTP-сервер, middleware, Swagger UI
│   └── db/                  # подключение к PostgreSQL и схема таблиц
├── pkg/
│   ├── config/              # настройки из переменных окружения
│   ├── logger/              # общий логгер (slog)
│   └── httpx/               # помощники для JSON-ответов
├── api/openapi.yaml         # спецификация для Swagger
├── data/                    # примеры файлов для импорта
├── Dockerfile
├── docker-compose.yml
└── .github/workflows/ci.yml
```

## Запуск

Нужен только Docker Desktop.

```bash
docker compose up --build
```

После запуска:

- Swagger UI: http://localhost:8080/swagger/
- Панель RabbitMQ: http://localhost:15672 (логин и пароль `guest` / `guest`)
- Уведомления: файл `logs/notifications.log`
- Отчёты: папка `reports/`

При первом запуске каталог автоматически заполняется книгами из `data/books.csv`.

Остановить: `Ctrl+C`, затем `docker compose down` (добавьте `-v`, чтобы удалить и базу).

### Запуск без Docker (для разработки)

Поднять только базу и брокер, а Go-сервисы запускать локально:

```bash
docker compose up -d postgres rabbitmq
go run ./cmd/api
go run ./cmd/notifier   # во втором терминале
```

## Сценарий для демонстрации

Всё можно делать прямо из Swagger UI («Try it out»).

1. `GET /api/books` — каталог уже заполнен из CSV.
2. `POST /api/import/books?format=json` — загрузить содержимое `data/books.json`.
3. `POST /api/readers` — создать читателя:
   `{"name": "Иван Петров", "email": "ivan@example.com"}`
4. `POST /api/loans` — выдать книгу на 0 дней, чтобы сразу получить просрочку:
   `{"book_id": 1, "reader_id": 1, "days": 0}`.
   В `logs/notifications.log` появится уведомление о выдаче.
5. Подождать ~15 секунд: фоновая проверка найдёт просрочку, в логах api будет видно,
   какой воркер её обработал, а в `notifications.log` появится напоминание.
6. `GET /api/reports/loans` — скачать текстовый отчёт (он же сохранится в `reports/`).
7. `POST /api/loans/1/return` — вернуть книгу; придёт уведомление о возврате.

## API

| Метод | Путь | Что делает |
|---|---|---|
| GET | `/api/books?search=` | список книг, поиск |
| POST | `/api/books` | добавить книгу |
| GET / PUT / DELETE | `/api/books/{id}` | получить / изменить / удалить |
| GET | `/api/readers` | список читателей |
| POST | `/api/readers` | добавить читателя |
| GET / PUT / DELETE | `/api/readers/{id}` | получить / изменить / удалить |
| GET | `/api/loans?status=active\|overdue` | список выдач |
| POST | `/api/loans` | выдать книгу |
| GET | `/api/loans/{id}` | получить выдачу |
| POST | `/api/loans/{id}/return` | вернуть книгу |
| POST | `/api/import/books?format=csv\|json` | импорт книг из файла |
| GET | `/api/reports/loans` | текстовый отчёт |
| GET | `/health` | проверка работоспособности |

## Распределение работы (8 участников)

| # | Ветка | Участник | Файлы | Что сделано |
|---|---|---|---|---|
| 1 | `feature/books` | | `internal/books/` | CRUD книг, поиск, работа с PostgreSQL |
| 2 | `feature/readers` | | `internal/readers/` | CRUD читателей, валидация |
| 3 | `feature/loans` | | `internal/loans/` | выдача и возврат в транзакциях, `FOR UPDATE`, публикация событий |
| 4 | `feature/files` | | `internal/files/`, `data/` | импорт CSV/JSON, текстовые отчёты |
| 5 | `feature/notifier` | | `cmd/notifier/`, `internal/notify/`, `internal/broker/`, `internal/events/` | RabbitMQ, второй сервис, запись уведомлений в файл |
| 6 | `feature/overdue` | | `internal/overdue/` | фоновая проверка: тикер, пул горутин, каналы, WaitGroup |
| 7 | `feature/server` | | `internal/server/`, `api/`, `cmd/api/` | HTTP-сервер, middleware, Swagger, graceful shutdown |
| 8 | `feature/devops` | | `Dockerfile`, `docker-compose.yml`, `.github/`, `pkg/`, `internal/db/` | Docker, CI/CD, конфиг, логгер, схема БД |

## Работа с Git

```bash
git checkout main
git pull
git checkout -b feature/books      # своя ветка
# ... работа ...
git add .
git commit -m "books: CRUD книг"
git push -u origin feature/books
```

Затем на GitHub открыть Pull Request в `main`. После прохождения CI и одобрения
кем-то из команды ветку можно вливать.
