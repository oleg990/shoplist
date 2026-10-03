-- +goose Up
-- Вход по логину и паролю вместо кода из письма (почтового сервиса нет).
-- Данные разработки переносятся: логин получается из части адреса до «@» плюс начало id.

ALTER TABLE users ADD COLUMN username text;
UPDATE users SET username = left(regexp_replace(split_part(email, '@', 1), '[^A-Za-z0-9_.-]', '_', 'g'), 20) || '_' || left(id::text, 8);
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
CREATE UNIQUE INDEX users_username_key ON users (lower(username)) WHERE deleted_at IS NULL;
DROP INDEX users_email_key;
ALTER TABLE users DROP COLUMN email;

DROP TABLE login_codes;
DROP TABLE auth_identities;

-- Пароль (argon2id) и защита от подбора: после серии неудач вход на время блокируется.
CREATE TABLE credentials (
    user_id         uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash   text NOT NULL,
    failed_attempts int NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Одноразовые коды восстановления: заменяют сброс пароля по письму.
CREATE TABLE recovery_codes (
    id        bigserial PRIMARY KEY,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash text NOT NULL,
    used_at   timestamptz,
    UNIQUE (user_id, code_hash)
);

-- +goose Down
-- Откат не восстанавливает прежние данные: email-коды больше не используются.
DROP TABLE recovery_codes;
DROP TABLE credentials;
CREATE TABLE auth_identities (
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider     text NOT NULL CHECK (provider IN ('email')),
    provider_uid text NOT NULL,
    PRIMARY KEY (provider, provider_uid)
);
CREATE TABLE login_codes (
    id         bigserial PRIMARY KEY,
    email      text NOT NULL,
    code_hash  text NOT NULL,
    attempts   int NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE users ADD COLUMN email text;
UPDATE users SET email = username || '@invalid.example';
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE deleted_at IS NULL;
DROP INDEX users_username_key;
ALTER TABLE users DROP COLUMN username;
