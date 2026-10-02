-- name: Notify :exec
-- Рассылка через Postgres LISTEN/NOTIFY: в транзакции доставляется только после коммита.
SELECT pg_notify('list_changes', sqlc.arg(payload)::text);
