// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestErrorCodesAreUnique verifies all error codes are unique.
func TestErrorCodesAreUnique(t *testing.T) {
	codes := []string{
		ErrCodeJobNotFound,
		ErrCodeJobAlreadyExists,
		ErrCodeInvalidJob,
		ErrCodeInvalidSchedule,
		ErrCodeHandlerNotFound,
		ErrCodeHandlerFailed,
		ErrCodeJobTimeout,
		ErrCodeMaxRetriesExceeded,
		ErrCodeSchedulerStopped,
		ErrCodeUnauthorized,
		ErrCodeStorageError,
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "duplicate error code: %s", code)
		seen[code] = true
	}
	assert.Len(t, seen, len(codes))
}

// TestErrorCodesNotEmpty verifies all error codes are non-empty strings.
func TestErrorCodesNotEmpty(t *testing.T) {
	t.Run("AllCodesNonEmpty", func(t *testing.T) {
		assert.NotEmpty(t, ErrCodeJobNotFound)
		assert.NotEmpty(t, ErrCodeJobAlreadyExists)
		assert.NotEmpty(t, ErrCodeInvalidJob)
		assert.NotEmpty(t, ErrCodeInvalidSchedule)
		assert.NotEmpty(t, ErrCodeHandlerNotFound)
		assert.NotEmpty(t, ErrCodeHandlerFailed)
		assert.NotEmpty(t, ErrCodeJobTimeout)
		assert.NotEmpty(t, ErrCodeMaxRetriesExceeded)
		assert.NotEmpty(t, ErrCodeSchedulerStopped)
		assert.NotEmpty(t, ErrCodeUnauthorized)
		assert.NotEmpty(t, ErrCodeStorageError)
	})
}

// TestSchedulerError tests the base SchedulerError type.
func TestSchedulerError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &SchedulerError{
			Code:    ErrCodeStorageError,
			Op:      "SaveJob",
			Message: "failed to save job",
			Err:     cause,
		}

		errorStr := err.Error()
		assert.Contains(t, errorStr, ErrCodeStorageError)
		assert.Contains(t, errorStr, "SaveJob")
		assert.Contains(t, errorStr, "failed to save job")
		assert.Contains(t, errorStr, "underlying error")
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := &SchedulerError{
			Code:    ErrCodeInvalidJob,
			Op:      "ValidateJob",
			Message: "missing required field",
			Err:     nil,
		}

		errorStr := err.Error()
		assert.Contains(t, errorStr, ErrCodeInvalidJob)
		assert.Contains(t, errorStr, "ValidateJob")
		assert.Contains(t, errorStr, "missing required field")
		assert.NotContains(t, errorStr, "<nil>")
	})

	t.Run("UnwrapReturnsCause", func(t *testing.T) {
		cause := errors.New("root cause")
		err := &SchedulerError{
			Code:    ErrCodeStorageError,
			Op:      "LoadJob",
			Message: "failed to load job",
			Err:     cause,
		}

		unwrapped := err.Unwrap()
		assert.Equal(t, cause, unwrapped)
	})

	t.Run("UnwrapReturnsNilWhenNoCause", func(t *testing.T) {
		err := &SchedulerError{
			Code:    ErrCodeJobNotFound,
			Op:      "GetJob",
			Message: "job does not exist",
			Err:     nil,
		}

		unwrapped := err.Unwrap()
		assert.Nil(t, unwrapped)
	})

	t.Run("ErrorsIsSupported", func(t *testing.T) {
		cause := errors.New("specific error")
		err := &SchedulerError{
			Code:    ErrCodeStorageError,
			Op:      "Test",
			Message: "test message",
			Err:     cause,
		}

		assert.True(t, errors.Is(err, cause))
	})
}

