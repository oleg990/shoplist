-- name: UpsertDevice :exec
-- Токен уникален: если устройство перешло к другому пользователю, оно переписывается на него.
INSERT INTO devices (user_id, expo_push_token, platform) VALUES ($1, $2, $3)
ON CONFLICT (expo_push_token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, updated_at = now();

-- name: TrimUserDevices :exec
-- Оставляет у пользователя не больше 20 самых свежих устройств.
DELETE FROM devices WHERE id IN (
    SELECT d.id FROM devices d WHERE d.user_id = $1 ORDER BY d.updated_at DESC, d.id OFFSET 20
);

-- name: DeleteUserDevice :execrows
DELETE FROM devices WHERE expo_push_token = $1 AND user_id = $2;

-- name: DeleteDeviceByToken :exec
DELETE FROM devices WHERE expo_push_token = $1;

-- name: ListDevicesForUsers :many
SELECT user_id, expo_push_token FROM devices WHERE user_id = ANY(sqlc.arg(user_ids)::uuid[]);

-- name: GetUserName :one
SELECT name FROM users WHERE id = $1;

-- name: GetListTitle :one
SELECT title FROM lists WHERE id = $1 AND deleted_at IS NULL;

-- name: CountUnboughtItems :one
SELECT count(*)::int FROM items WHERE list_id = $1 AND NOT is_bought AND deleted_at IS NULL;

-- name: ItemExists :one
SELECT EXISTS (SELECT 1 FROM items WHERE id = $1);

-- name: ItemIsBought :one
SELECT is_bought FROM items WHERE id = $1 AND list_id = $2 AND deleted_at IS NULL;
