package operation

type InvalidRequestError struct {
	Message string
}

func (e *InvalidRequestError) Error() string { return e.Message }

func InvalidRequest(message string) error {
	return &InvalidRequestError{Message: message}
}

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

func (e *SelectionRequiredError) Error() string {
	return "model selector requires one exact selection"
}

func SelectionRequired(kind, selector string, candidates []SelectionCandidate) error {
	return &SelectionRequiredError{Kind: kind, Selector: selector, Candidates: candidates}
}
