package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	jsonpatch "github.com/evanphx/json-patch"
	data "github.com/mr-destructive/dummy-json-patch/dummyuser"
	"github.com/mr-destructive/dummy-json-patch/embedsql"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"golang.org/x/crypto/bcrypt"
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
	var userId int64
	if userIdStr != "" {
		userId, _ = strconv.ParseInt(userIdStr, 10, 64)
	}

	if req.HTTPMethod == "GET" {
		if userIdStr != "" {
			if err != nil {
				log.Fatal(err)
			}
			user, err := queries.GetUser(ctx, userId)
			if err != nil {
				log.Fatal(err)
			}
			jsonUser := jsonify(user)
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: jsonUser,
			}, nil
		} else {
			users, err := queries.ListUsers(ctx)
			if err != nil {
				log.Fatal(err)
			}
			jsonUsers := jsonify(users)
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: jsonUsers,
			}, nil
		}
	} else if req.HTTPMethod == "POST" {

		var userPayload UserPayload
		if err := json.Unmarshal([]byte(req.Body), &userPayload); err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusBadRequest,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: err.Error(),
			}, nil
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(userPayload.Password), bcrypt.DefaultCost)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusInternalServerError,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: err.Error(),
			}, nil
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
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusInternalServerError,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: err.Error(),
			}, nil
		}
		createdUser, err := queries.GetUser(context.Background(), int64(user.ID))

		jsonUser := jsonify(createdUser)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: jsonUser,
		}, nil

	} else if req.HTTPMethod == "PATCH" {

		var updates map[string]interface{}
		if err := json.Unmarshal([]byte(req.Body), &updates); err != nil {
			return errorResponse(http.StatusBadRequest, "Invalid JSON"), nil
		}

		var updateParts []string
		var args []interface{}

		allowedColumns := map[string]bool{
			"name":  true,
			"email": true,
			"roles": true,
		}

		for field, value := range updates {
			if !allowedColumns[field] {
				continue
			}
			updateParts = append(updateParts, fmt.Sprintf("%s = ?", field))
			args = append(args, value)
		}

		if len(updateParts) == 0 {
			return errorResponse(http.StatusBadRequest, "No valid fields to update"), nil
		}

		query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updateParts, ", "))
		args = append(args, userId)

		_, err = db.ExecContext(context.Background(), query, args...)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "Failed to update user"), nil
		}

		updatedUser, err := queries.GetUser(context.Background(), userId)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "Failed to get updated user"), nil
		}

		jsonUser := jsonify(updatedUser)

		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: jsonUser,
		}, nil

	} else if req.HTTPMethod == "DELETE" {
		err := queries.DeleteUser(context.Background(), userId)
		if err != nil {
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusInternalServerError,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: err.Error(),
			}, nil
		}
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: "User deleted",
		}, nil
	} else {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: "Method not allowed",
		}, nil
	}
}

func jsonify(user any) string {
	userJson, err := json.Marshal(user)
	if err != nil {
		log.Fatal(err)
	}
	return string(userJson)
}

func errorResponse(statusCode int, message string) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: fmt.Sprintf(`{"error": "%s"}`, message),
	}
}

// Validation helper
func validateUserUpdate(user data.UpdateUserParams) error {
	if user.Email != nil && !isValidEmail(*user.Email) {
		return fmt.Errorf("invalid email format")
	}
	// Add other validation as needed
	return nil
}
