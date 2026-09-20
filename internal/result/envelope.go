package result

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const SchemaVersion = 1

const (
	ExitSuccess               = 0
	ExitInvalidRequest        = 2
	ExitSelectionRequired     = 3
	ExitUnsupportedCapability = 4
	ExitConnection            = 5
	ExitInvokeAIFailure       = 6
	ExitInterrupted           = 130
)

type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Warning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Envelope struct {
	SchemaVersion int       `json:"schema_version"`
	OK            bool      `json:"ok"`
	Operation     string    `json:"operation"`
	Data          any       `json:"data,omitempty"`
	Error         *Error    `json:"error,omitempty"`
	Warnings      []Warning `json:"warnings"`
}

func Success(operation string, data any, warnings []Warning) Envelope {
	if data == nil {
		data = map[string]any{}
	}
	if warnings == nil {
		warnings = []Warning{}
	}
	return Envelope{
		SchemaVersion: SchemaVersion,
		OK:            true,
		Operation:     operation,
		Data:          data,
		Warnings:      warnings,
	}
}

func Failure(operation string, err Error, warnings []Warning) Envelope {
	if warnings == nil {
		warnings = []Warning{}
	}
	return Envelope{
		SchemaVersion: SchemaVersion,
		OK:            false,
		Operation:     operation,
		Error:         &err,
		Warnings:      warnings,
	}
}

func (e Envelope) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported envelope schema version %d", e.SchemaVersion)
	}
	if e.Operation == "" {
		return errors.New("operation is required")
	}
	if e.OK && e.Error != nil {
		return errors.New("successful envelope cannot contain an error")
	}
	if !e.OK && e.Error == nil {
		return errors.New("failed envelope must contain an error")
	}
	if e.Error != nil && (e.Error.Code == "" || e.Error.Message == "") {
		return errors.New("error code and message are required")
	}
	return nil
}

func WriteJSON(w io.Writer, envelope Envelope) error {
	if err := envelope.Validate(); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}
