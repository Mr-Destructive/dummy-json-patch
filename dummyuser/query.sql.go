// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: query.sql

package dummyuser

import (
	"context"
	"database/sql"
)

const createUser = `-- name: CreateUser :one
INSERT INTO users (name, email, roles, password_hash) VALUES (?, ?, ?, ?) RETURNING id, name, email, roles, password_hash
`

type CreateUserParams struct {
	Name         string
	Email        string
	Roles        sql.NullString
	PasswordHash string
}

func (q *Queries) CreateUser(ctx context.Context, arg CreateUserParams) (User, error) {
	row := q.db.QueryRowContext(ctx, createUser,
		arg.Name,
		arg.Email,
		arg.Roles,
		arg.PasswordHash,
	)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Email,
		&i.Roles,
		&i.PasswordHash,
	)
	return i, err
}

const getUser = `-- name: GetUser :one
SELECT id, name, email, roles FROM users WHERE id = ?
`

type GetUserRow struct {
	ID    int64
	Name  string
	Email string
	Roles sql.NullString
}

func (q *Queries) GetUser(ctx context.Context, id int64) (GetUserRow, error) {
	row := q.db.QueryRowContext(ctx, getUser, id)
	var i GetUserRow
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Email,
		&i.Roles,
	)
	return i, err
}

const listUsers = `-- name: ListUsers :many
SELECT id, name, email, roles from users
`

type ListUsersRow struct {
	ID    int64
	Name  string
	Email string
	Roles sql.NullString
}

func (q *Queries) ListUsers(ctx context.Context) ([]ListUsersRow, error) {
	rows, err := q.db.QueryContext(ctx, listUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ListUsersRow
	for rows.Next() {
		var i ListUsersRow
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.Email,
			&i.Roles,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateUser = `-- name: UpdateUser :exec
UPDATE users SET name = ?, email = ?, roles = ? WHERE id = ?
`

type UpdateUserParams struct {
	Name  string
	Email string
	Roles sql.NullString
	ID    int64
}

func (q *Queries) UpdateUser(ctx context.Context, arg UpdateUserParams) error {
	_, err := q.db.ExecContext(ctx, updateUser,
		arg.Name,
		arg.Email,
		arg.Roles,
		arg.ID,
	)
	return err
}
