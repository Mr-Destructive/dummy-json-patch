-- name: GetUser :one
SELECT id, name, email, roles FROM users WHERE id = ?;

-- name: CreateUser :one
INSERT INTO users (name, email, roles, password_hash) VALUES (?, ?, ?, ?) RETURNING *;

-- name: UpdateUser :exec
UPDATE users SET name = ?, email = ?, roles = ? WHERE id = ?;

-- name: ListUsers :many
SELECT id, name, email, roles from users;

