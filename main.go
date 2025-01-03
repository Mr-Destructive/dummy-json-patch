package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	data "github.com/mr-destructive/dummy-json-patch/dummyuser"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/crypto/bcrypt"
)

type UserPayload struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Roles    string `json:"roles"`
	Password string `json:"password"`
}

var users = make(map[string]data.User)

//go:embed schema.sql
var ddl string

var (
	queries *data.Queries
	db      *sql.DB
)

func main() {
	ctx := context.Background()
	dbName := os.Getenv("DB_NAME")
	dbToken := os.Getenv("DB_TOKEN")

	var err error
	dbString := fmt.Sprintf("libsql://%s?token=%s", dbName, dbToken)
	db, err = sql.Open("libsql", dbString)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, ddl); err != nil {
		log.Fatal(err)
	}

	queries = data.New(db)
	http.HandleFunc("/users/", usersHandler)
	http.HandleFunc("/users", usersHandler)
	log.Fatal(http.ListenAndServe(":8001", nil))

}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	idStr := ""
	if len(parts) >= 3 && parts[1] == "users" {
		idStr = parts[2]
	}

	var id int
	if idStr != "" {
		var err error
		id, err = strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "Invalid user ID", http.StatusBadRequest)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		getUser(w, r, id)
	case http.MethodPatch:
		patchUser(w, r, id)
	case http.MethodPost:
		fmt.Println("post")
		createUser(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func createUser(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/users" {
		http.Error(w, "Invalid path for creating a user", http.StatusBadRequest)
		return
	}

	var userPayload UserPayload
	err := json.NewDecoder(r.Body).Decode(&userPayload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(userPayload.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Error hashing password", http.StatusInternalServerError)
		return
	}

	user, err := queries.CreateUser(context.Background(), data.CreateUserParams{
		Name:  userPayload.Name,
		Email: userPayload.Email,
		Roles: sql.NullString{
			String: userPayload.Roles,
			Valid:  true,
		},
		PasswordHash: string(hashedPassword),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func getUser(w http.ResponseWriter, r *http.Request, id int) {
	if id == 0 {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	user, err := queries.GetUser(context.Background(), int64(id))
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userResponse := data.User{
		ID:    int64(user.ID),
		Name:  user.Name,
		Email: user.Email,
		Roles: sql.NullString{
			String: user.Roles.String,
			Valid:  true,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse)
}

func patchUser(w http.ResponseWriter, r *http.Request, id int) {
	if id == 0 {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	_, err := queries.GetUser(context.Background(), int64(id))
	if err == sql.ErrNoRows {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var patchOps []jsonpatch.Operation

	if err := json.Unmarshal(response, &patchOps); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
	updateParts := []string{}
	updateArgs := []interface{}{}

	for _, op := range patchOps {
		if op.Kind() != "replace" {
			continue
		}
		path, err := op.Path()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		value, err := op.ValueInterface()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch path {
		case "/name":
			updateParts = append(updateParts, "name = ?")
			updateArgs = append(updateArgs, value)
		case "/email":
			updateParts = append(updateParts, "email = ?")
			updateArgs = append(updateArgs, value)
		case "/roles":
			updateParts = append(updateParts, "roles = ?")
			updateArgs = append(updateArgs, sql.NullString{String: value.(string), Valid: true})
		}
	}

	if len(updateParts) > 0 {
		query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updateParts, ", "))
		updateArgs = append(updateArgs, id)

		_, err = db.ExecContext(context.Background(), query, updateArgs...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		updatedUser, err := queries.GetUser(context.Background(), int64(id))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updatedUser)
	}
}
