-- name: CreateList :one
INSERT INTO lists (title, owner_id) VALUES ($1, $2)
RETURNING id, title, owner_id, created_at, updated_at;

-- name: AddListMember :exec
INSERT INTO list_members (list_id, user_id, role) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: ListListsForUser :many
SELECT l.id, l.title, l.owner_id, l.created_at, l.updated_at, lm.role,
       (SELECT count(*) FROM list_members m WHERE m.list_id = l.id)::int AS member_count
FROM lists l
JOIN list_members lm ON lm.list_id = l.id AND lm.user_id = $1
WHERE l.deleted_at IS NULL
ORDER BY l.updated_at DESC, l.created_at DESC;

-- name: GetListForMember :one
SELECT l.id, l.title, l.owner_id, l.created_at, l.updated_at, lm.role,
       (SELECT count(*) FROM list_members m WHERE m.list_id = l.id)::int AS member_count
FROM lists l
JOIN list_members lm ON lm.list_id = l.id AND lm.user_id = sqlc.arg(user_id)
WHERE l.id = sqlc.arg(list_id) AND l.deleted_at IS NULL;

-- name: ListMembers :many
SELECT u.id AS user_id, u.name, u.email, lm.role, lm.joined_at
FROM list_members lm
JOIN users u ON u.id = lm.user_id
WHERE lm.list_id = $1
ORDER BY lm.joined_at, u.id;

-- name: RenameList :exec
UPDATE lists SET title = $2, updated_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: TouchList :exec
UPDATE lists SET updated_at = now() WHERE id = $1;

-- name: SoftDeleteList :exec
UPDATE lists SET deleted_at = now(), updated_at = now() WHERE id = $1;

-- name: RemoveListMember :execrows
DELETE FROM list_members WHERE list_id = $1 AND user_id = $2 AND role <> 'owner';

-- name: CountListMembers :one
SELECT count(*)::int FROM list_members WHERE list_id = $1;

-- name: IsListMember :one
SELECT EXISTS (SELECT 1 FROM list_members WHERE list_id = $1 AND user_id = $2);

-- name: CreateInvite :one
INSERT INTO invites (list_id, code, created_by, expires_at, max_uses)
VALUES ($1, $2, $3, $4, $5)
RETURNING code, expires_at, max_uses, uses;

-- name: GetInviteByCodeForUpdate :one
SELECT i.id, i.list_id, i.expires_at, i.max_uses, i.uses
FROM invites i
JOIN lists l ON l.id = i.list_id AND l.deleted_at IS NULL
WHERE i.code = $1
FOR UPDATE OF i;

-- name: IncrementInviteUses :exec
UPDATE invites SET uses = uses + 1 WHERE id = $1;
