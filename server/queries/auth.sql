-- name: CreateLoginCode :exec
INSERT INTO login_codes (email, code_hash, expires_at) VALUES (lower(sqlc.arg(email)::text), $1, $2);

-- name: LatestLoginCodeCreatedAt :one
SELECT created_at FROM login_codes WHERE email = lower(sqlc.arg(email)::text) ORDER BY created_at DESC LIMIT 1;

-- name: GetActiveLoginCodeForUpdate :one
SELECT id, code_hash, attempts FROM login_codes
WHERE email = lower(sqlc.arg(email)::text) AND expires_at > now()
ORDER BY created_at DESC LIMIT 1
FOR UPDATE;

-- name: IncrementLoginCodeAttempts :exec
UPDATE login_codes SET attempts = attempts + 1 WHERE id = $1;

-- name: DeleteLoginCodesByEmail :exec
DELETE FROM login_codes WHERE email = lower(sqlc.arg(email)::text);

-- name: DeleteExpiredLoginCodes :exec
DELETE FROM login_codes WHERE expires_at < now() - interval '1 day';

-- name: UpsertUserByEmail :one
INSERT INTO users (email) VALUES (lower(sqlc.arg(email)::text))
ON CONFLICT ((lower(email))) WHERE deleted_at IS NULL DO UPDATE SET email = users.email
RETURNING id, email, name, created_at;

-- name: InsertEmailIdentity :exec
INSERT INTO auth_identities (user_id, provider, provider_uid)
VALUES ($1, 'email', lower(sqlc.arg(email)::text))
ON CONFLICT DO NOTHING;

-- name: GetUser :one
SELECT id, email, name, created_at FROM users WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserName :one
UPDATE users SET name = $2 WHERE id = $1 AND deleted_at IS NULL
RETURNING id, email, name, created_at;

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
