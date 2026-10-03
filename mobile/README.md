# ShopList mobile

Expo (SDK 57) + TypeScript + expo-router. Интерфейс только на русском.

## Запуск

```sh
cd mobile
npm install
EXPO_PUBLIC_API_URL=http://<адрес сервера>:8080 npx expo start
```

Телефон и компьютер должны быть в одной сети; на телефоне откройте Expo Go (Android/iOS) и отсканируйте QR.
Адрес сервера для сборки задаётся переменной `EXPO_PUBLIC_API_URL` (по умолчанию `http://localhost:8080`).

## Проверки

```sh
npm run typecheck   # tsc
npm test            # jest (API-клиент)
npm run export:web  # сборка бандла, ловит ошибки импортов

# Сквозная проверка против настоящего сервера:
#   make dev   # или запустите сервер сами
E2E_API_URL=http://localhost:8080 npm run test:e2e
```

## Структура

- `app/` — экраны (expo-router): `(auth)/login`, `(auth)/code`, `(app)/index` (списки), `(app)/list/[id]` (позиции), `(app)/list/[id]/members`.
- `src/sync/` — офлайн-движок, SQLite-хранилище и WebSocket, описание в `docs/mobile-sync.md`.
- `src/items/` — хук `useListItems` (читает локальное состояние), слияние `merge.ts`, подсказки каталога.
- `src/api/` — HTTP-клиент: подставляет токен, при 401 один раз обновляет его (параллельные запросы делят один refresh, т. к. refresh-токены одноразовые).
- `src/api/secureSession.ts` — токены в Keychain/Keystore через expo-secure-store.
- `src/auth/` — состояние входа; сохранённая сессия работает без сети.

Идентификаторы `app.shoplist` (iOS bundle id и Android package) пока предварительные — их нужно подтвердить до первой публикации.

## Запуск в браузере (для просмотра без телефона)

Нужны Docker и Node.js. В корне репозитория поднимите сервер с базой (он слушает http://localhost:8080 и разрешает браузеру с http://localhost:8081):

```bash
make dev
```

Затем в другом терминале:

```bash
cd mobile
npm install
npx expo start --web --port 8081
```

Откроется приложение: регистрация, коды восстановления, списки, позиции, приглашения (второго участника откройте в окне инкогнито). Офлайн-режим проверяйте на телефоне: в вебе данные хранятся в памяти страницы, push и защищённое хранилище недоступны (сессия лежит в localStorage).
