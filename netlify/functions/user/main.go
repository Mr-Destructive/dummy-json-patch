package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	data "github.com/mr-destructive/dummy-json-patch/dummyuser"
	"github.com/mr-destructive/dummy-json-patch/embedsql"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

type UserPayload struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Roles    string `json:"roles"`
	Password string `json:"password"`
}

var (
	queries *data.Queries
	db      *sql.DB
)

var users = make(map[string]data.User)

func main() {
	lambda.Start(handler)
}

func handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := context.Background()
	dbName := os.Getenv("DB_NAME")
	dbToken := os.Getenv("DB_TOKEN")

	var err error
	dbString := fmt.Sprintf("libsql://%s?authToken=%s", dbName, dbToken)
	db, err = sql.Open("libsql", dbString)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	queries = data.New(db)
	if _, err := db.ExecContext(ctx, embedsql.DDL); err != nil {
		log.Fatal(err)
	}

	userIdStr := req.QueryStringParameters["id"]
	userId, err := strconv.ParseInt(userIdStr, 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	user, err := queries.GetUser(ctx, userId)
	if err != nil {
		log.Fatal(err)
	}

	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: fmt.Sprintf("%+v", user),
	}, nil

}

func getUserhandler(w http.ResponseWriter, r *http.Request, id int) {
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
