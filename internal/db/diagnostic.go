package db

import "errors"

// recoveryError keeps the database's returned error and unwrap contract while
// exposing both failures to the runtime's structural diagnostic walker.
type recoveryError struct{ original, recovery error }

func (e *recoveryError) Error() string           { return e.original.Error() }
func (e *recoveryError) Unwrap() error           { return e.original }
func (e *recoveryError) DiagnosticSource() error { return errors.Join(e.original, e.recovery) }

func preserveRecovery(err, recovery error) error {
	if recovery == nil {
		return err
	}
	return &recoveryError{original: err, recovery: recovery}
}
