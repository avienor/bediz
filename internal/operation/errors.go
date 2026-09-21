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
