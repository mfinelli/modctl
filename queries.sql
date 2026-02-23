-- name: GetStoreById :one
SELECT * FROM stores WHERE id = ? LIMIT 1;
