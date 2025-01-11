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
)

var (
	queries *data.Queries
	sqlDB   *sql.DB
)

func main() {
	lambda.Start(handler)
}

func handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ctx := context.Background()
	dbName := os.Getenv("DB_NAME")
	dbToken := os.Getenv("DB_TOKEN")

	var err error
	dbString := fmt.Sprintf("libsql://%s?authToken=%s", dbName, dbToken)
	db, err := sql.Open("libsql", dbString)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Database connection failed"), nil
	}
	defer db.Close()

	queries = data.New(db)
	if _, err := db.ExecContext(ctx, embedsql.DDL); err != nil {
		log.Fatal(err)
	}

	docIDStr := req.QueryStringParameters["id"]
	var docID int64
	if docIDStr != "" {
		docID, _ = strconv.ParseInt(docIDStr, 10, 64)
	}

	switch req.HTTPMethod {
	case "GET":
		return handleGet(ctx, docID)
	case "POST":
		return handlePost(ctx, req.Body)
	case "PUT":
		return handlePut(ctx, docID, req.Body)
	case "PATCH":
		return handlePatch(ctx, docID, req.Body, getHeader(req.Headers, "Content-Type"))
	case "DELETE":
		return handleDelete(ctx, docID)
	default:
		return errorResponse(http.StatusMethodNotAllowed, "Method not allowed"), nil
	}
}

func handleGet(ctx context.Context, docID int64) (events.APIGatewayProxyResponse, error) {
	if docID != 0 {
		doc, err := queries.GetDocument(ctx, docID)
		if err == sql.ErrNoRows {
			return errorResponse(http.StatusNotFound, "Document not found"), nil
		}
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "Failed to fetch document"), nil
		}
		return jsonResponse(http.StatusOK, doc), nil
	}

	docs, err := queries.ListDocuments(ctx)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to fetch documents"), nil
	}
	return jsonResponse(http.StatusOK, docs), nil
}

func handlePost(ctx context.Context, body string) (events.APIGatewayProxyResponse, error) {
	var jsonData json.RawMessage
	if err := json.Unmarshal([]byte(body), &jsonData); err != nil {
		return errorResponse(http.StatusBadRequest, "Invalid JSON"), nil
	}

	doc, err := queries.CreateDocument(ctx, sql.NullString{
		String: body,
		Valid:  true,
	})
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to create document"), nil
	}

	return jsonResponse(http.StatusCreated, doc), nil
}

func handlePut(ctx context.Context, docID int64, body string) (events.APIGatewayProxyResponse, error) {
	var jsonData json.RawMessage
	if err := json.Unmarshal([]byte(body), &jsonData); err != nil {
		return errorResponse(http.StatusBadRequest, "Invalid JSON"), nil
	}

	err := queries.UpdateDocument(ctx, data.UpdateDocumentParams{
		ID: docID,
		Data: sql.NullString{
			String: body,
			Valid:  true,
		},
	})
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to update document"), nil
	}

	doc, err := queries.GetDocument(ctx, docID)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to fetch updated document"), nil
	}

	return jsonResponse(http.StatusOK, doc), nil
}