// TestJobNotFoundError tests the JobNotFoundError error type.
func TestJobNotFoundError(t *testing.T) {
	t.Run("ErrorFormat", func(t *testing.T) {
		err := JobNotFoundError{JobID: 12345}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "job not found")
		assert.Contains(t, errorStr, "12345")
	})

	t.Run("ZeroJobID", func(t *testing.T) {
		err := JobNotFoundError{JobID: 0}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "job not found")
		assert.Contains(t, errorStr, "0")
	})

	t.Run("LargeJobID", func(t *testing.T) {
		err := JobNotFoundError{JobID: 18446744073709551615}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "job not found")
		assert.Contains(t, errorStr, "18446744073709551615")
	})
}

// TestJobAlreadyExistsError tests the JobAlreadyExistsError error type.
func TestJobAlreadyExistsError(t *testing.T) {
	t.Run("ErrorWithName", func(t *testing.T) {
		err := JobAlreadyExistsError{
			JobID: 100,
			Name:  "daily-backup",
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "job already exists")
		assert.Contains(t, errorStr, "daily-backup")
		assert.Contains(t, errorStr, "100")
	})

	t.Run("ErrorWithoutName", func(t *testing.T) {
		err := JobAlreadyExistsError{
			JobID: 200,
			Name:  "",
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "job already exists")
		assert.Contains(t, errorStr, "200")
		assert.NotContains(t, errorStr, "(ID:")
	})

	t.Run("ZeroJobID", func(t *testing.T) {
		err := JobAlreadyExistsError{JobID: 0, Name: "test"}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "test")
		assert.Contains(t, errorStr, "0")
	})
}

// TestInvalidJobError tests the InvalidJobError error type.
func TestInvalidJobError(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		message string
	}{
		{
			name:    "MissingName",
			field:   "name",
			message: "cannot be empty",
		},
		{
			name:    "InvalidTimeout",
			field:   "timeout",
			message: "must be positive",
		},
		{
			name:    "InvalidSchedule",
			field:   "schedule",
			message: "invalid cron expression",
		},
		{
			name:    "MissingHandler",
			field:   "handler",
			message: "handler is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := InvalidJobError{
				Field:   tc.field,
				Message: tc.message,
			}
			errorStr := err.Error()
			assert.Contains(t, errorStr, "invalid job")
			assert.Contains(t, errorStr, tc.field)
			assert.Contains(t, errorStr, tc.message)
		})
	}

	t.Run("EmptyFields", func(t *testing.T) {
		err := InvalidJobError{Field: "", Message: ""}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "invalid job")
	})
}

// TestInvalidScheduleError tests the InvalidScheduleError error type.
func TestInvalidScheduleError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		cause := errors.New("parse error at position 3")
		err := InvalidScheduleError{
			Schedule: "* * * *",
			Cause:    cause,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "invalid schedule")
		assert.Contains(t, errorStr, "* * * *")
		assert.Contains(t, errorStr, "parse error at position 3")
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := InvalidScheduleError{
			Schedule: "bad-cron",
			Cause:    nil,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "invalid schedule")
		assert.Contains(t, errorStr, "bad-cron")
	})

	t.Run("UnwrapReturnsCause", func(t *testing.T) {
		cause := errors.New("cron parse error")
		err := InvalidScheduleError{
			Schedule: "*/invalid * * * *",
			Cause:    cause,
		}
		unwrapped := err.Unwrap()
		assert.Equal(t, cause, unwrapped)
	})

	t.Run("UnwrapReturnsNilWhenNoCause", func(t *testing.T) {
		err := InvalidScheduleError{
			Schedule: "invalid",
			Cause:    nil,
		}
		unwrapped := err.Unwrap()
		assert.Nil(t, unwrapped)
	})

	t.Run("EmptySchedule", func(t *testing.T) {
		err := InvalidScheduleError{Schedule: "", Cause: nil}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "invalid schedule")
	})
}

// TestHandlerNotFoundError tests the HandlerNotFoundError error type.
func TestHandlerNotFoundError(t *testing.T) {
	t.Run("StandardHandler", func(t *testing.T) {
		err := HandlerNotFoundError{Handler: "backup-handler"}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "handler not found")
		assert.Contains(t, errorStr, "backup-handler")
	})

	t.Run("EmptyHandler", func(t *testing.T) {
		err := HandlerNotFoundError{Handler: ""}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "handler not found")
	})

	t.Run("HandlerWithSpecialChars", func(t *testing.T) {
		err := HandlerNotFoundError{Handler: "handler/v2:backup"}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "handler/v2:backup")
	})
}

