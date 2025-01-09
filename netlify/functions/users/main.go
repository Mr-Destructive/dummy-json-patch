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

		_, err := queries.GetUser(context.Background(), userId)
		if err == sql.ErrNoRows {
		} else if err != nil {
		}

		if err != nil {
		}
		var patchOps []jsonpatch.Operation

		if err := json.Unmarshal([]byte(req.Body), &patchOps); err != nil {
		}
		updateParts := []string{}
		updateArgs := []interface{}{}

		for _, op := range patchOps {
			if op.Kind() != "replace" {
				continue
			}
			path, err := op.Path()
			if err != nil {
				return events.APIGatewayProxyResponse{
					StatusCode: http.StatusBadRequest,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
					Body: err.Error(),
				}, nil
			}
			value, err := op.ValueInterface()
			if err != nil {
				return events.APIGatewayProxyResponse{
					StatusCode: http.StatusBadRequest,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
					Body: err.Error(),
				}, nil
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
			updateArgs = append(updateArgs, userId)

			_, err = db.ExecContext(context.Background(), query, updateArgs...)
			if err != nil {
			}

			updatedUser, err := queries.GetUser(context.Background(), int64(userId))
			if err != nil {
				return events.APIGatewayProxyResponse{
					StatusCode: http.StatusInternalServerError,
					Headers: map[string]string{
						"Content-Type": "application/json",
					},
					Body: err.Error(),
				}, nil
			}
			type userJson struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
				Roles string `json:"roles"`
			}
			var userJsonBody userJson
			userJsonBody.ID = updatedUser.ID
			userJsonBody.Name = updatedUser.Name
			userJsonBody.Email = updatedUser.Email
			userJsonBody.Roles = updatedUser.Roles.String

			updatedUserJson, err := json.Marshal(userJsonBody)
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
				Body: string(updatedUserJson),
			}, nil
		}

		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: "",
		}, nil

	} else if req.HTTPMethod == "DELETE" {
	} else {
		return events.APIGatewayProxyResponse{
			StatusCode: http.StatusMethodNotAllowed,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: "Method not allowed",
		}, nil
	}

	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: fmt.Sprintf("%+v", users),
	}, nil

}

func jsonify(user any) string {
	userJson, err := json.Marshal(user)
	if err != nil {
		log.Fatal(err)
	}
	return string(userJson)
}
