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

-- name: GetDocument :one
SELECT data FROM document WHERE id = ?;

-- name: ListDocuments :many
SELECT id, data FROM document;

-- name: CreateDocument :one
INSERT INTO document (data) VALUES (?) RETURNING id;

-- name: UpdateDocument :exec
UPDATE document SET data = ? WHERE id = ?;

-- name: DeleteDocument :exec
DELETE FROM document WHERE id = ?;
