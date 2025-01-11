-- name: GetUser :one
SELECT id, name, email, bio, roles FROM users WHERE id = ?;

-- name: CreateUser :one
INSERT INTO users (name, email, bio, roles, password_hash) VALUES (?, ?, ?, ?, ?) RETURNING id, name, email, bio, roles;

-- name: UpdateUser :exec
UPDATE users SET name = ?, email = ?, bio = ?, roles = ?  WHERE id = ?;

-- name: ListUsers :many
SELECT id, name, email, bio, roles from users;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;
