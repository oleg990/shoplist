-- name: SearchCatalog :many
-- Поиск по справочнику: сначала совпадения по началу слова, потом похожие (с опечатками).
SELECT ci.id, ci.name, ci.category_id, c.name AS category_name, ci.default_unit
FROM catalog_items ci
LEFT JOIN categories c ON c.id = ci.category_id
WHERE lower(ci.name) LIKE '%' || lower(sqlc.arg(query)::text) || '%'
   OR word_similarity(lower(sqlc.arg(query)::text), lower(ci.name)) > 0.4
ORDER BY (lower(ci.name) LIKE lower(sqlc.arg(query)::text) || '%') DESC,
         word_similarity(lower(sqlc.arg(query)::text), lower(ci.name)) DESC,
         ci.popularity DESC,
         ci.name
LIMIT sqlc.arg(max_results);

-- name: ListCategories :many
SELECT id, name, sort_order FROM categories ORDER BY sort_order, name;
