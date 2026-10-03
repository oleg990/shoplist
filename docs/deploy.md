# Развёртывание на VPS

Всё крутится на одном сервере в Docker: **Caddy** (HTTPS) → **сервер** → **Postgres**, рядом контейнер **бэкапов**.
Файлы: `infra/prod/`. Файл compose проверен командой `docker compose config` (синтаксис и обязательные переменные); сами контейнеры, получение сертификата и бэкапы на настоящем сервере не запускались (в среде разработки Docker-демона нет), поэтому первый запуск делайте внимательно и пишите, если что-то не заведётся.

## Что нужно заранее

1. VPS с Linux (подойдёт 1 vCPU / 1–2 ГБ), Docker и Docker Compose plugin.
2. Домен (например `api.ваш-домен.ru`) с A-записью на IP сервера. Порты 80 и 443 открыты.
3. Образ сервера в GitHub Container Registry. Его собирает workflow `server-image` при каждом изменении `server/` в `main`.
   После первой сборки сделайте пакет публичным (GitHub → Packages → shoplist-server → Package settings → Change visibility)
   или на сервере выполните `docker login ghcr.io` с токеном `read:packages`.

## Первый запуск

```sh
git clone https://github.com/oleg990/shoplist.git && cd shoplist/infra/prod
cp .env.example .env && chmod 600 .env
# заполните .env: DOMAIN, ACME_EMAIL, POSTGRES_PASSWORD, JWT_SECRET
docker compose pull
docker compose up -d
docker compose logs -f server caddy
```

Проверка: `curl https://<домен>/healthz` должен вернуть 200. Миграции базы сервер применяет сам при старте.
Затем в приложении (`mobile/eas.json`) подставьте `https://<домен>` в `EXPO_PUBLIC_API_URL` (см. `docs/mobile-release.md`).

## Обновление

```sh
cd shoplist/infra/prod
git pull && docker compose pull && docker compose up -d
```

Перед обновлением с миграциями стоит сделать бэкап (ниже).

## Бэкапы

Контейнер `backup` раз в сутки в 03:00 UTC делает `pg_dump` в `BACKUP_DIR` (по умолчанию `infra/prod/backups/`)
и хранит `BACKUP_KEEP_DAYS` дней (14).

- Копия прямо сейчас: `docker compose run --rm backup now`
- **Бэкап на том же диске не защищает от потери сервера.** Регулярно копируйте папку на другое место, например
  `rsync -a сервер:~/shoplist/infra/prod/backups/ ~/shoplist-backups/` по cron, или в облачное хранилище.
- Восстановление в чистую базу:
  ```sh
  gunzip -c backups/shoplist-ГГГГММДД-ЧЧММСС.sql.gz | docker compose exec -T postgres psql -U shoplist -d shoplist
  ```
  Проверьте восстановление на тестовой копии до того, как оно понадобится по-настоящему.

## Безопасность

- Наружу открыты только 80 и 443; Postgres и сервер порты не публикуют. Включите файрвол (`ufw allow 22,80,443/tcp`), вход по SSH-ключу, автообновления безопасности.
- `.env` содержит секреты: права 600, не коммитить. Смена `JWT_SECRET` разлогинит всех (токены доступа живут 15 минут, дальше обновление по refresh-токену не затронуто).
- Ограничения частоты: по IP (в памяти процесса): все запросы до 20 в секунду с запасом 200, вход (`/auth/*`) до 1 запроса в 2 секунды с запасом 20; кроме того, код на один email не чаще раза в минуту и ограничено число попыток ввода кода. За Caddy адрес клиента берётся из `X-Forwarded-For`, который Caddy выставляет сам.
- Если будете добавлять лимит по IP: сервер использует `middleware.RealIP`, то есть доверяет заголовку `X-Forwarded-For`. За Caddy это безопасно только если сервер недоступен напрямую (в `compose.yml` порт не публикуется); без прокси заголовок подделывается.
- Для публикации в магазинах нужна страница политики конфиденциальности (`docs/mobile-release.md`).

## Мониторинг (минимум)

`docker compose ps`, `docker compose logs --tail=100 server`. Логи сервера в JSON. Для внешней проверки доступности подойдёт
любой бесплатный монитор на `https://<домен>/healthz`.
