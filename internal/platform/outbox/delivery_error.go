package outbox

import "errors"

// Permanent marks an invalid delivery that cannot succeed on retry.
func Permanent(err error) error { return permanentError{err} }

type permanentError struct{ error }

func (e permanentError) Unwrap() error { return e.error }
func isPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}
