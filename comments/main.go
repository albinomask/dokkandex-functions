package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/fauna/faunadb-go/v4/faunadb"
	"github.com/go-playground/validator/v10"
	"golang.org/x/crypto/sha3"
)

func handler(ctx context.Context, request events.APIGatewayProxyRequest) (*events.APIGatewayProxyResponse, error) {
	switch request.HTTPMethod {
	case http.MethodPost:
		return Post(request)
	}

	return ServeError(http.StatusMethodNotAllowed, "method now allowed"), nil
}

func main() {
	lambda.Start(handler)
}

type Comment struct {
	ID string `json:"id" fauna:"-"`
	Name string `json:"name" fauna:"name" validate:"required,max=16"`
	Password string `json:"password" fauna:"password" validate:"required,min=4,max=32"`
	Content string `json:"content" fauna:"content" validate:"required,max=200"`
	CreatedAt time.Time `json:"created_at" fauna:"created_at"`
}

func Post(request events.APIGatewayProxyRequest) (*events.APIGatewayProxyResponse, error) {
	comment := &Comment{}
	err := json.Unmarshal([]byte(request.Body), comment)
	if err != nil {
		return ServeError(http.StatusBadRequest, "failed to parse json body"), nil
	}

	if err = validator.New().Struct(comment); err != nil {
		return ServeError(http.StatusBadRequest, err.Error()), nil
	}

	comment.CreatedAt = time.Now()

	hashSrc := make([]byte, 16 + len([]byte(comment.Password)))
	if _, err = rand.New(rand.NewSource(time.Now().UnixNano())).Read(hashSrc[:16]); err != nil {
		fmt.Println(err)
		return ServeError(http.StatusInternalServerError, "unknown error"), nil
	}

	hash := sha3.Sum256(hashSrc)
	passwordData := make([]byte, 48)
	copy(passwordData[:16], hashSrc[:16])
	copy(passwordData[16:], hash[:])
	comment.Password = base64.StdEncoding.EncodeToString(passwordData)

	fauna := faunadb.NewFaunaClient(
		os.Getenv("FAUNADB_SECRET"),
		faunadb.Endpoint(os.Getenv("FAUNADB_ENDPOINT")),
	)

	val, err := fauna.Query(faunadb.Create(faunadb.Collection("comments"), faunadb.Obj{"data": comment}))
	if err != nil {
		fmt.Println(err)
		return ServeError(http.StatusInternalServerError, "unknown error"), nil
	}

	var ref faunadb.RefV
	if err = val.At(faunadb.ObjKey("ref")).Get(&ref); err != nil {
		fmt.Println(err)
		return ServeError(http.StatusInternalServerError, "unknown error"), nil
	}

	comment.ID = ref.ID

	return ServeJSON(http.StatusOK, comment), nil
}

func ServeError(statusCode int, message string) *events.APIGatewayProxyResponse {
	return ServeJSON(statusCode, map[string]interface{}{"message": message})
}

func ServeJSON(statusCode int, body interface{}) (*events.APIGatewayProxyResponse,) {
	data, err := json.Marshal(body)
	if err != nil {
		return ServeError(http.StatusBadGateway, "unknown error")
	}

	return &events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body: string(data),
	}
}