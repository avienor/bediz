package operation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type InvalidRequestError struct {
	Message string
}

func (e *InvalidRequestError) Error() string { return e.Message }

func InvalidRequest(message string) error {
	return &InvalidRequestError{Message: message}
}

type AuthenticationRequiredError struct {
	Message string
}

func (e *AuthenticationRequiredError) Error() string { return e.Message }

type UnsupportedCapabilityError struct {
	Message string
}

func (e *UnsupportedCapabilityError) Error() string { return e.Message }

func UnsupportedCapability(message string) error {
	return &UnsupportedCapabilityError{Message: message}
}

type SelectionCandidate struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Base string `json:"base"`
	Type string `json:"type"`
}

type SelectionRequiredError struct {
	Kind       string
	Selector   string
	Candidates []SelectionCandidate
}

type CivitaiFileCandidate struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
}

type CivitaiFileSelectionError struct {
	VersionID  int
	Candidates []CivitaiFileCandidate
}

func (*CivitaiFileSelectionError) Error() string {
	return "Civitai version requires one exact file selection"
}

type CivitaiVersionCandidate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type CivitaiVersionSelectionError struct {
	ModelID    int
	Candidates []CivitaiVersionCandidate
}

func (*CivitaiVersionSelectionError) Error() string {
	return "Civitai model page requires one exact version selection"
}

type BoardCandidate struct {
	BoardID   string `json:"board_id"`
	BoardName string `json:"board_name"`
}

// BoardSelectionError reports a board name shared by several visible boards.
// Candidates are sorted by board identifier.
type BoardSelectionError struct {
	Selector   string
	Candidates []BoardCandidate
}

func (*BoardSelectionError) Error() string {
	return "board name requires one exact board selection"
}

// BoardNameExistsError reports visible boards that already have the exact name
// a create request asked for. BoardIDs are sorted by board identifier.
type BoardNameExistsError struct {
	BoardName string
	BoardIDs  []string
}

func (e *BoardNameExistsError) Error() string {
	return fmt.Sprintf("a visible board is already named %q", e.BoardName)
}

// NotFoundError reports that a selector resolved to no visible InvokeAI
// resource.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

func NotFound(message string) error {
	return &NotFoundError{Message: message}
}

func (e *SelectionRequiredError) Error() string {
	return "model selector requires one exact selection"
}

func SelectionRequired(kind, selector string, candidates []SelectionCandidate) error {
	return &SelectionRequiredError{Kind: kind, Selector: selector, Candidates: candidates}
}

type MissingComponentError struct {
	ComponentType        string
	RequiredBase         string
	RequiredType         string
	InstallationGuidance string
}

func (e *MissingComponentError) Error() string {
	return fmt.Sprintf("no compatible %s is installed; %s", e.ComponentType, e.InstallationGuidance)
}

func MissingComponent(componentType, requiredBase, requiredType, installationGuidance string) error {
	return &MissingComponentError{
		ComponentType: componentType, RequiredBase: requiredBase, RequiredType: requiredType,
		InstallationGuidance: installationGuidance,
	}
}

// QueuePosition identifies accepted remote work: the InvokeAI queue, batch, and
// ordered item identifiers returned by a conclusive enqueue. Failures that
// happen after acceptance report it so callers can inspect or continue the
// remote work instead of submitting it again.
type QueuePosition struct {
	QueueID string `json:"queue_id"`
	BatchID string `json:"batch_id"`
	ItemIDs []int  `json:"item_ids"`
}

// describe names the accepted items in a short human-readable phrase.
func (p QueuePosition) describe() string {
	switch len(p.ItemIDs) {
	case 0:
		return "the accepted queue item"
	case 1:
		return fmt.Sprintf("queue item %d", p.ItemIDs[0])
	}
	identifiers := make([]string, 0, len(p.ItemIDs))
	for _, itemID := range p.ItemIDs {
		identifiers = append(identifiers, strconv.Itoa(itemID))
	}
	return "queue items " + strings.Join(identifiers, ", ")
}

// WaitTimeoutError reports that a caller-supplied wait deadline elapsed before
// accepted remote work reached a terminal state. The remote item was not
// canceled.
type WaitTimeoutError struct {
	Position QueuePosition
}

func (e *WaitTimeoutError) Error() string {
	return "wait timeout elapsed before " + e.Position.describe() + " reached a terminal state; the InvokeAI item was not canceled"
}

// InterruptedError reports that local interruption stopped Bediz while accepted
// remote work continued. It unwraps to context.Canceled.
type InterruptedError struct {
	Position QueuePosition
}

func (e *InterruptedError) Error() string {
	return "waiting for " + e.Position.describe() + " was interrupted locally; the InvokeAI item was not canceled"
}

func (e *InterruptedError) Unwrap() error { return context.Canceled }

// ItemFailureError reports accepted work that InvokeAI concluded with a failed
// or canceled queue item. Its message is concise and normalized: server
// tracebacks are never included.
type ItemFailureError struct {
	Position       QueuePosition
	ItemID         int
	Status         string
	FailureType    string
	FailureMessage string
}

func (e *ItemFailureError) Error() string {
	message := fmt.Sprintf("queue item %d reached terminal status %q", e.ItemID, e.Status)
	if e.FailureType != "" {
		message += " (" + e.FailureType + ")"
	}
	if e.FailureMessage != "" {
		message += ": " + e.FailureMessage
	}
	return message
}

// InvalidQueueResultError reports a queue item whose normalized result
// contradicts the tested InvokeAI contract, such as an unrecognized status or a
// completed item without exactly one image output.
type InvalidQueueResultError struct {
	Position QueuePosition
	ItemID   int
	Status   string
	Detail   string
}

func (e *InvalidQueueResultError) Error() string {
	return fmt.Sprintf("queue item %d %s", e.ItemID, e.Detail)
}
