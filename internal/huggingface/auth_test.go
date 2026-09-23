package huggingface_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/huggingface"
	"github.com/avienor/bediz/internal/operation"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestLoginRejectsNonLoopbackPlainHTTPBeforeSendingToken(t *testing.T) {
	requests := 0
	client, err := httpclient.New("http://example.org", "", httpclient.Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = huggingface.Login(t.Context(), client, "sentinel-plain-http")
	if _, ok := err.(*operation.InvalidRequestError); !ok || requests != 0 {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestLoginAllowsHTTPSWithPrivateBodyAndNoTokenInResult(t *testing.T) {
	const sentinel = "hf-https-sentinel-307"
	requests := 0
	client, err := httpclient.New("https://example.org", "", httpclient.Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		body := ""
		switch request.URL.Path {
		case "/api/v1/app/version":
			body = `{"version":"6.14.1"}`
		case "/openapi.json":
			body = `{"paths":{"/api/v2/models/hf_login":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Body_do_hf_login"}}}}}}},"components":{"schemas":{"Body_do_hf_login":{"properties":{"token":{"type":"string"}},"required":["token"]}}}}`
		case "/api/v2/models/hf_login":
			if request.Method != http.MethodPost {
				t.Errorf("method=%s", request.Method)
			}
			requestBody, _ := io.ReadAll(request.Body)
			if string(requestBody) != `{"token":"`+sentinel+`"}` {
				t.Errorf("request body=%q", requestBody)
			}
			body = `"valid"`
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: request}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := huggingface.Login(t.Context(), client, sentinel)
	if err != nil || value.Status != "valid" || requests != 3 {
		t.Fatalf("result=%#v error=%v requests=%d", value, err, requests)
	}
}
