# ShopList

Приложение «список покупок» для нескольких пользователей: общие списки, отметки «куплено», работа без сети, push-уведомления.

Статус: ранняя разработка. Есть справочник товаров, вход по email-коду, списки с участниками, позиции с отметкой «куплено» мгновенные уведомления (WebSocket) и push через Expo; остальное по [плану](docs/plan.md).

## Стек

Go (chi, pgx, sqlc, goose) · PostgreSQL 16 · React Native + Expo (TypeScript, будет в `mobile/`) · свой VPS (Docker Compose + Caddy).
Подробно и с обоснованием: [docs/plan.md](docs/plan.md).

## Быстрый старт

Нужны Docker и Go 1.26+.

```bash
make dev            # Postgres + сервер в Docker, http://localhost:8080
curl localhost:8080/healthz
curl "localhost:8080/api/v1/catalog/search?q=малоко"   # найдёт «Молоко», несмотря на опечатку
```

В режиме разработки код входа не отправляется по почте, а пишется в лог сервера (`docker compose -f infra/docker-compose.yml logs server`):

```bash
curl -X POST localhost:8080/api/v1/auth/request-code -d '{"email":"me@example.com"}'
curl -X POST localhost:8080/api/v1/auth/verify -d '{"email":"me@example.com","code":"<код из лога>"}'
```

Настройки сервера (JWT, SMTP) описаны в `.env.example`.

Без Docker для сервера: `docker compose -f infra/docker-compose.yml up -d postgres`, затем `make run`.

Тесты (нужен Postgres):

```bash
export TEST_DATABASE_URL="postgres://shoplist:shoplist@localhost:5432/shoplist?sslmode=disable"
make test
```

## Структура

```
server/      Go-сервер (cmd/api, internal/*, migrations, queries для sqlc)
mobile/      приложение на Expo (пока пусто)
api/         OpenAPI-контракт
infra/       docker-compose
docs/        план, архитектура, ADR
```

## Работа с базой

Схема живёт в `server/migrations/*.sql` (goose), миграции применяются при старте сервера.
Запросы пишутся в `server/queries/*.sql`, после правки: `make gen` (нужен [sqlc](https://sqlc.dev)).
