-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Пользователи и вход по email-коду

CREATE TABLE users (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text NOT NULL,
    name       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE deleted_at IS NULL;

-- Способы входа. Сейчас только email; Google/Apple добавятся новыми provider.
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
CREATE INDEX login_codes_email_idx ON login_codes (lower(email), created_at DESC);

CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Устройства для push-уведомлений (Expo push token)
CREATE TABLE devices (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expo_push_token text NOT NULL UNIQUE,
    platform        text NOT NULL CHECK (platform IN ('android', 'ios')),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Справочник

CREATE TABLE categories (
    id         serial PRIMARY KEY,
    name       text NOT NULL UNIQUE,
    sort_order int NOT NULL DEFAULT 0
);

CREATE TABLE units (
    code text PRIMARY KEY,
    name text NOT NULL
);
INSERT INTO units (code, name) VALUES
    ('pcs', 'шт'), ('kg', 'кг'), ('g', 'г'), ('l', 'л'), ('ml', 'мл'), ('pack', 'уп');

CREATE TABLE catalog_items (
    id           serial PRIMARY KEY,
    name         text NOT NULL UNIQUE,
    category_id  int REFERENCES categories (id) ON DELETE SET NULL,
    default_unit text REFERENCES units (code),
    popularity   int NOT NULL DEFAULT 0
);
CREATE INDEX catalog_items_name_trgm_idx ON catalog_items USING gin (lower(name) gin_trgm_ops);

-- Списки и участники

CREATE TABLE lists (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text NOT NULL,
    owner_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE list_members (
    list_id   uuid NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role      text NOT NULL DEFAULT 'editor' CHECK (role IN ('owner', 'editor')),
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (list_id, user_id)
);
CREATE INDEX list_members_user_idx ON list_members (user_id);

CREATE TABLE invites (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id    uuid NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    code       text NOT NULL UNIQUE,
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    max_uses   int NOT NULL DEFAULT 10,
    uses       int NOT NULL DEFAULT 0
);

-- Позиции списка. id генерирует клиент (офлайн), version растёт при каждом изменении.
CREATE SEQUENCE item_version_seq;

CREATE TABLE items (
    id              uuid PRIMARY KEY,
    list_id         uuid NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    catalog_item_id int REFERENCES catalog_items (id) ON DELETE SET NULL,
    name            text NOT NULL,
    quantity        numeric(10, 3),
    unit            text REFERENCES units (code),
    price           numeric(12, 2),
    category_id     int REFERENCES categories (id) ON DELETE SET NULL,
    is_bought       boolean NOT NULL DEFAULT false,
    bought_by       uuid REFERENCES users (id) ON DELETE SET NULL,
    bought_at       timestamptz,
    position        int NOT NULL DEFAULT 0,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    version         bigint NOT NULL DEFAULT nextval('item_version_seq'),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);
CREATE INDEX items_list_version_idx ON items (list_id, version);

-- Купленное, убранное кнопкой «Очистить купленное»
CREATE TABLE purchase_history (
    id        bigserial PRIMARY KEY,
    list_id   uuid NOT NULL REFERENCES lists (id) ON DELETE CASCADE,
    item_name text NOT NULL,
    quantity  numeric(10, 3),
    unit      text,
    price     numeric(12, 2),
    bought_by uuid REFERENCES users (id) ON DELETE SET NULL,
    bought_at timestamptz NOT NULL
);
CREATE INDEX purchase_history_list_idx ON purchase_history (list_id, bought_at DESC);

-- +goose Down
DROP TABLE purchase_history;
DROP TABLE items;
DROP SEQUENCE item_version_seq;
DROP TABLE invites;
DROP TABLE list_members;
DROP TABLE lists;
DROP TABLE catalog_items;
DROP TABLE units;
DROP TABLE categories;
DROP TABLE devices;
DROP TABLE refresh_tokens;
DROP TABLE login_codes;
DROP TABLE auth_identities;
DROP TABLE users;
