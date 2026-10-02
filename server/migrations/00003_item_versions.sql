-- +goose Up
-- Версия позиции берётся из счётчика самого списка. Запись в список берёт блокировку его строки
-- до коммита, поэтому версии внутри списка фиксируются строго по порядку, и клиент, который
-- запрашивает «всё новее версии N», не пропустит транзакцию, закоммиченную позже.
ALTER TABLE lists ADD COLUMN item_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE items ALTER COLUMN version DROP DEFAULT;
DROP SEQUENCE item_version_seq;

-- +goose Down
CREATE SEQUENCE item_version_seq;
ALTER TABLE items ALTER COLUMN version SET DEFAULT nextval('item_version_seq');
ALTER TABLE lists DROP COLUMN item_seq;