// TestHandlerFailedError tests the HandlerFailedError error type.
func TestHandlerFailedError(t *testing.T) {
	t.Run("StandardError", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := HandlerFailedError{
			Handler: "database-backup",
			JobID:   12345,
			Cause:   cause,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "handler")
		assert.Contains(t, errorStr, "database-backup")
		assert.Contains(t, errorStr, "failed")
		assert.Contains(t, errorStr, "12345")
		assert.Contains(t, errorStr, "connection refused")
	})

	t.Run("UnwrapReturnsCause", func(t *testing.T) {
		cause := errors.New("timeout")
		err := HandlerFailedError{
			Handler: "test",
			JobID:   1,
			Cause:   cause,
		}
		unwrapped := err.Unwrap()
		assert.Equal(t, cause, unwrapped)
	})

	t.Run("NilCause", func(t *testing.T) {
		err := HandlerFailedError{
			Handler: "test",
			JobID:   1,
			Cause:   nil,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "handler")
		unwrapped := err.Unwrap()
		assert.Nil(t, unwrapped)
	})

	t.Run("ErrorsIsSupported", func(t *testing.T) {
		cause := errors.New("specific error")
		err := HandlerFailedError{
			Handler: "test",
			JobID:   1,
			Cause:   cause,
		}
		assert.True(t, errors.Is(err, cause))
	})
}

// TestJobTimeoutError tests the JobTimeoutError error type.
func TestJobTimeoutError(t *testing.T) {
	tests := []struct {
		name    string
		jobID   uint64
		timeout time.Duration
	}{
		{
			name:    "ShortTimeout",
			jobID:   100,
			timeout: 5 * time.Second,
		},
		{
			name:    "LongTimeout",
			jobID:   200,
			timeout: 30 * time.Minute,
		},
		{
			name:    "ZeroTimeout",
			jobID:   300,
			timeout: 0,
		},
		{
			name:    "SubSecondTimeout",
			jobID:   400,
			timeout: 500 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := JobTimeoutError{
				JobID:   tc.jobID,
				Timeout: tc.timeout,
			}
			errorStr := err.Error()
			assert.Contains(t, errorStr, "timed out")
			assert.Contains(t, errorStr, tc.timeout.String())
		})
	}
}

// TestMaxRetriesExceededError tests the MaxRetriesExceededError error type.
func TestMaxRetriesExceededError(t *testing.T) {
	t.Run("StandardError", func(t *testing.T) {
		lastErr := errors.New("connection failed")
		err := MaxRetriesExceededError{
			JobID:      12345,
			MaxRetries: 5,
			LastError:  lastErr,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "12345")
		assert.Contains(t, errorStr, "max retries")
		assert.Contains(t, errorStr, "5")
		assert.Contains(t, errorStr, "connection failed")
	})

	t.Run("UnwrapReturnsLastError", func(t *testing.T) {
		lastErr := errors.New("final error")
		err := MaxRetriesExceededError{
			JobID:      1,
			MaxRetries: 3,
			LastError:  lastErr,
		}
		unwrapped := err.Unwrap()
		assert.Equal(t, lastErr, unwrapped)
	})

	t.Run("NilLastError", func(t *testing.T) {
		err := MaxRetriesExceededError{
			JobID:      1,
			MaxRetries: 3,
			LastError:  nil,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "max retries")
		unwrapped := err.Unwrap()
		assert.Nil(t, unwrapped)
	})

	t.Run("ZeroRetries", func(t *testing.T) {
		err := MaxRetriesExceededError{
			JobID:      1,
			MaxRetries: 0,
			LastError:  errors.New("error"),
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "0")
	})

	t.Run("ErrorsIsSupported", func(t *testing.T) {
		lastErr := errors.New("root cause")
		err := MaxRetriesExceededError{
			JobID:      1,
			MaxRetries: 3,
			LastError:  lastErr,
		}
		assert.True(t, errors.Is(err, lastErr))
	})
}

