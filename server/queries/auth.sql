-- name: CreateUser :one
INSERT INTO users (username, name) VALUES (sqlc.arg(username)::text, sqlc.arg(name)::text)
RETURNING id, username, name, created_at;

-- name: CreateCredentials :exec
INSERT INTO credentials (user_id, password_hash) VALUES ($1, $2);

-- name: GetUserByUsername :one
SELECT id, username, name, created_at FROM users WHERE lower(username) = lower(sqlc.arg(username)::text) AND deleted_at IS NULL;

-- name: GetCredentialsForUpdate :one
SELECT password_hash, failed_attempts, locked_until FROM credentials WHERE user_id = $1 FOR UPDATE;

-- name: RecordLoginFailure :exec
-- После max_attempts подряд неудач вход блокируется на lock_for; счётчик сбрасывается при блокировке.
UPDATE credentials SET
    failed_attempts = CASE WHEN failed_attempts + 1 >= sqlc.arg(max_attempts)::int THEN 0 ELSE failed_attempts + 1 END,
    locked_until = CASE WHEN failed_attempts + 1 >= sqlc.arg(max_attempts)::int THEN now() + make_interval(secs => sqlc.arg(lock_secs)::float8) ELSE locked_until END
WHERE user_id = sqlc.arg(user_id);

-- name: ResetLoginFailures :exec
UPDATE credentials SET failed_attempts = 0, locked_until = NULL WHERE user_id = $1;

-- name: SetPassword :exec
UPDATE credentials SET password_hash = $2, failed_attempts = 0, locked_until = NULL, updated_at = now() WHERE user_id = $1;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: UseRecoveryCode :one
-- Помечает код использованным; пусто, если кода нет или он уже использован.
UPDATE recovery_codes SET used_at = now()
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
RETURNING id;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: GetUser :one
SELECT id, username, name, created_at FROM users WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserName :one
UPDATE users SET name = $2 WHERE id = $1 AND deleted_at IS NULL
RETURNING id, username, name, created_at;

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3);

-- name: GetRefreshTokenForUpdate :one
SELECT id, user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenByHash :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: TransferOwnedLists :exec
-- Перед удалением аккаунта: списки, где есть другие участники, переходят самому раннему из них.
WITH new_owner AS (
    SELECT DISTINCT ON (lm.list_id) lm.list_id, lm.user_id
    FROM list_members lm
    JOIN lists l ON l.id = lm.list_id
    WHERE l.owner_id = $1 AND lm.user_id <> $1
    ORDER BY lm.list_id, lm.joined_at
), moved AS (
    UPDATE lists l SET owner_id = n.user_id
    FROM new_owner n WHERE l.id = n.list_id
    RETURNING l.id AS list_id, n.user_id
)
UPDATE list_members lm SET role = 'owner'
FROM moved WHERE lm.list_id = moved.list_id AND lm.user_id = moved.user_id;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
