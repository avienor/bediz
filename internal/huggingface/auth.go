package huggingface

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

const endpoint = capability.HuggingFaceAuthEndpoint

type Result struct {
	Status string `json:"status"`
}

type RejectedTokenError struct{}

func (*RejectedTokenError) Error() string { return "Hugging Face rejected the token" }

type UnchangedStateError struct{}

func (*UnchangedStateError) Error() string { return "InvokeAI did not clear the Hugging Face token" }

func Status(ctx context.Context, client *httpclient.Client) (Result, error) {
	var status string
	if err := client.GetJSON(ctx, endpoint, &status); err != nil {
		return Result{}, err
	}
	return normalized(status)
}

func Login(ctx context.Context, client *httpclient.Client, token string) (Result, error) {
	if token == "" {
		return Result{}, operation.InvalidRequest("Hugging Face token must not be empty")
	}
	if !client.AllowsSourceToken() {
		return Result{}, operation.InvalidRequest("Hugging Face login requires HTTPS or a loopback InvokeAI target")
	}
	if err := compatible(ctx, client, http.MethodPost); err != nil {
		return Result{}, err
	}
	var status string
	if err := client.DoJSONPrivate(ctx, http.MethodPost, endpoint, struct {
		Token string `json:"token"`
	}{Token: token}, &status); err != nil {
		return Result{}, err
	}
	result, err := normalized(status)
	if err != nil {
		return Result{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, Err: err}
	}
	switch result.Status {
	case "valid":
		return result, nil
	case "invalid":
		return Result{}, &RejectedTokenError{}
	default:
		return Result{}, &httpclient.OutcomeUnknownError{Method: http.MethodPost, Err: errors.New("Hugging Face token verification was inconclusive")}
	}
}

func Logout(ctx context.Context, client *httpclient.Client) (Result, error) {
	if err := compatible(ctx, client, http.MethodDelete); err != nil {
		return Result{}, err
	}
	var status string
	if err := client.DoJSONPrivate(ctx, http.MethodDelete, endpoint, nil, &status); err != nil {
		return Result{}, err
	}
	result, err := normalized(status)
	if err != nil {
		return Result{}, &httpclient.OutcomeUnknownError{Method: http.MethodDelete, Err: err}
	}
	switch result.Status {
	case "invalid":
		return result, nil
	case "valid":
		return Result{}, &UnchangedStateError{}
	default:
		return Result{}, &httpclient.OutcomeUnknownError{Method: http.MethodDelete, Err: errors.New("Hugging Face logout verification was inconclusive")}
	}
}

func normalized(status string) (Result, error) {
	switch status {
	case "valid", "invalid", "unknown":
		return Result{Status: status}, nil
	default:
		return Result{}, &httpclient.InvalidResponseError{Err: errors.New("unknown Hugging Face token status")}
	}
}

func compatible(ctx context.Context, client *httpclient.Client, method string) error {
	if err := capability.RequireSupportedVersion(ctx, client); err != nil {
		return err
	}
	var document struct {
		Paths      map[string]map[string]jsontext.Value `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := client.GetJSON(ctx, "/openapi.json", &document); err != nil {
		return err
	}
	endpointMethods := document.Paths[endpoint]
	if _, ok := endpointMethods[lowerMethod(method)]; !ok {
		return operation.UnsupportedCapability("InvokeAI Hugging Face authentication endpoint is unavailable")
	}
	if method == http.MethodPost {
		schema := document.Components.Schemas["Body_do_hf_login"]
		if !capability.HasHuggingFaceTokenBody(endpointMethods["post"], schema.Properties["token"].Type, schema.Required) {
			return operation.UnsupportedCapability("InvokeAI Hugging Face login token schema is incompatible")
		}
	}
	return nil
}

func lowerMethod(method string) string {
	if method == http.MethodPost {
		return "post"
	}
	return "delete"
}
