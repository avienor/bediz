package httpclient_test

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
)

func TestPrivateMutationOmitsInvokeAIDetail(t *testing.T) {
	const token = "private-token-sentinel"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != token {
			t.Error("private mutation did not reach the backend")
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":[{"loc":["body","token"],"msg":"rejected private-token-sentinel","input":"private-token-sentinel"}]}`))
	}))
	t.Cleanup(server.Close)
	client, err := httpclient.New(server.URL, "", httpclient.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.DoJSONPrivate(t.Context(), http.MethodPost, "/install?access_token="+token, map[string]any{"token": token}, nil)
	failure, ok := errors.AsType[*httpclient.HTTPError](err)
	if !ok || failure.StatusCode != http.StatusUnprocessableEntity || failure.Body != "" || failure.Detail != nil {
		t.Fatal("private mutation did not return a conclusive error without body or detail")
	}
	encoded, encodeErr := json.Marshal(failure)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	for _, private := range []string{token, "access_token", "rejected", "/install"} {
		if strings.Contains(err.Error()+string(encoded), private) {
			t.Fatal("private request data leaked through the public error")
		}
	}
}
