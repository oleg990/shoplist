# ShopList: стек, структура и план (v2, с учётом ответов)

Статус: предложение для согласования, код ещё не пишем.

## 0. Что решено по ответам

| Вопрос | Ответ | Что из этого следует |
|---|---|---|
| Язык | Go | Сервер пишем на Go |
| Аудитория | Все, публикация в сторах | Нужны политика конфиденциальности, удаление аккаунта из приложения (требуют Apple и Google), Apple Developer $99/год, Google Play $25 разово |
| iPhone | Есть | Тестируем на нём через Expo, Mac не нужен |
| Офлайн | Да | Локальная БД на телефоне + очередь изменений + синхронизация |
| Вход | Только собственные аккаунты: email + одноразовый код | Google и Apple пока не делаем; App Store не требует Sign in with Apple (см. раздел 1а) |
| Списки | Много списков, в каждом много участников | Таблица участников списка и приглашения |
| Справочник | Общий для всех | Справочник на сервере, поиск по нему с опечатками |
| Поля позиции | Количество, единицы, цена, категория (без фото) | Сразу в модель данных |
| Купленное | Очищать по кнопке | Кнопка «Очистить купленное», позиции уходят в историю |
| Push | В MVP | Expo Push (FCM для Android, APNs для iOS) |
| Хостинг | Свой VPS | Docker Compose + Caddy + Postgres + бэкапы |
| Язык UI | Русский | Строки всё равно держим в одном файле, чтобы потом легко добавить языки |

## 1. Итоговый стек

| Слой | Выбор | Почему |
|---|---|---|
| Сервер | **Go 1.23+**, роутер **chi** (или echo) | Ваш язык; быстрый, один бинарник, мало памяти на VPS |
| Доступ к БД | **pgx** + **sqlc** (пишете SQL, получаете типизированный Go-код) | Без тяжёлого ORM, полный контроль над запросами |
| Миграции | **goose** | Простые SQL-миграции в репозитории |
| База | **PostgreSQL 16** + расширение **pg_trgm** | Поиск по справочнику с опечатками («малако» → «молоко») |
| Realtime | **WebSocket** (`github.com/coder/websocket`) + Postgres `LISTEN/NOTIFY` | Изменения списка мгновенно у всех участников; NOTIFY позволит позже запустить несколько копий сервера |
| API-контракт | **OpenAPI-спека** → `oapi-codegen` (Go) и `openapi-typescript` (клиент) | Одна спека, типы генерируются для обеих сторон, не расходятся |
| Авторизация | Email-код → свой **JWT** (access 15 мин + refresh) | Таблица `auth_identities` позволит позже добавить Google/Apple без переделки |
| Почта для кодов | SMTP-провайдер (Unisender Go, Resend, Postmark и т.п.) | С VPS напрямую письма попадут в спам |
| Push | **Expo Push API**, вызывается из Go обычным HTTP-запросом | Не нужно отдельно интегрировать FCM и APNs |
| Мобильное приложение | **React Native + Expo, TypeScript** | Один код на Android и iOS, сборка iOS в облаке без Mac |
| Офлайн на клиенте | **expo-sqlite** (или WatermelonDB) + очередь изменений | Список работает в магазине без сети |
| Состояние/запросы | TanStack Query + Zustand | Стандартная связка, хорошо дружит с офлайном |
| Инфраструктура | VPS + Docker Compose: `caddy` (HTTPS автоматически), `server`, `postgres`; бэкап `pg_dump` по cron в S3-совместимое хранилище | Дёшево, просто, всё в одном файле |
| CI/CD | GitHub Actions (go test, golangci-lint, сборка Docker-образа, деплой по тегу) + EAS Build для мобилки | |

**Почему клиент не на Go:** для мобильного UI на Go нет зрелого инструмента (gomobile даёт только библиотеки, не интерфейс). Expo остаётся лучшим вариантом без Mac. TypeScript для Go-разработчика осваивается быстро.

## 1а. Вход и App Store

Вход только через собственные аккаунты приложения: email и одноразовый код из письма. По правилу App Store 4.8 такие приложения не обязаны предлагать Sign in with Apple. Если позже добавим Google, одновременно добавим и Apple: сервер к ещё одному провайдеру готов.

**Альтернатива клиенту:** Flutter (Dart) + Codemagic для iOS-сборок. Тоже рабочий вариант, если Dart нравится больше TS.

## 2. Синхронизация и офлайн (самая сложная часть)

- У каждой позиции есть `id` (UUID, генерирует клиент, чтобы создавать офлайн) и `version` (растёт при каждом изменении на сервере).
- Клиент пишет изменения в локальную SQLite и в очередь `pending_ops`. Когда есть сеть, отправляет пачкой `POST /sync`.
- Сервер применяет изменения, отвечает новыми версиями и всем, что изменилось с `last_sync_version` клиента.
- Конфликты: «последняя запись побеждает» по каждому полю отдельно (два человека отметили разные товары, конфликта нет; один и тот же флажок, побеждает более позднее изменение). Для списка покупок этого достаточно.
- Удаление мягкое (`deleted_at`), чтобы офлайн-клиенты узнали об удалении.
- WebSocket только подсказывает «в списке X есть изменения», данные клиент забирает через тот же `/sync`. Так логика одна и для онлайна, и для офлайна.