// TestSchedulerStoppedError tests the SchedulerStoppedError error type.
func TestSchedulerStoppedError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := SchedulerStoppedError{}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "scheduler")
		assert.Contains(t, errorStr, "stopped")
	})

	t.Run("ConsistentMessage", func(t *testing.T) {
		err1 := SchedulerStoppedError{}
		err2 := SchedulerStoppedError{}
		assert.Equal(t, err1.Error(), err2.Error())
	})
}

// TestUnauthorizedError tests the UnauthorizedError error type.
func TestUnauthorizedError(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
		jobID    uint64
		op       string
	}{
		{
			name:     "CancelOperation",
			tenantID: "tenant-123",
			jobID:    100,
			op:       "cancel",
		},
		{
			name:     "ViewOperation",
			tenantID: "tenant-456",
			jobID:    200,
			op:       "view",
		},
		{
			name:     "ModifyOperation",
			tenantID: "tenant-789",
			jobID:    300,
			op:       "modify",
		},
		{
			name:     "DeleteOperation",
			tenantID: "",
			jobID:    400,
			op:       "delete",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := UnauthorizedError{
				TenantID: tc.tenantID,
				JobID:    tc.jobID,
				Op:       tc.op,
			}
			errorStr := err.Error()
			assert.Contains(t, errorStr, "unauthorized")
			assert.Contains(t, errorStr, tc.tenantID)
			assert.Contains(t, errorStr, tc.op)
		})
	}
}

// TestErrStorageError tests the ErrStorageError error type.
func TestErrStorageError(t *testing.T) {
	t.Run("StandardError", func(t *testing.T) {
		cause := errors.New("disk full")
		err := ErrStorageError{
			Op:    "SaveJob",
			Cause: cause,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "storage error")
		assert.Contains(t, errorStr, "SaveJob")
		assert.Contains(t, errorStr, "disk full")
	})

	t.Run("UnwrapReturnsCause", func(t *testing.T) {
		cause := errors.New("io error")
		err := ErrStorageError{
			Op:    "LoadJobs",
			Cause: cause,
		}
		unwrapped := err.Unwrap()
		assert.Equal(t, cause, unwrapped)
	})

	t.Run("NilCause", func(t *testing.T) {
		err := ErrStorageError{
			Op:    "DeleteJob",
			Cause: nil,
		}
		errorStr := err.Error()
		assert.Contains(t, errorStr, "storage error")
		unwrapped := err.Unwrap()
		assert.Nil(t, unwrapped)
	})

	t.Run("VariousOperations", func(t *testing.T) {
		operations := []string{
			"Create",
			"Read",
			"Update",
			"Delete",
			"List",
			"Query",
			"Migrate",
		}

		for _, op := range operations {
			err := ErrStorageError{
				Op:    op,
				Cause: errors.New("test error"),
			}
			errorStr := err.Error()
			assert.Contains(t, errorStr, op)
		}
	})

	t.Run("ErrorsIsSupported", func(t *testing.T) {
		cause := errors.New("specific cause")
		err := ErrStorageError{
			Op:    "Test",
			Cause: cause,
		}
		assert.True(t, errors.Is(err, cause))
	})
}

// TestErrorChaining tests that error chaining works correctly.
func TestErrorChaining(t *testing.T) {
	t.Run("NestedErrors", func(t *testing.T) {
		rootCause := errors.New("connection reset")
		storageErr := ErrStorageError{Op: "LoadJob", Cause: rootCause}
		schedulerErr := &SchedulerError{
			Code:    ErrCodeStorageError,
			Op:      "GetJob",
			Message: "failed to retrieve job",
			Err:     storageErr,
		}

		assert.True(t, errors.Is(schedulerErr, rootCause))
		assert.True(t, errors.Is(schedulerErr, storageErr))
	})
}
