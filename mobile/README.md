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
```

## Структура

- `app/` — экраны (expo-router): `(auth)/login`, `(auth)/code`, `(app)/index` (списки), `(app)/list/[id]` (позиции), `(app)/list/[id]/members`.
- `src/sync/` — офлайн-движок, SQLite-хранилище и WebSocket, описание в `docs/mobile-sync.md`.
- `src/items/` — хук `useListItems` (читает локальное состояние), слияние `merge.ts`, подсказки каталога.
- `src/api/` — HTTP-клиент: подставляет токен, при 401 один раз обновляет его (параллельные запросы делят один refresh, т. к. refresh-токены одноразовые).
- `src/api/secureSession.ts` — токены в Keychain/Keystore через expo-secure-store.
- `src/auth/` — состояние входа; сохранённая сессия работает без сети.

Идентификаторы `app.shoplist` (iOS bundle id и Android package) пока предварительные — их нужно подтвердить до первой публикации.