## 3. Модель данных

```
users            id, email, name, created_at, deleted_at
auth_identities  user_id, provider (email|google|apple), provider_uid
login_codes      email, code_hash, expires_at, attempts
refresh_tokens   id, user_id, token_hash, expires_at, revoked_at
devices          id, user_id, expo_push_token, platform, updated_at

lists            id, title, owner_id, created_at, updated_at, deleted_at
list_members     list_id, user_id, role (owner|editor), joined_at
invites          id, list_id, code, created_by, expires_at, max_uses

categories       id, name, sort_order               -- «Молочное», «Овощи»
catalog_items    id, name, category_id, default_unit, popularity
units            code, name                          -- шт, кг, г, л, уп

items            id (uuid), list_id, catalog_item_id NULL, name, quantity, unit,
                 price, category_id, is_bought, bought_by, bought_at,
                 position, created_by, version, updated_at, deleted_at
purchase_history id, list_id, item_name, quantity, unit, price, bought_by, bought_at
```

Справочник общий: стартовый набор (~1–2 тыс. товаров) загружаем сидом. Если пользователь ввёл товар, которого нет, он сохраняется только в его позиции; часто вводимые новые товары можно потом добавлять в общий справочник (сначала вручную, позже автоматически).

«Очистить купленное» переносит позиции с `is_bought = true` в `purchase_history` и мягко удаляет их. История пригодится для подсказок и статистики расходов.

## 4. Структура репозитория (монорепо)

```
shoplist/
├── server/                    # Go
│   ├── cmd/api/main.go
│   ├── internal/
│   │   ├── auth/              # email-коды, JWT
│   │   ├── lists/             # списки, участники, приглашения
│   │   ├── items/             # позиции, sync
│   │   ├── catalog/           # справочник и поиск
│   │   ├── realtime/          # WebSocket-хаб, LISTEN/NOTIFY
│   │   ├── push/              # Expo Push
│   │   ├── db/                # sqlc-сгенерированный код
│   │   └── httpapi/           # хендлеры, middleware
│   ├── migrations/            # goose
│   ├── queries/               # .sql для sqlc
│   ├── sqlc.yaml
│   ├── Dockerfile
│   └── go.mod
├── mobile/                    # Expo (TypeScript)
│   ├── app/                   # экраны (expo-router)
│   ├── src/{api,db,sync,components,i18n}/
│   └── app.json, eas.json
├── api/
│   └── openapi.yaml           # единый контракт API
├── infra/
│   ├── docker-compose.yml     # локальная разработка
│   ├── docker-compose.prod.yml
│   ├── Caddyfile
│   └── backup/
├── docs/
│   ├── architecture.md
│   ├── sync.md                # как работает синхронизация
│   ├── deploy.md
│   └── adr/                   # записи решений: «почему Go», «почему Expo»
├── .github/workflows/         # server-ci.yml, mobile-ci.yml, deploy.yml
├── Makefile                   # make dev, make migrate, make gen, make test
└── README.md                  # как поднять всё локально за 5 минут
```

## 5. GitHub

- Репозиторий `shoplist`, ветка `main` защищена, работа через ветки и PR.
- GitHub Projects (доска) + Issues с метками `server`, `mobile`, `infra`.
- Секреты (БД, JWT-ключ, SMTP, ключи push, SSH для деплоя) только в GitHub Secrets и `.env` на VPS.
- Деплой: тег `v*` → сборка образа → GHCR → `docker compose pull && up -d` на VPS по SSH.

## 6. Что подготовить заранее (не код)

- Домен (например, `shoplist.ru`) и VPS с Ubuntu, 2 ГБ RAM хватит.
- Apple Developer Program ($99/год): нужен для push на iOS и публикации. Оформление может занять несколько дней.
- Google Play Console ($25), Firebase-проект для push на Android.
- Аккаунт Expo (бесплатно).
- SMTP-провайдер для писем с кодами.
- Политика конфиденциальности на сайте (обязательна для сторов).

## 7. Дорожная карта

1. **Каркас:** репозиторий, docker-compose (Postgres), Go-сервер с healthcheck, миграции, OpenAPI-спека, CI.
2. **Авторизация:** email-код, JWT, удаление аккаунта.
3. **Списки и позиции:** CRUD, участники, приглашения по ссылке/коду.
4. **Справочник:** сид категорий и товаров, поиск с pg_trgm.
5. **Мобилка MVP:** вход, мои списки, список с флажками, добавление с автодополнением, «очистить купленное».
6. **Синхронизация и офлайн:** `/sync`, локальная SQLite, очередь, WebSocket.
7. **Push:** регистрация устройств, уведомления «добавили товар» / «всё купили».
8. **Деплой и публикация:** VPS + Caddy + бэкапы, TestFlight, закрытое тестирование в Google Play, затем релиз.
9. **Дальше:** история и статистика расходов, порядок категорий под конкретный магазин, повторяющиеся покупки, голосовой ввод, мультиязычность.

## 8. Что ещё стоит решить (не блокирует старт)

- Когда добавлять вход через Google (тогда сразу и через Apple).
- Название приложения и бандл-id (`ru.example.shoplist`): нужны для регистрации в сторах.
- Права участников: все редакторы, или нужен режим «только смотреть»?
- Валюта цены: только рубли?
