// Package document decodes strict V1 JSON documents at local input boundaries.
package document

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// options retain V1 value decoding while preserving typed errors with field
// locations and rejecting unknown or repeated members at every nesting depth.
var options = jsonv2.JoinOptions(json.DefaultOptionsV1(),
	json.ReportErrorsWithLegacySemantics(false), jsonv2.MatchCaseInsensitiveNames(false),
	jsontext.AllowDuplicateNames(false), jsonv2.RejectUnknownMembers(true))

// FieldError identifies the document field that failed validation. Field uses
// document member names joined by dots, with numeric array indexes.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }
func (e *FieldError) Unwrap() error { return e.Err }

// Decode accepts exactly one object and rejects null members and list elements.
// Subject names the document in parsing errors, such as "request document".
func Decode(subject string, reader io.Reader, target any) error {
	decoder := jsontext.NewDecoder(reader, options)
	value, err := decoder.ReadValue()
	if err != nil {
		return decodeError(subject, err)
	}
	if value.Kind() != '{' {
		return fmt.Errorf("%s must be a JSON object", subject)
	}
	value = value.Clone()
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err != nil {
			return decodeError(subject, err)
		}
		return fmt.Errorf("%s must contain exactly one JSON value", subject)
	}
	if err := rejectNulls(value); err != nil {
		return err
	}
	if err := jsonv2.Unmarshal(value, target, options); err != nil {
		return decodeError(subject, err)
	}
	return nil
}

func decodeError(subject string, err error) error {
	wrapped := fmt.Errorf("decode %s: %w", subject, err)
	var pointer jsontext.Pointer
	if semantic, ok := errors.AsType[*jsonv2.SemanticError](err); ok {
		pointer = semantic.JSONPointer
	} else if syntax, ok := errors.AsType[*jsontext.SyntacticError](err); ok {
		pointer = syntax.JSONPointer
	}
	if pointer != "" {
		return &FieldError{Field: fieldPath(pointer), Err: wrapped}
	}
	return wrapped
}

// rejectNulls reports the first explicit null member or list element in input
// order, so an optional field is always omitted rather than null.
func rejectNulls(value jsontext.Value) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(value), options)
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return decodeError("document", err)
		}
		if token.Kind() != 'n' {
			continue
		}
		pointer := decoder.StackPointer()
		switch parent, _ := decoder.StackIndex(decoder.StackDepth()); parent {
		case '{':
			return &FieldError{Field: fieldPath(pointer), Err: fmt.Errorf("field %q cannot be null", fieldPath(pointer))}
		case '[':
			return &FieldError{Field: fieldPath(pointer), Err: fmt.Errorf("field %q cannot contain null", fieldPath(pointer.Parent()))}
		}
	}
}

func fieldPath(pointer jsontext.Pointer) string {
	return strings.Join(slices.Collect(pointer.Tokens()), ".")
}
