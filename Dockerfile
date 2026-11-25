# Один Dockerfile для обоих сервисов: какой собирать, задаёт аргумент SERVICE (api или notifier).

# --- Этап 1: сборка ---
FROM golang:1.24-alpine AS build
WORKDIR /src

# Сначала зависимости — этот слой кэшируется, пока не меняются go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVICE=api
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# --- Этап 2: маленький итоговый образ ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/app /app/app
COPY data /app/data
EXPOSE 8080
ENTRYPOINT ["/app/app"]