func handlePatch(ctx context.Context, docID int64, body, contentType string) (events.APIGatewayProxyResponse, error) {
	currentDoc, err := queries.GetDocument(ctx, docID)
	if err == sql.ErrNoRows {
		return errorResponse(http.StatusNotFound, "Document not found"), nil
	}
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to fetch document"), nil
	}

	var currentData map[string]interface{}
	if err := json.Unmarshal([]byte(currentDoc.String), &currentData); err != nil {
		return errorResponse(http.StatusInternalServerError, "Invalid current document JSON"), nil
	}

	if contentType == "application/json-patch+json" {
		var patchOps []jsonpatch.Operation
		if err := json.Unmarshal([]byte(body), &patchOps); err != nil {
			return errorResponse(http.StatusBadRequest, "Invalid JSON Patch"), nil
		}

		for _, op := range patchOps {
			path, err := op.Path()
			if err != nil {
				return errorResponse(http.StatusBadRequest, fmt.Sprintf("Invalid path in operation: %v", err)), nil
			}
			pathSegments := strings.Split(strings.TrimPrefix(path, "/"), "/")

			switch op.Kind() {
			case "add":
				value, err := op.ValueInterface()
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid value in add operation"), nil
				}
				err = handleAdd(currentData, pathSegments, value)

			case "remove":
				err = handleRemove(currentData, pathSegments)

			case "replace":
				value, err := op.ValueInterface()
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid value in replace operation"), nil
				}
				err = handleReplace(currentData, pathSegments, value)

			case "move":
				from, err := getFromPath(op)
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid from path in move operation"), nil
				}
				fromSegments := strings.Split(strings.TrimPrefix(from, "/"), "/")
				err = handleMove(currentData, fromSegments, pathSegments)

			case "copy":
				from, err := getFromPath(op)
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid from path in copy operation"), nil
				}
				fromSegments := strings.Split(strings.TrimPrefix(from, "/"), "/")
				err = handleCopy(currentData, fromSegments, pathSegments)

			case "test":
				value, err := op.ValueInterface()
				if err != nil {
					return errorResponse(http.StatusBadRequest, "Invalid value in test operation"), nil
				}
				err = handleTest(currentData, pathSegments, value)
			}

			if err != nil {
				return errorResponse(http.StatusBadRequest, err.Error()), nil
			}
		}

		updatedJSON, err := json.Marshal(currentData)
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "Failed to marshal updated document"), nil
		}

		err = queries.UpdateDocument(ctx, data.UpdateDocumentParams{
			ID: docID,
			Data: sql.NullString{
				String: string(updatedJSON),
				Valid:  true,
			},
		})
		if err != nil {
			return errorResponse(http.StatusInternalServerError, "Failed to update document"), nil
		}

		return jsonResponse(http.StatusOK, map[string]interface{}{
			"id":   docID,
			"data": currentData,
		}), nil
	} else {
		return handleMergePatch(ctx, docID, body, data.Document{
			ID:   docID,
			Data: sql.NullString{String: currentDoc.String, Valid: true},
		})
	}
}

func handleAdd(data map[string]interface{}, path []string, value interface{}) error {
	if len(path) == 0 {
		return fmt.Errorf("invalid path: empty")
	}

	if len(path) > 1 {
		return setNestedValue(data, path, value)
	}

	data[path[0]] = value
	return nil
}

func handleRemove(data map[string]interface{}, path []string) error {
	if len(path) == 0 {
		return fmt.Errorf("invalid path: empty")
	}

	if len(path) > 1 {
		return removeNestedValue(data, path)
	}

	delete(data, path[0])
	return nil
}

func handleReplace(data map[string]interface{}, path []string, value interface{}) error {
	if len(path) == 0 {
		return fmt.Errorf("invalid path: empty")
	}

	if !pathExists(data, path) {
		return fmt.Errorf("path does not exist: %s", strings.Join(path, "/"))
	}

	if len(path) > 1 {
		return setNestedValue(data, path, value)
	}

	data[path[0]] = value
	return nil
}

func handleMove(data map[string]interface{}, from, to []string) error {
	value, err := getNestedValue(data, from)
	if err != nil {
		return fmt.Errorf("move source not found: %s", strings.Join(from, "/"))
	}

	if err := handleRemove(data, from); err != nil {
		return err
	}

	return handleAdd(data, to, value)
}

func handleCopy(data map[string]interface{}, from, to []string) error {
	value, err := getNestedValue(data, from)
	if err != nil {
		return fmt.Errorf("copy source not found: %s", strings.Join(from, "/"))
	}

	copiedValue := deepCopy(value)

	return handleAdd(data, to, copiedValue)
}

