# Сборка и публикация приложения

Собирать iOS без Mac можно в облаке Expo (EAS Build). Здесь перечислено, что нужно сделать вам (аккаунты и ключи),
и какие команды запускать. Ничего из этого пока не выполнялось: нужны ваши аккаунты.

## Что понадобится

| Что | Зачем | Стоимость |
|---|---|---|
| Аккаунт Expo (expo.dev) | облачные сборки, push-токены | бесплатно для начала |
| Apple Developer Program | TestFlight, App Store, push на iPhone | 99 $/год |
| Google Play Console | публикация на Android | 25 $ один раз |
| Проект Firebase (FCM) | push на Android | бесплатно |
| Домен и VPS с HTTPS | сервер; адрес API зашивается в сборку | по тарифу |
| Политика конфиденциальности (URL) | обязательна для обоих магазинов | |

## Первый запуск без магазинов (Expo Go)

`cd mobile && npm install && EXPO_PUBLIC_API_URL=http://<IP компьютера>:8080 npx expo start`, затем отсканируйте QR в приложении
Expo Go на телефоне. Вход, списки, офлайн и WebSocket так проверяются. Push в Expo Go проверять не стоит: нужна собственная сборка.

## Собственные сборки

```sh
npm i -g eas-cli
cd mobile
eas login
eas init                 # создаст projectId и запишет его в app.json (extra.eas.projectId); без него push не регистрируется
eas build -p android --profile preview     # APK для проверки на телефоне
eas build -p ios --profile preview         # потребует Apple Developer; для установки на iPhone нужна регистрация устройства
```

В `eas.json` замените `https://api.example.com` на адрес вашего сервера. Это значение `EXPO_PUBLIC_API_URL` попадает в приложение при сборке.

## Push

- **Android:** создайте проект в Firebase, добавьте Android-приложение с пакетом `app.shoplist`, скачайте `google-services.json`
  и ключ сервисного аккаунта для FCM v1, загрузите ключ в EAS (`eas credentials`). Путь к `google-services.json` пропишите в `app.json` (`android.googleServicesFile`).
- **iOS:** EAS сам создаст ключ APNs после входа в Apple Developer (`eas credentials`).
- Сервер отправляет через Expo Push API (`docs/push.md`); если включите защиту push-токенов в проекте Expo, задайте серверу `EXPO_ACCESS_TOKEN`.

## Перед публикацией

- Подтвердите название и идентификаторы: сейчас `ShopList` и `app.shoplist` (iOS bundle id и Android package). После первой публикации менять их нельзя.
- Иконка и заставка в `mobile/assets` пока шаблонные, нужны свои.
- Политика конфиденциальности и описание сбора данных (email, имя, списки, push-токен) для App Store и Google Play.
- Удаление аккаунта: кнопка есть на экране списков (`DELETE /api/v1/me`), Apple требует её для приложений с регистрацией. В описании для магазинов укажите, где она находится.
- Вход только через email-код, поэтому «Вход через Apple» не требуется (правило 4.8). Если добавится Google, добавится и Apple.
- Для проверки Apple понадобится тестовый аккаунт с рабочим входом (например, email, на который вы заранее знаете код).
