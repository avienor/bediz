package result

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSuccessEnvelopeHasStableShape(t *testing.T) {
	var output bytes.Buffer
	if err := WriteJSON(&output, Success("doctor", map[string]any{"ready": true}, nil)); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not one JSON object: %v", err)
	}
	if decoded["schema_version"] != float64(1) || decoded["ok"] != true || decoded["operation"] != "doctor" {
		t.Fatalf("unexpected envelope: %#v", decoded)
	}
	warnings, ok := decoded["warnings"].([]any)
	if !ok || len(warnings) != 0 {
		t.Fatalf("warnings must be an empty array: %#v", decoded["warnings"])
	}
	if _, exists := decoded["error"]; exists {
		t.Fatal("success envelope contains error")
	}
}

func TestFailureEnvelopeHasNoData(t *testing.T) {
	var output bytes.Buffer
	envelope := Failure("doctor", Error{Code: "connection_failed", Message: "offline"}, nil)
	if err := WriteJSON(&output, envelope); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["data"]; exists {
		t.Fatal("failure envelope contains data")
	}
	if decoded["ok"] != false {
		t.Fatalf("unexpected envelope: %#v", decoded)
	}
}

func TestValidateRejectsContradictoryEnvelope(t *testing.T) {
	envelope := Success("doctor", nil, nil)
	envelope.Error = &Error{Code: "bad", Message: "bad"}
	if err := envelope.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSuccessWithNilDataWritesObject(t *testing.T) {
	var output bytes.Buffer
	if err := WriteJSON(&output, Success("empty", nil, nil)); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["data"].(map[string]any); !ok {
		t.Fatalf("data = %#v, want object", decoded["data"])
	}
}