func handleTest(data map[string]interface{}, path []string, value interface{}) error {
	currentValue, err := getNestedValue(data, path)
	if err != nil {
		return fmt.Errorf("test path not found: %s", strings.Join(path, "/"))
	}

	if !reflect.DeepEqual(currentValue, value) {
		return fmt.Errorf("test failed: values do not match at path %s", strings.Join(path, "/"))
	}
	return nil
}

func getFromPath(op jsonpatch.Operation) (string, error) {
	from, err := op.From()
	if err != nil {
		return "", err
	}
	return from, nil
}

func setNestedValue(data map[string]interface{}, path []string, value interface{}) error {
	current := data
	for i := 0; i < len(path)-1; i++ {
		key := path[i]
		if _, ok := current[key]; !ok {
			current[key] = make(map[string]interface{})
		}
		if next, ok := current[key].(map[string]interface{}); ok {
			current = next
		} else {
			return fmt.Errorf("invalid path: %s is not an object", strings.Join(path[:i+1], "/"))
		}
	}
	current[path[len(path)-1]] = value
	return nil
}

func removeNestedValue(data map[string]interface{}, path []string) error {
	current := data
	for i := 0; i < len(path)-1; i++ {
		next, ok := current[path[i]].(map[string]interface{})
		if !ok {
			return fmt.Errorf("path not found: %s", strings.Join(path[:i+1], "/"))
		}
		current = next
	}
	delete(current, path[len(path)-1])
	return nil
}

func getNestedValue(data map[string]interface{}, path []string) (interface{}, error) {
	current := data
	for i := 0; i < len(path)-1; i++ {
		next, ok := current[path[i]].(map[string]interface{})
		log.Printf("next: %v, ok: %v", next, ok)
		if !ok {
			return nil, fmt.Errorf("path not found: %s", strings.Join(path[:i+1], "/"))
		}
		current = next
	}
	value, exists := current[path[len(path)-1]]
	log.Printf("value: %v, exists: %v", value, exists)
	if !exists {
		return nil, fmt.Errorf("path not found: %s", strings.Join(path, "/"))
	}
	return value, nil
}

func pathExists(data map[string]interface{}, path []string) bool {
	_, err := getNestedValue(data, path)
	return err == nil
}

func deepCopy(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v := range v {
			newMap[k] = deepCopy(v)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(v))
		for i, v := range v {
			newSlice[i] = deepCopy(v)
		}
		return newSlice
	default:
		return v
	}
}

func handleMergePatch(ctx context.Context, docID int64, body string, currentDoc data.Document) (events.APIGatewayProxyResponse, error) {
	mergedData, err := jsonpatch.MergePatch([]byte(currentDoc.Data.String), []byte(body))
	if err != nil {
		return errorResponse(http.StatusBadRequest, "Failed to apply merge patch"), nil
	}

	err = queries.UpdateDocument(ctx, data.UpdateDocumentParams{
		ID: docID,
		Data: sql.NullString{
			String: string(mergedData),
			Valid:  true,
		},
	})
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to update document"), nil
	}

	return jsonResponse(http.StatusOK, map[string]interface{}{
		"id":   docID,
		"data": json.RawMessage(mergedData),
	}), nil
}

func handleDelete(ctx context.Context, docID int64) (events.APIGatewayProxyResponse, error) {
	err := queries.DeleteDocument(ctx, docID)
	if err != nil {
		return errorResponse(http.StatusInternalServerError, "Failed to delete document"), nil
	}

	return jsonResponse(http.StatusOK, map[string]string{"message": "Document deleted"}), nil
}

func jsonResponse(statusCode int, data interface{}) events.APIGatewayProxyResponse {
	body, _ := json.Marshal(data)
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(body),
	}
}

func errorResponse(statusCode int, message string) events.APIGatewayProxyResponse {
	return jsonResponse(statusCode, map[string]string{"error": message})
}

func getHeader(headers map[string]string, key string) string {
	if val, ok := headers[key]; ok {
		return val
	}

	lowerKey := strings.ToLower(key)
	for k, v := range headers {
		if strings.ToLower(k) == lowerKey {
			return v
		}
	}
	return ""
}
