package manager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrJobBusy               = errors.New("another operation is already running")
	ErrJobStorageUnavailable = errors.New("operation storage is unavailable")
)

const (
	defaultPreviewTTL   = 10 * time.Minute
	JobStateIdle        = "idle"
	JobStateRunning     = "running"
	JobStateCompleted   = "completed"
	JobStatePartial     = "partial"
	JobStateInterrupted = "interrupted"
	JobStateFailed      = "failed"
	maxPreviewEntries   = 1000
)

func randomIdentifier() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func operationStatusFromJobState(state string) string {
	switch state {
	case JobStateRunning:
		return OperationStatusRunning
	case JobStateCompleted:
		return OperationStatusSucceeded
	case JobStatePartial:
		return OperationStatusPartial
	case JobStateInterrupted:
		return OperationStatusInterrupted
	default:
		return OperationStatusFailed
	}
}

func operationReasonFromJobState(state string) string {
	switch state {
	case JobStateCompleted:
		return "completed"
	case JobStatePartial:
		return "partial_failure"
	case JobStateInterrupted:
		return "interrupted"
	case JobStateRunning:
		return ""
	default:
		return "operation_failed"
	}
}
