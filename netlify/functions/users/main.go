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

type UserUpdatePayload struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Roles string `json:"roles"`
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
	var merge string
	merge = req.QueryStringParameters["merge"]
	if merge == "" {
		merge = "false"
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
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: string(formatUserResponse(user)),
			}, nil
		} else {
			users, err := queries.ListUsers(ctx)
			if err != nil {
				log.Fatal(err)
			}
			usersJson, err := json.Marshal(users)
			if err != nil {
				log.Fatal(err)
			}
			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: string(usersJson),
			}, nil
		}
	} else if req.HTTPMethod == "POST" {

		var userPayload UserPayload
		if err := json.Unmarshal([]byte(req.Body), &userPayload); err != nil {
			return errorResponse(http.StatusBadRequest, err.Error()), nil
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(userPayload.Password), bcrypt.DefaultCost)
		if err != nil {
			return errorResponse(http.StatusBadRequest, err.Error()), nil
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
			return errorResponse(http.StatusBadRequest, err.Error()), nil
		}
		createdUser, err := queries.GetUser(context.Background(), int64(user.ID))

		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: string(formatUserResponse(createdUser)),
		}, nil
	} else if req.HTTPMethod == "PUT" {

		var userPayload UserUpdatePayload
		if err := json.Unmarshal([]byte(req.Body), &userPayload); err != nil {
			return errorResponse(http.StatusBadRequest, err.Error()), nil
		}

		if err := validateUserUpdate(data.UpdateUserParams{
			Name:  userPayload.Name,
			Email: userPayload.Email,
			Roles: sql.NullString{
				String: userPayload.Roles,
				Valid:  true,
			},
		}); err != nil {
			return errorResponse(http.StatusBadRequest, err.Error()), nil
		}
		err := queries.UpdateUser(context.Background(), data.UpdateUserParams{
			ID:    userId,
			Name:  userPayload.Name,
			Email: userPayload.Email,
			Roles: sql.NullString{
				String: userPayload.Roles,
				Valid:  true,
			},
		})
		if err != nil {
			return errorResponse(http.StatusBadRequest, err.Error()), nil
		}
		updatedUser, err := queries.GetUser(context.Background(), userId)
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: string(formatUserResponse(updatedUser)),
		}, nil

	} else if req.HTTPMethod == "PATCH" {
		fmt.Println(req.Headers["Content-Type"])
		if merge == "false" || req.Headers["Content-Type"] == "application/json-patch+json" {

			existingUser, err := queries.GetUser(context.Background(), userId)
			if err != nil {
				return errorResponse(http.StatusNotFound, "User not found"), nil
			}

			var patchOps []jsonpatch.Operation
			if err := json.Unmarshal([]byte(req.Body), &patchOps); err != nil {
				return errorResponse(http.StatusBadRequest, "Invalid JSON Patch format"), nil
			}

			updateParts := make([]string, 0)
			updateArgs := make([]interface{}, 0)

			allowedPaths := map[string]struct{}{
				"/name":  {},
				"/email": {},
				"/roles": {},
			}

			for _, op := range patchOps {
				if op.Kind() != "replace" {
					return errorResponse(http.StatusBadRequest,
						fmt.Sprintf("Operation '%s' not supported. Only 'replace' is allowed", op.Kind())), nil
				}

				path, err := op.Path()
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid path in patch operation"), nil
				}

				if _, ok := allowedPaths[path]; !ok {
					return errorResponse(http.StatusBadRequest,
						fmt.Sprintf("Path '%s' is not allowed", path)), nil
				}

				value, err := op.ValueInterface()
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid value in patch operation"), nil
				}

				switch path {
				case "/name":
					strValue, ok := value.(string)
					if !ok || strValue == "" {
						return errorResponse(http.StatusBadRequest, "Name must be a non-empty string"), nil
					}
					updateParts = append(updateParts, "name = ?")
					updateArgs = append(updateArgs, strValue)

				case "/email":
					strValue, ok := value.(string)
					if !ok || strValue == "" {
						return errorResponse(http.StatusBadRequest, "Invalid email format"), nil
					}
					updateParts = append(updateParts, "email = ?")
					updateArgs = append(updateArgs, strValue)

				case "/roles":
					strValue, ok := value.(string)
					if !ok {
						return errorResponse(http.StatusBadRequest, "Roles must be a string"), nil
					}
					updateParts = append(updateParts, "roles = ?")
					updateArgs = append(updateArgs, sql.NullString{String: strValue, Valid: true})
				}
			}

			if len(updateParts) == 0 {
				return events.APIGatewayProxyResponse{
					StatusCode: http.StatusOK,
					Headers:    map[string]string{"Content-Type": "application/json"},
					Body:       string(formatUserResponse(existingUser)),
				}, nil
			}

			query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updateParts, ", "))
			updateArgs = append(updateArgs, userId)

			if _, err := db.ExecContext(context.Background(), query, updateArgs...); err != nil {
				return errorResponse(http.StatusInternalServerError, "Failed to update user"), nil
			}

			updatedUser, err := queries.GetUser(context.Background(), userId)
			if err != nil {
				return errorResponse(http.StatusInternalServerError, "Failed to fetch updated user"), nil
			}

			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       string(formatUserResponse(updatedUser)),
			}, nil

		} else {
			var updates map[string]interface{}
			if err := json.Unmarshal([]byte(req.Body), &updates); err != nil {
				return errorResponse(http.StatusBadRequest, err.Error()), nil
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

			return events.APIGatewayProxyResponse{
				StatusCode: http.StatusOK,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
				Body: string(formatUserResponse(updatedUser)),
			}, nil
		}

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

func validateUserUpdate(user data.UpdateUserParams) error {
	if user.Email != "" && !strings.Contains(user.Email, "@") {
		return fmt.Errorf("invalid email format")
	}
	return nil
}

func formatUserResponse(user data.GetUserRow) []byte {
	response := struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Roles string `json:"roles"`
	}{
		ID:    user.ID,
		Name:  user.Name,
		Email: user.Email,
		Roles: user.Roles.String,
	}

	bytes, _ := json.Marshal(response)
	return bytes
}
