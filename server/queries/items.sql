-- name: NextItemVersion :one
-- Занимает следующую версию списка и блокирует его строку до конца транзакции.
UPDATE lists SET item_seq = item_seq + 1, updated_at = now() WHERE id = $1 AND deleted_at IS NULL
RETURNING item_seq;

-- name: ListItems :many
SELECT i.id, i.list_id, i.catalog_item_id, i.name, i.quantity, i.unit, i.price,
       i.category_id, i.is_bought, i.bought_by, i.bought_at, i.position, i.version, i.updated_at, (i.deleted_at IS NOT NULL)::bool AS deleted
FROM items i
WHERE i.list_id = sqlc.arg(list_id)
  AND i.version > sqlc.arg(since)::bigint
  AND (i.deleted_at IS NULL OR sqlc.arg(include_deleted)::bool)
ORDER BY i.version, i.position, i.id;

-- name: UpsertItem :one
-- Создаёт позицию с id от клиента или заменяет её. Позицию из другого списка или уже удалённую не трогает
-- (тогда строки в ответе нет), поэтому по чужому id нельзя перехватить чужую позицию.
INSERT INTO items AS i (id, list_id, catalog_item_id, name, quantity, unit, price, category_id,
                        is_bought, bought_by, bought_at, position, created_by, version)
VALUES (sqlc.arg(id), sqlc.arg(list_id), sqlc.narg(catalog_item_id), sqlc.arg(name), sqlc.narg(quantity)::numeric,
        sqlc.narg(unit), sqlc.narg(price)::numeric, sqlc.narg(category_id),
        sqlc.arg(is_bought)::bool,
        CASE WHEN sqlc.arg(is_bought)::bool THEN sqlc.arg(actor)::uuid END,
        CASE WHEN sqlc.arg(is_bought)::bool THEN now() END,
        sqlc.arg(position), sqlc.arg(actor)::uuid, sqlc.arg(version))
ON CONFLICT (id) DO UPDATE SET
    catalog_item_id = EXCLUDED.catalog_item_id,
    name = EXCLUDED.name,
    quantity = EXCLUDED.quantity,
    unit = EXCLUDED.unit,
    price = EXCLUDED.price,
    category_id = EXCLUDED.category_id,
    is_bought = EXCLUDED.is_bought,
    bought_by = CASE WHEN EXCLUDED.is_bought THEN COALESCE(CASE WHEN i.is_bought THEN i.bought_by END, EXCLUDED.bought_by) END,
    bought_at = CASE WHEN EXCLUDED.is_bought THEN COALESCE(CASE WHEN i.is_bought THEN i.bought_at END, EXCLUDED.bought_at) END,
    position = EXCLUDED.position,
    version = EXCLUDED.version,
    updated_at = now()
WHERE i.list_id = EXCLUDED.list_id AND i.deleted_at IS NULL
RETURNING i.id, i.list_id, i.catalog_item_id, i.name, i.quantity, i.unit, i.price,
       i.category_id, i.is_bought, i.bought_by, i.bought_at, i.position, i.version, i.updated_at, (i.deleted_at IS NOT NULL)::bool AS deleted;

-- name: PatchItem :one
-- Меняет только поля, для которых set_* = true (иначе клиент не может отличить «не менять» от «очистить»).
UPDATE items i SET
    name        = CASE WHEN sqlc.arg(set_name)::bool THEN sqlc.arg(name)::text ELSE i.name END,
    quantity    = CASE WHEN sqlc.arg(set_quantity)::bool THEN sqlc.narg(quantity)::numeric ELSE i.quantity END,
    unit        = CASE WHEN sqlc.arg(set_unit)::bool THEN sqlc.narg(unit)::text ELSE i.unit END,
    price       = CASE WHEN sqlc.arg(set_price)::bool THEN sqlc.narg(price)::numeric ELSE i.price END,
    category_id = CASE WHEN sqlc.arg(set_category_id)::bool THEN sqlc.narg(category_id)::int ELSE i.category_id END,
    position    = CASE WHEN sqlc.arg(set_position)::bool THEN sqlc.arg(position)::int ELSE i.position END,
    is_bought   = CASE WHEN sqlc.arg(set_bought)::bool THEN sqlc.arg(is_bought)::bool ELSE i.is_bought END,
    bought_by   = CASE WHEN NOT sqlc.arg(set_bought)::bool THEN i.bought_by
                       WHEN NOT sqlc.arg(is_bought)::bool THEN NULL
                       WHEN i.is_bought THEN i.bought_by
                       ELSE sqlc.arg(actor)::uuid END,
    bought_at   = CASE WHEN NOT sqlc.arg(set_bought)::bool THEN i.bought_at
                       WHEN NOT sqlc.arg(is_bought)::bool THEN NULL
                       WHEN i.is_bought THEN i.bought_at
                       ELSE now() END,
    version     = sqlc.arg(version),
    updated_at  = now()
WHERE i.id = sqlc.arg(id) AND i.list_id = sqlc.arg(list_id) AND i.deleted_at IS NULL
RETURNING i.id, i.list_id, i.catalog_item_id, i.name, i.quantity, i.unit, i.price,
       i.category_id, i.is_bought, i.bought_by, i.bought_at, i.position, i.version, i.updated_at, (i.deleted_at IS NOT NULL)::bool AS deleted;

-- name: SoftDeleteItem :execrows
UPDATE items SET deleted_at = now(), version = $3, updated_at = now()
WHERE id = $1 AND list_id = $2 AND deleted_at IS NULL;

-- name: ArchiveBoughtItems :execrows
INSERT INTO purchase_history (list_id, item_name, quantity, unit, price, bought_by, bought_at)
SELECT i.list_id, i.name, i.quantity, i.unit, i.price, i.bought_by, COALESCE(i.bought_at, now())
FROM items i WHERE i.list_id = $1 AND i.is_bought AND i.deleted_at IS NULL;

-- name: SoftDeleteBoughtItems :execrows
UPDATE items SET deleted_at = now(), version = $2, updated_at = now()
WHERE list_id = $1 AND is_bought AND deleted_at IS NULL;
