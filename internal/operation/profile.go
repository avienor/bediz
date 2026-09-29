package operation

import "github.com/avienor/bediz/internal/result"

// ProfilePreferenceError retains skipped component preferences when a later
// step fails before an accepted enqueue.
type ProfilePreferenceError struct {
	Err      error
	Warnings []result.Warning
}

func (e *ProfilePreferenceError) Error() string { return e.Err.Error() }
func (e *ProfilePreferenceError) Unwrap() error { return e.Err }
