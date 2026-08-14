//go:build !integration && hashicorpmetrics && local

package api

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestRequiredFieldError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &RequiredFieldError{Field: "username"}
		if got := e.Error(); got != "username is required" {
			t.Errorf("got %q, want %q", got, "username is required")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewRequiredFieldError("password")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
		if de.Op != "api.validate" {
			t.Errorf("op = %q, want %q", de.Op, "api.validate")
		}
		var target *RequiredFieldError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected RequiredFieldError in Err")
		}
		if target.Field != "password" {
			t.Errorf("field = %q, want %q", target.Field, "password")
		}
	})
}

func TestServiceUnavailableError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ServiceUnavailableError{Service: "KMS"}
		if got := e.Error(); got != "KMS service not available" {
			t.Errorf("got %q, want %q", got, "KMS service not available")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewServiceUnavailableError("vector")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v, want ErrUnavailable", de.Code)
		}
		var target *ServiceUnavailableError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected ServiceUnavailableError")
		}
		if target.Service != "vector" {
			t.Errorf("service = %q, want %q", target.Service, "vector")
		}
	})
}

func TestNotImplementedError(t *testing.T) {
	t.Run("WithFeature", func(t *testing.T) {
		e := &NotImplementedError{Feature: "streaming"}
		if got := e.Error(); got != "streaming not yet implemented" {
			t.Errorf("got %q, want %q", got, "streaming not yet implemented")
		}
	})

	t.Run("WithoutFeature", func(t *testing.T) {
		e := &NotImplementedError{}
		if got := e.Error(); got != "not yet implemented" {
			t.Errorf("got %q, want %q", got, "not yet implemented")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewNotImplementedError("watch")
		if de.Code != dberrors.ErrNotImplemented {
			t.Errorf("code = %v, want ErrNotImplemented", de.Code)
		}
	})
}

func TestUnknownToolError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownToolError{Tool: "quicbaas_foo"}
		if got := e.Error(); got != "unknown tool: quicbaas_foo" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownToolError("bar")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInvalidFormatError(t *testing.T) {
	cause := errors.New("bad parse")

	t.Run("WithCause", func(t *testing.T) {
		e := &InvalidFormatError{Field: "ttl", Cause: cause}
		want := "invalid ttl format: bad parse"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap did not return cause")
		}
	})

	t.Run("WithMessage", func(t *testing.T) {
		e := &InvalidFormatError{Field: "id", Message: "must be UUID"}
		want := "invalid id format: must be UUID"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != nil {
			t.Error("Unwrap should be nil when no Cause")
		}
	})

	t.Run("ConstructorWithCause", func(t *testing.T) {
		de := NewInvalidFormatError("json", cause)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("ConstructorWithMessage", func(t *testing.T) {
		de := NewInvalidFormatMessageError("date", "must be RFC3339")
		var target *InvalidFormatError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected InvalidFormatError")
		}
		if target.Message != "must be RFC3339" {
			t.Errorf("message = %q", target.Message)
		}
	})
}

func TestUnexpectedResponseError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnexpectedResponseError{Service: "auth", Method: "Login"}
		want := "unexpected response type from auth Login"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnexpectedResponseError("kms", "Encrypt")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestOperationFailedError(t *testing.T) {
	cause := errors.New("disk full")

	t.Run("Error", func(t *testing.T) {
		e := &OperationFailedError{Operation: "backup", Cause: cause}
		want := "backup failed: disk full"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewOperationFailedError("restore", cause)
		if de.Op != "api.restore" {
			t.Errorf("op = %q", de.Op)
		}
	})

	t.Run("UnclassifiedCauseDefaultsToInternal", func(t *testing.T) {
		de := NewOperationFailedError("restore", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})

	t.Run("ClassifiedCauseCodePreserved", func(t *testing.T) {
		// A cause chain that already carries a classified QBError must keep
		// its code so transports map the failure to the correct client-facing
		// status (404/409/400) instead of masking it as a 500.
		cases := []struct {
			name string
			code dberrors.ErrorCode
		}{
			{"NotFound", dberrors.ErrNotFound},
			{"AlreadyExists", dberrors.ErrAlreadyExists},
			{"InvalidInput", dberrors.ErrInvalidInput},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				inner := &dberrors.QBError{Code: tc.code, Op: "iam.role", Err: cause}
				de := NewOperationFailedError("delete role", inner)
				if de.Code != tc.code {
					t.Errorf("code = %v, want %v", de.Code, tc.code)
				}
				if de.Op != "api.delete role" {
					t.Errorf("op = %q", de.Op)
				}
			})
		}
	})

	t.Run("UnknownCauseCodeDefaultsToInternal", func(t *testing.T) {
		inner := &dberrors.QBError{Code: dberrors.ErrUnknown, Op: "iam.role", Err: cause}
		de := NewOperationFailedError("delete role", inner)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})
}

func TestInvalidBase64Error(t *testing.T) {
	cause := errors.New("illegal base64")

	t.Run("Error", func(t *testing.T) {
		e := &InvalidBase64Error{Field: "ciphertext", Cause: cause}
		want := "invalid base64 ciphertext: illegal base64"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewInvalidBase64Error("data", cause)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestStreamError(t *testing.T) {
	cause := errors.New("connection reset")

	t.Run("Error", func(t *testing.T) {
		e := &StreamError{Cause: cause}
		if got := e.Error(); got != "stream error: connection reset" {
			t.Errorf("got %q", got)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewStreamError(cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestBatchItemError(t *testing.T) {
	cause := errors.New("invalid key")

	t.Run("WithCause", func(t *testing.T) {
		e := &BatchItemError{Index: 3, Field: "key", Cause: cause}
		want := "item 3: key: invalid key"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("WithMessage", func(t *testing.T) {
		e := &BatchItemError{Index: 5, Message: "missing value"}
		want := "item 5: missing value"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ConstructorWithCause", func(t *testing.T) {
		de := NewBatchItemError(1, "key", cause)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("ConstructorWithMessage", func(t *testing.T) {
		de := NewBatchItemMessageError(2, "empty value")
		var target *BatchItemError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected BatchItemError")
		}
		if target.Message != "empty value" {
			t.Errorf("message = %q", target.Message)
		}
	})
}

func TestServiceNotConfiguredError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ServiceNotConfiguredError{Service: "secrets"}
		if got := e.Error(); got != "secrets service not configured" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewServiceNotConfiguredError("auth")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestEmptyCollectionError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &EmptyCollectionError{Field: "items"}
		if got := e.Error(); got != "items cannot be empty" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewEmptyCollectionError("vectors")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestRangeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &RangeError{Field: "port", Min: 1024, Max: 65535}
		want := "port must be between 1024 and 65535"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewRangeError("limit", 1, 100)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NotFoundError{Resource: "user", ID: "abc-123"}
		want := "user not found: abc-123"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewNotFoundError("key", "mykey")
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestNoResponseError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NoResponseError{Operation: "list"}
		if got := e.Error(); got != "no response received from list" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewNoResponseError("query")
		if de.Op != "api.query" {
			t.Errorf("op = %q", de.Op)
		}
	})
}

func TestMaxLimitError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &MaxLimitError{Field: "keys", Max: 1000}
		want := "maximum 1000 keys allowed per batch"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewMaxLimitError("items", 500)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestMarshalError(t *testing.T) {
	cause := errors.New("invalid UTF-8")

	t.Run("Error", func(t *testing.T) {
		e := &MarshalError{Cause: cause}
		if got := e.Error(); got != "failed to marshal result: invalid UTF-8" {
			t.Errorf("got %q", got)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewMarshalError(cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInvalidTypeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &InvalidTypeError{Field: "count", Expected: "an integer"}
		want := "count must be an integer"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewInvalidTypeError("ttl", "a duration")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestNilInputError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NilInputError{Name: "request"}
		if got := e.Error(); got != "request cannot be nil" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewNilInputError("quic.Get", "config")
		if de.Op != "quic.Get" {
			t.Errorf("op = %q", de.Op)
		}
	})
}

func TestEncodeDecodeErrors(t *testing.T) {
	cause := errors.New("encoding failed")

	t.Run("EncodeError", func(t *testing.T) {
		e := &EncodeError{Subject: "request", Cause: cause}
		want := "failed to encode request: encoding failed"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
		de := NewEncodeError("quic.Put", "payload", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		e := &DecodeError{Subject: "response", Cause: cause}
		want := "failed to decode response: encoding failed"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
		de := NewDecodeError("quic.Get", "body", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestClientClosedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ClientClosedError{}
		if got := e.Error(); got != "client is closed" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewClientClosedError("quic.Get")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestConnectionError(t *testing.T) {
	cause := errors.New("timeout")

	t.Run("WithTarget", func(t *testing.T) {
		e := &ConnectionError{Action: "connect", Target: "localhost:8080", Cause: cause}
		want := "failed to connect localhost:8080: timeout"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("WithoutTarget", func(t *testing.T) {
		e := &ConnectionError{Action: "dial", Cause: cause}
		want := "failed to dial: timeout"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewConnectionError("quic", "open", "node1", cause)
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestWriteReadErrors(t *testing.T) {
	cause := errors.New("io error")

	t.Run("WriteError", func(t *testing.T) {
		e := &WriteError{Field: "header", Cause: cause}
		want := "failed to write header: io error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
		de := NewWriteError("quic", "body", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("ReadError", func(t *testing.T) {
		e := &ReadError{Field: "payload", Cause: cause}
		want := "failed to read payload: io error"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
		de := NewReadError("quic", "length", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestSizeLimitError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &SizeLimitError{Field: "value", Size: 2048, Maximum: 1024}
		want := "value size 2048 exceeds maximum 1024"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewSizeLimitError("quic", "key", 500, 256)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestConfigurationError(t *testing.T) {
	cause := errors.New("missing cert")

	t.Run("Error", func(t *testing.T) {
		e := &ConfigurationError{Action: "load TLS", Cause: cause}
		want := "failed to load TLS: missing cert"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewConfigurationError("server", "parse config", cause)
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestComponentUnavailableError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ComponentUnavailableError{Component: "backup manager"}
		if got := e.Error(); got != "backup manager not available" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewComponentUnavailableError("seal handler")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestBatchItemValidationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &BatchItemValidationError{Index: 7, Message: "key is required"}
		want := "item 7 key is required"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewBatchItemValidationError(0, "empty value")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInvalidInputError(t *testing.T) {
	cause := errors.New("bad input")

	t.Run("WithCause", func(t *testing.T) {
		e := &InvalidInputError{Message: "invalid key", Cause: cause}
		want := "invalid key: bad input"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("WithoutCause", func(t *testing.T) {
		e := &InvalidInputError{Message: "too short"}
		if got := e.Error(); got != "too short" {
			t.Errorf("got %q", got)
		}
		if e.Unwrap() != nil {
			t.Error("expected nil Unwrap")
		}
	})

	t.Run("ConstructorMessage", func(t *testing.T) {
		de := NewInvalidInputError("bad data")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("ConstructorCause", func(t *testing.T) {
		de := NewInvalidInputCauseError("parse failed", cause)
		var target *InvalidInputError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected InvalidInputError")
		}
		if target.Cause != cause {
			t.Error("cause mismatch")
		}
	})
}

func TestDataTooShortError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &DataTooShortError{Size: 2}
		if got := e.Error(); got != "data too short: 2 bytes" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewDataTooShortError("quic.decode", 0)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestMissingSubOpcodeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &MissingSubOpcodeError{Service: "KMS"}
		if got := e.Error(); got != "KMS request missing sub-opcode" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewMissingSubOpcodeError("dispatch", "vector")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUnknownSubOpcodeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownSubOpcodeError{Service: "auth", SubOpcode: 0xFF}
		want := "unknown auth sub-opcode: 0xFF"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownSubOpcodeError("dispatch", "kms", 0xAB)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestInvalidBatchCheckError(t *testing.T) {
	cause := errors.New("bad check")

	t.Run("Error", func(t *testing.T) {
		e := &InvalidBatchCheckError{Index: 2, Cause: cause}
		want := "invalid check at index 2: bad check"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewInvalidBatchCheckError("txn", 0, cause)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestShardNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ShardNotFoundError{ShardID: 42}
		want := "shard 42 not found on this node"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewShardNotFoundError("route", 99)
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestItemReadError(t *testing.T) {
	cause := errors.New("eof")

	t.Run("Error", func(t *testing.T) {
		e := &ItemReadError{Field: "value", Index: 3, Cause: cause}
		want := "failed to read value for item 3: eof"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewItemReadError("decode", "key", 1, cause)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestEmptyListError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &EmptyListError{Field: "keys"}
		if got := e.Error(); got != "keys is empty" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewEmptyListError("batch", "items")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUnknownFederationSubOpError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownFederationSubOpError{SubOp: 0x10}
		want := "unknown federation sub-operation: 0x10"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownFederationSubOpError("federation", 0x20)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestAuthenticationErrors(t *testing.T) {
	tests := []struct {
		name        string
		constructor func() *dberrors.QBError
		wantMsg     string
		wantCode    dberrors.ErrorCode
	}{
		{"AuthenticationDisabled", func() *dberrors.QBError { return NewAuthenticationDisabledError("login") }, "authentication is not enabled", dberrors.ErrConfiguration},
		{"TokenRequired", func() *dberrors.QBError { return NewTokenRequiredError("api") }, "authentication token required", dberrors.ErrUnauthorized},
		{"TokenRevoked", func() *dberrors.QBError { return NewTokenRevokedError("api") }, "token has been revoked", dberrors.ErrUnauthorized},
		{"InsufficientPermissions", func() *dberrors.QBError { return NewInsufficientPermissionsError("admin") }, "insufficient permissions", dberrors.ErrForbidden},
		{"InvalidTokenClaims", func() *dberrors.QBError { return NewInvalidTokenClaimsError("auth") }, "invalid token claims", dberrors.ErrUnauthorized},
		{"InvalidRefreshToken", func() *dberrors.QBError { return NewInvalidRefreshTokenError("refresh") }, "invalid refresh token", dberrors.ErrUnauthorized},
		{"InvalidUserType", func() *dberrors.QBError { return NewInvalidUserTypeError("auth") }, "invalid user type", dberrors.ErrInternal},
		{"RolesNotFound", func() *dberrors.QBError { return NewRolesNotFoundError("assign") }, "one or more roles do not exist", dberrors.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			de := tt.constructor()
			if de.Code != tt.wantCode {
				t.Errorf("code = %v, want %v", de.Code, tt.wantCode)
			}
			if de.Err.Error() != tt.wantMsg {
				t.Errorf("msg = %q, want %q", de.Err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestInvalidTokenError(t *testing.T) {
	cause := errors.New("expired")

	t.Run("WithCause", func(t *testing.T) {
		e := &InvalidTokenError{Cause: cause}
		if got := e.Error(); got != "invalid token: expired" {
			t.Errorf("got %q", got)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("WithoutCause", func(t *testing.T) {
		e := &InvalidTokenError{}
		if got := e.Error(); got != "invalid token" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewInvalidTokenError("auth", cause)
		if de.Code != dberrors.ErrUnauthorized {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUnexpectedSigningMethodError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnexpectedSigningMethodError{Algorithm: "RS384"}
		if got := e.Error(); got != "unexpected signing method: RS384" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnexpectedSigningMethodError("jwt", "HS256")
		if de.Code != dberrors.ErrUnauthorized {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestAlreadyExistsError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &AlreadyExistsError{Resource: "user", ID: "admin"}
		if got := e.Error(); got != "user already exists: admin" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewAlreadyExistsError("iam", "role", "viewer")
		if de.Code != dberrors.ErrAlreadyExists {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestBuiltInRoleError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &BuiltInRoleError{Action: "delete", RoleName: "admin"}
		want := "cannot delete built-in role: admin"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewBuiltInRoleError("iam", "modify", "system")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUserHasRoleError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UserHasRoleError{RoleName: "admin"}
		if got := e.Error(); got != "user already has role: admin" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUserHasRoleError("iam", "editor")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUserMissingRoleError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UserMissingRoleError{RoleName: "admin"}
		if got := e.Error(); got != "user does not have role: admin" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUserMissingRoleError("iam", "viewer")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestReservedKeyPrefixError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ReservedKeyPrefixError{Prefix: "__sys/"}
		want := "key uses reserved internal prefix '__sys/'"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewReservedKeyPrefixError("kv.put", "__internal/")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestDeprecatedOperationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &DeprecatedOperationError{Message: "use v2 API"}
		if got := e.Error(); got != "use v2 API" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewDeprecatedOperationError("api", "this endpoint is deprecated")
		if de.Code != dberrors.ErrUnimplemented {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUnknownManagerTypeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownManagerTypeError{TypeName: "custom"}
		if got := e.Error(); got != "unknown auth manager type: custom" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownManagerTypeError("auth", "unknown")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestIteratorError(t *testing.T) {
	cause := errors.New("corrupt data")

	t.Run("Error", func(t *testing.T) {
		e := &IteratorError{Cause: cause}
		if got := e.Error(); got != "iterator error: corrupt data" {
			t.Errorf("got %q", got)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewIteratorError("storage.list", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestUnknownOpcodeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownOpcodeError{Opcode: 0xFE}
		want := "unknown opcode: 0xFE"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownOpcodeError("unix", 0x01)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestHandlerClosedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &HandlerClosedError{}
		if got := e.Error(); got != "handler is closed" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewHandlerClosedError("unix.handle")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestProtocolStatusError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ProtocolStatusError{Message: "not found"}
		if got := e.Error(); got != "not found" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewProtocolStatusError("client", dberrors.ErrNotFound, "resource missing")
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestFeatureUnsupportedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &FeatureUnsupportedError{Feature: "watch"}
		if got := e.Error(); got != "watch is not supported" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewFeatureUnsupportedError("api", "streaming")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestCodecErrors(t *testing.T) {
	t.Run("NilEntityError", func(t *testing.T) {
		e := &NilEntityError{Operation: "encode"}
		if got := e.Error(); got != "cannot encode nil entity" {
			t.Errorf("got %q", got)
		}
		de := NewNilEntityError("codec", "decode")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("EmptyDataError", func(t *testing.T) {
		e := &EmptyDataError{}
		if got := e.Error(); got != "cannot decode empty data" {
			t.Errorf("got %q", got)
		}
		de := NewEmptyDataError("codec.decode")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("CodecNotFoundError", func(t *testing.T) {
		e := &CodecNotFoundError{Codec: "protobuf"}
		if got := e.Error(); got != "codec protobuf not found in registry" {
			t.Errorf("got %q", got)
		}
		de := NewCodecNotFoundError("registry", "msgpack")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("NoSuitableCodecError", func(t *testing.T) {
		e := &NoSuitableCodecError{EntityType: "UserEvent"}
		if got := e.Error(); got != "no suitable codec found for entity type UserEvent" {
			t.Errorf("got %q", got)
		}
		de := NewNoSuitableCodecError("codec", "CustomType")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("NonPointerError", func(t *testing.T) {
		e := &NonPointerError{TypeName: "string"}
		if got := e.Error(); got != "entity must be a pointer, got string" {
			t.Errorf("got %q", got)
		}
		de := NewNonPointerError("codec", "int")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestServerErrors(t *testing.T) {
	t.Run("UnsupportedOpcodeError", func(t *testing.T) {
		e := &UnsupportedOpcodeError{Opcode: 0xFF}
		want := "unsupported operation: 0xff"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		de := NewUnsupportedOpcodeError("server", 0x01)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("InvalidDataError", func(t *testing.T) {
		e := &InvalidDataError{Context: "missing header"}
		if got := e.Error(); got != "invalid data: missing header" {
			t.Errorf("got %q", got)
		}
		de := NewInvalidDataError("server", "truncated")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v", de.Code)
		}
	})

	t.Run("UnsupportedQueryTypeError", func(t *testing.T) {
		e := &UnsupportedQueryTypeError{TypeName: "spatial"}
		if got := e.Error(); got != "unsupported query type: spatial" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("UnexpectedResultTypeError", func(t *testing.T) {
		e := &UnexpectedResultTypeError{TypeName: "[]byte"}
		if got := e.Error(); got != "unexpected result type: []byte" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("UnsupportedModeOperationError", func(t *testing.T) {
		e := &UnsupportedModeOperationError{Operation: "write", Mode: "readonly"}
		if got := e.Error(); got != "write not supported in readonly" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ShardNotFoundInModeError", func(t *testing.T) {
		e := &ShardNotFoundInModeError{ShardID: 5, Mode: "standalone"}
		want := "shard 5 not found in standalone"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("UnknownServerModeError", func(t *testing.T) {
		e := &UnknownServerModeError{Mode: "invalid"}
		want := `unknown server mode "invalid", valid modes are: standalone, cluster, devcluster`
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ServerStateError", func(t *testing.T) {
		e := &ServerStateError{State: "shutting down"}
		if got := e.Error(); got != "shutting down" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("NoListenersConfiguredError", func(t *testing.T) {
		e := &NoListenersConfiguredError{Message: "no listeners"}
		if got := e.Error(); got != "no listeners" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ShutdownError", func(t *testing.T) {
		errs := []error{errors.New("err1"), errors.New("err2")}
		e := &ShutdownError{Count: 2, Errors: errs}
		want := fmt.Sprintf("shutdown completed with 2 errors: %v", errs)
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		de := NewShutdownError("server", errs)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v", de.Code)
		}
	})
}

func TestMiscErrors(t *testing.T) {
	cause := errors.New("test cause")

	t.Run("UnknownProviderError", func(t *testing.T) {
		e := &UnknownProviderError{Provider: "aws"}
		if got := e.Error(); got != "unknown provider: aws" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("EntityShardPinError", func(t *testing.T) {
		e := &EntityShardPinError{EntityType: "secret", ShardID: 10}
		want := `entity "secret" pinned to unknown shard 10`
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("EncryptionKeySizeError", func(t *testing.T) {
		e := &EncryptionKeySizeError{Expected: 32, Actual: 16}
		want := "encryption key must be 32 bytes, got 16 bytes"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("CertGenerationError", func(t *testing.T) {
		e := &CertGenerationError{Step: "generate key", Cause: cause}
		want := "failed to generate key: test cause"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("DataDirOperationError", func(t *testing.T) {
		e := &DataDirOperationError{Action: "create", Path: "/data", Cause: cause}
		want := "failed to create /data: test cause"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if e.Unwrap() != cause {
			t.Error("Unwrap failed")
		}
	})

	t.Run("StateMachineClosedError", func(t *testing.T) {
		e := &StateMachineClosedError{}
		if got := e.Error(); got != "state machine is closed" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("NilFieldError", func(t *testing.T) {
		e := &NilFieldError{Field: "config"}
		if got := e.Error(); got != "config must not be nil" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("EmptyFieldError", func(t *testing.T) {
		e := &EmptyFieldError{Field: "name"}
		if got := e.Error(); got != "name must not be empty" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("KeyLengthError", func(t *testing.T) {
		e := &KeyLengthError{Expected: 32, Actual: 16}
		want := "expected key length 32, got 16"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("KeyTooShortError", func(t *testing.T) {
		e := &KeyTooShortError{Component: "entity prefix"}
		if got := e.Error(); got != "key too short for entity prefix" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("FeatureDisabledError", func(t *testing.T) {
		e := &FeatureDisabledError{Feature: "encryption"}
		if got := e.Error(); got != "encryption not enabled" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ZeroValueError", func(t *testing.T) {
		e := &ZeroValueError{Field: "shard_id"}
		if got := e.Error(); got != "shard_id must not be zero" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("HTTPStatusError", func(t *testing.T) {
		e := &HTTPStatusError{StatusCode: 404, Body: "not found"}
		want := "HTTP 404: not found"
		if got := e.Error(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("RemoteError", func(t *testing.T) {
		e := &RemoteError{Message: "server error"}
		if got := e.Error(); got != "server error" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("MinimumValueError", func(t *testing.T) {
		e := &MinimumValueError{Field: "replicas", Minimum: "3"}
		if got := e.Error(); got != "replicas must be at least 3" {
			t.Errorf("got %q", got)
		}
	})
}

// =============================================================================
// Consolidated from coverage_boost_92_test.go
// =============================================================================

// ---------------------------------------------------------------------------
// errors_auth.go — PasswordTooShortError
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// errors_auth.go — PasswordHashError
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// errors_auth.go — AccountLockedError / IsAccountLocked
// ---------------------------------------------------------------------------

func TestAccountLockedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &AccountLockedError{Username: "alice", RetryAfter: 30 * time.Second}
		assert.Contains(t, e.Error(), "account locked for user alice")
		assert.Contains(t, e.Error(), "30s")
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewAccountLockedError("auth.Login", "bob", 60*time.Second)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrRateLimited, de.Code)
		assert.Equal(t, "auth.Login", de.Op)

		var target *AccountLockedError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "bob", target.Username)
		assert.Equal(t, 60*time.Second, target.RetryAfter)
	})
}

func TestIsAccountLocked(t *testing.T) {
	t.Run("ReturnsTrue", func(t *testing.T) {
		de := NewAccountLockedError("auth.Login", "alice", 10*time.Second)
		assert.True(t, IsAccountLocked(de))
	})

	t.Run("ReturnsFalseForNil", func(t *testing.T) {
		assert.False(t, IsAccountLocked(nil))
	})

	t.Run("ReturnsFalseForOtherError", func(t *testing.T) {
		assert.False(t, IsAccountLocked(errors.New("something else")))
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — FlagBindError
// ---------------------------------------------------------------------------

func TestFlagBindError(t *testing.T) {
	cause := errors.New("no such flag")

	t.Run("Constructor", func(t *testing.T) {
		de := NewFlagBindError("config.Init", "verbose", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *FlagBindError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "verbose", target.Flag)
		assert.Equal(t, cause, target.Cause)
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — ConfigFileReadError
// ---------------------------------------------------------------------------

func TestConfigFileReadError(t *testing.T) {
	cause := errors.New("permission denied")

	t.Run("Constructor", func(t *testing.T) {
		de := NewConfigFileReadError("config.Load", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *ConfigFileReadError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, cause, target.Cause)
		assert.Contains(t, target.Error(), "failed to read config file")
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — ConfigUnmarshalError
// ---------------------------------------------------------------------------

func TestConfigUnmarshalError(t *testing.T) {
	cause := errors.New("invalid YAML")

	t.Run("Constructor", func(t *testing.T) {
		de := NewConfigUnmarshalError("config.Parse", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *ConfigUnmarshalError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "failed to unmarshal config")
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — ConfigFileWriteError
// ---------------------------------------------------------------------------

func TestConfigFileWriteError(t *testing.T) {
	cause := errors.New("disk full")

	t.Run("Constructor", func(t *testing.T) {
		de := NewConfigFileWriteError("config.Save", "/etc/quicbaas.yaml", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *ConfigFileWriteError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "/etc/quicbaas.yaml", target.Path)
		assert.Contains(t, target.Error(), "/etc/quicbaas.yaml")
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — ConfigMarshalError
// ---------------------------------------------------------------------------

func TestConfigMarshalError(t *testing.T) {
	cause := errors.New("unsupported type")

	t.Run("Constructor", func(t *testing.T) {
		de := NewConfigMarshalError("config.Export", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *ConfigMarshalError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "failed to marshal config")
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — InvalidURLError
// ---------------------------------------------------------------------------

func TestInvalidURLError(t *testing.T) {
	cause := errors.New("missing host")

	t.Run("Constructor", func(t *testing.T) {
		de := NewInvalidURLError("config.Validate", "callback_url", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrValidation, de.Code)

		var target *InvalidURLError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "callback_url", target.Field)
		assert.Contains(t, target.Error(), "invalid callback_url")
	})
}

// ---------------------------------------------------------------------------
// errors_config.go — RequiredWhenEnabledError
// ---------------------------------------------------------------------------

func TestRequiredWhenEnabledError(t *testing.T) {
	t.Run("Constructor", func(t *testing.T) {
		de := NewRequiredWhenEnabledError("config.Validate", "api_key", "tls_enabled")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrValidation, de.Code)

		var target *RequiredWhenEnabledError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "api_key", target.Field)
		assert.Equal(t, "tls_enabled", target.Condition)
		assert.Contains(t, target.Error(), "api_key cannot be empty when tls_enabled is true")
	})
}

// ---------------------------------------------------------------------------
// errors_embedding.go — constructor coverage
// ---------------------------------------------------------------------------

func TestEmbeddingErrorConstructors(t *testing.T) {
	cause := errors.New("network error")

	t.Run("MarshalRequest", func(t *testing.T) {
		de := NewEmbeddingMarshalRequestError("embed.Generate", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingMarshalRequestError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("CreateRequest", func(t *testing.T) {
		de := NewEmbeddingCreateRequestError("embed.Generate", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingCreateRequestError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "failed to create request")
	})

	t.Run("ReadResponse", func(t *testing.T) {
		de := NewEmbeddingReadResponseError("embed.Generate", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingReadResponseError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "failed to read response")
	})

	t.Run("UnmarshalResponse", func(t *testing.T) {
		de := NewEmbeddingUnmarshalResponseError("embed.Parse", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingUnmarshalResponseError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "failed to unmarshal response")
	})

	t.Run("InvalidIndex", func(t *testing.T) {
		de := NewEmbeddingInvalidIndexError("embed.Parse", 42)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingInvalidIndexError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, 42, target.Index)
		assert.Contains(t, target.Error(), "42")
	})

	t.Run("EmptyAtIndex", func(t *testing.T) {
		de := NewEmbeddingEmptyAtIndexError("embed.Parse", 7)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingEmptyAtIndexError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, 7, target.Index)
		assert.Contains(t, target.Error(), "empty embedding at index 7")
	})

	t.Run("MissingAtIndex", func(t *testing.T) {
		de := NewEmbeddingMissingAtIndexError("embed.Parse", 3)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *EmbeddingMissingAtIndexError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, 3, target.Index)
		assert.Contains(t, target.Error(), "missing embedding at index 3")
	})
}

// ---------------------------------------------------------------------------
// errors_kms.go — constructor coverage
// ---------------------------------------------------------------------------

func TestKMSErrorConstructors(t *testing.T) {
	t.Run("InvalidAlgorithm", func(t *testing.T) {
		de := NewInvalidAlgorithmError("kms.Encrypt", "ROT13")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *InvalidAlgorithmError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "ROT13", target.Algorithm)
	})

	t.Run("KeyDisabled", func(t *testing.T) {
		de := NewKeyDisabledError("kms.Sign", "key-123")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *KeyDisabledError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "key-123", target.KeyID)
		assert.Contains(t, target.Error(), "disabled")
	})

	t.Run("KeyDeleted", func(t *testing.T) {
		de := NewKeyDeletedError("kms.Encrypt", "key-456")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *KeyDeletedError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "key-456", target.KeyID)
		assert.Contains(t, target.Error(), "deleted")
	})

	t.Run("KeyPendingDeletion", func(t *testing.T) {
		de := NewKeyPendingDeletionError("kms.Use", "key-789")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *KeyPendingDeletionError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "pending deletion")
	})

	t.Run("AlgorithmOperationUnsupported", func(t *testing.T) {
		de := NewAlgorithmOperationUnsupportedError("kms.Op", "AES-128", "sign")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *AlgorithmOperationUnsupportedError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "AES-128", target.Algorithm)
		assert.Equal(t, "sign", target.Operation)
	})

	t.Run("UnknownAlgorithm", func(t *testing.T) {
		de := NewUnknownAlgorithmError("kms.Lookup", "FOOBAR")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *UnknownAlgorithmError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "FOOBAR", target.Name)
	})

	t.Run("AlgorithmSigningUnsupported", func(t *testing.T) {
		de := NewAlgorithmSigningUnsupportedError("kms.Sign", "AES-GCM")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *AlgorithmSigningUnsupportedError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "does not support signing")
	})

	t.Run("NoBackendsAvailable", func(t *testing.T) {
		de := NewNoBackendsAvailableError("kms.Get", "primary")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrUnavailable, de.Code)

		var target *NoBackendsAvailableError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "primary", target.Source)
	})

	t.Run("NoBackendsRegistered", func(t *testing.T) {
		de := NewNoBackendsRegisteredError("kms.Init")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrUnavailable, de.Code)

		var target *NoBackendsRegisteredError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "no backends were successfully registered")
	})

	t.Run("UnsupportedPublicKeyType", func(t *testing.T) {
		de := NewUnsupportedPublicKeyTypeError("kms.Export", "DSA")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *UnsupportedPublicKeyTypeError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "DSA", target.TypeName)
	})

	t.Run("NoPublicComponent", func(t *testing.T) {
		de := NewNoPublicComponentError("kms.GetPublic")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *NoPublicComponentError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "key has no public component", target.Error())
	})

	t.Run("UnsupportedHashAlgorithm", func(t *testing.T) {
		de := NewUnsupportedHashAlgorithmError("kms.Hash", "MD4")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *UnsupportedHashAlgorithmError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "MD4", target.Algorithm)
	})
}

// ---------------------------------------------------------------------------
// errors_raft.go — New* constructor coverage
// ---------------------------------------------------------------------------

func TestRaftConstructors_Simple(t *testing.T) {
	tests := []struct {
		name     string
		buildFn  func() *dberrors.QBError
		wantCode dberrors.ErrorCode
		wantOp   string
	}{
		{
			"NewResourceClosedError",
			func() *dberrors.QBError {
				return NewResourceClosedError("raft.Write", "writer", dberrors.ErrUnavailable)
			},
			dberrors.ErrUnavailable, "raft.Write",
		},
		{
			"NewChecksumError",
			func() *dberrors.QBError { return NewChecksumError("raft.Restore", "snapshot") },
			dberrors.ErrDataLoss, "raft.Restore",
		},
		{
			"NewUnsupportedValueError",
			func() *dberrors.QBError {
				return NewUnsupportedValueError("raft.Parse", "format", "xml")
			},
			dberrors.ErrInvalidInput, "raft.Parse",
		},
		{
			"NewInvalidOpcodeError",
			func() *dberrors.QBError {
				return NewInvalidOpcodeError("raft.Dispatch", 0xFF, "batch")
			},
			dberrors.ErrInvalidInput, "raft.Dispatch",
		},
		{
			"NewIncompleteDataError",
			func() *dberrors.QBError { return NewIncompleteDataError("raft.Decode", "header") },
			dberrors.ErrInvalidInput, "raft.Decode",
		},
		{
			"NewDeadlineRequiredError",
			func() *dberrors.QBError { return NewDeadlineRequiredError("raft.Read") },
			dberrors.ErrInvalidInput, "raft.Read",
		},
		{
			"NewDeadlineExceededError",
			func() *dberrors.QBError { return NewDeadlineExceededError("raft.Read") },
			dberrors.ErrDeadlineExceeded, "raft.Read",
		},
		{
			"NewUnknownOperationError",
			func() *dberrors.QBError { return NewUnknownOperationError("raft.Handle", "foobar") },
			dberrors.ErrInvalidInput, "raft.Handle",
		},
		{
			"NewInvalidContextTypeError",
			func() *dberrors.QBError {
				return NewInvalidContextTypeError("raft.Lookup", "[]byte")
			},
			dberrors.ErrInternal, "raft.Lookup",
		},
		{
			"NewCanceledError",
			func() *dberrors.QBError { return NewCanceledError("raft.Propose", "write") },
			dberrors.ErrCanceled, "raft.Propose",
		},
		{
			"NewComponentNotInitializedError",
			func() *dberrors.QBError {
				return NewComponentNotInitializedError("raft.Start", "nodehost")
			},
			dberrors.ErrInternal, "raft.Start",
		},
		{
			"NewWriterClosedError",
			func() *dberrors.QBError { return NewWriterClosedError("raft.Write") },
			dberrors.ErrUnavailable, "raft.Write",
		},
		{
			"NewAlreadyBootstrappedError",
			func() *dberrors.QBError { return NewAlreadyBootstrappedError("raft.Bootstrap") },
			dberrors.ErrAlreadyExists, "raft.Bootstrap",
		},
		{
			"NewInvalidNodeIDZeroError",
			func() *dberrors.QBError { return NewInvalidNodeIDZeroError("raft.Validate") },
			dberrors.ErrInvalidInput, "raft.Validate",
		},
		{
			"NewInvalidNodeIDMembersError",
			func() *dberrors.QBError { return NewInvalidNodeIDMembersError("raft.Validate") },
			dberrors.ErrInvalidInput, "raft.Validate",
		},
		{
			"NewMTLSConfigError",
			func() *dberrors.QBError { return NewMTLSConfigError("raft.Init") },
			dberrors.ErrConfiguration, "raft.Init",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			de := tt.buildFn()
			require.NotNil(t, de)
			assert.Equal(t, tt.wantCode, de.Code)
			assert.Equal(t, tt.wantOp, de.Op)
			assert.NotEmpty(t, de.Error())
		})
	}
}

func TestRaftConstructors_WithParams(t *testing.T) {
	t.Run("NewShardOperationError", func(t *testing.T) {
		cause := errors.New("timeout")
		de := NewShardOperationError("raft.Propose", dberrors.ErrTimeout, 5, "propose", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrTimeout, de.Code)

		var target *ShardOperationError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(5), target.ShardID)
		assert.Equal(t, "propose", target.Operation)
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("NewNodeOperationError", func(t *testing.T) {
		cause := errors.New("rejected")
		de := NewNodeOperationError("raft.AddNode", 10, 3, "add", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrRaft, de.Code)

		var target *NodeOperationError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(10), target.NodeID)
		assert.Equal(t, uint64(3), target.ShardID)
	})

	t.Run("NewLengthMismatchError", func(t *testing.T) {
		de := NewLengthMismatchError("raft.Apply", "keys", 5, "values", 3)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *LengthMismatchError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "keys", target.FieldA)
		assert.Equal(t, 5, target.LenA)
		assert.Equal(t, "values", target.FieldB)
		assert.Equal(t, 3, target.LenB)
	})

	t.Run("NewSnapshotOperationError", func(t *testing.T) {
		cause := errors.New("write failed")
		de := NewSnapshotOperationError("raft.Snapshot", "save", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrSnapshot, de.Code)

		var target *SnapshotOperationError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("NewBatchOperationError", func(t *testing.T) {
		cause := errors.New("encode failed")
		de := NewBatchOperationError("raft.Batch", 2, "put", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *BatchOperationError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, 2, target.Index)
		assert.Equal(t, "put", target.Operation)
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("NewInvalidQueryTypeError", func(t *testing.T) {
		de := NewInvalidQueryTypeError("raft.Query", "[]byte", "string")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *InvalidQueryTypeError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "[]byte", target.Expected)
		assert.Equal(t, "string", target.Actual)
	})

	t.Run("NewRequiredFieldOperationError", func(t *testing.T) {
		de := NewRequiredFieldOperationError("raft.Apply", "key", "put")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *RequiredFieldOperationError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "key", target.Field)
		assert.Equal(t, "put", target.Operation)
	})

	t.Run("NewInvalidEntityKeyError", func(t *testing.T) {
		de := NewInvalidEntityKeyError("raft.Route", "lookup")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *InvalidEntityKeyError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "lookup", target.Operation)
	})

	t.Run("NewBatchFormatError", func(t *testing.T) {
		de := NewBatchFormatError("raft.Parse", "an array of operations")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *BatchFormatError
		require.True(t, errors.As(de.Err, &target))
		assert.Contains(t, target.Error(), "an array of operations")
	})

	t.Run("NewInsufficientPeersError", func(t *testing.T) {
		de := NewInsufficientPeersError("raft.Bootstrap", dberrors.ErrPreconditionFailed, 3, 1)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrPreconditionFailed, de.Code)

		var target *InsufficientPeersError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, 3, target.Required)
		assert.Equal(t, 1, target.Have)
	})

	t.Run("NewDuplicateNodeIDError", func(t *testing.T) {
		de := NewDuplicateNodeIDError("raft.Validate", 42)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *DuplicateNodeIDError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(42), target.NodeID)
	})

	t.Run("NewEmptyPeerAddressError", func(t *testing.T) {
		de := NewEmptyPeerAddressError("raft.Validate", 7)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *EmptyPeerAddressError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(7), target.NodeID)
	})

	t.Run("NewNodeNotInMembersError", func(t *testing.T) {
		de := NewNodeNotInMembersError("raft.Validate", 99)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *NodeNotInMembersError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(99), target.NodeID)
	})

	t.Run("NewLeaderNotFoundInPeersError", func(t *testing.T) {
		de := NewLeaderNotFoundInPeersError("raft.Check", dberrors.ErrLeaderNotAvailable, 5)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrLeaderNotAvailable, de.Code)

		var target *LeaderNotFoundInPeersError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(5), target.LeaderID)
	})

	t.Run("NewLeaderMismatchError", func(t *testing.T) {
		de := NewLeaderMismatchError("raft.Verify", 1, 2)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *LeaderMismatchError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(1), target.Expected)
		assert.Equal(t, uint64(2), target.Observed)
	})

	t.Run("NewNoFollowersError", func(t *testing.T) {
		de := NewNoFollowersError("raft.Replicate", "in shard 1")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *NoFollowersError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "in shard 1", target.Context)
	})

	t.Run("NewTimeoutWaitingError", func(t *testing.T) {
		cause := errors.New("context done")
		de := NewTimeoutWaitingError("raft.WaitLeader", "leader election", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrTimeout, de.Code)

		var target *TimeoutWaitingError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "leader election", target.Subject)
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("NewChannelNotRegisteredError", func(t *testing.T) {
		de := NewChannelNotRegisteredError("raft.Send", 42)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrNotFound, de.Code)

		var target *ChannelNotRegisteredError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(42), target.NodeID)
	})

	t.Run("NewContextCancelledError", func(t *testing.T) {
		cause := errors.New("user cancel")
		de := NewContextCancelledError("raft.Propose", "propose", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrTimeout, de.Code)

		var target *ContextCancelledError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "propose", target.Operation)
		assert.Equal(t, cause, target.Unwrap())
	})

	t.Run("NewProposalResultError", func(t *testing.T) {
		de := NewProposalResultError("raft.Apply", dberrors.ErrInternal, 3, "rejected")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *ProposalResultError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(3), target.ShardID)
		assert.Equal(t, "rejected", target.Result)
	})

	t.Run("NewRaftGroupExistsError", func(t *testing.T) {
		de := NewRaftGroupExistsError("raft.Create", 10)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrAlreadyExists, de.Code)

		var target *RaftGroupExistsError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(10), target.ShardID)
	})

	t.Run("NewRaftGroupNotFoundError", func(t *testing.T) {
		de := NewRaftGroupNotFoundError("raft.Get", 20)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrNotFound, de.Code)

		var target *RaftGroupNotFoundError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(20), target.ShardID)
	})

	t.Run("NewConfigMultiValidationError", func(t *testing.T) {
		errs := []string{"missing host", "invalid port"}
		de := NewConfigMultiValidationError("raft.Config", errs)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *ConfigMultiValidationError
		require.True(t, errors.As(de.Err, &target))
		assert.Len(t, target.Errors, 2)
	})

	t.Run("NewInvalidWriteModeError", func(t *testing.T) {
		de := NewInvalidWriteModeError("raft.Apply", "turbo", nil)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *InvalidWriteModeError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "turbo", target.Mode)
	})

	t.Run("NewUnsupportedConsistencyLevelError", func(t *testing.T) {
		de := NewUnsupportedConsistencyLevelError("raft.Read", "eventual")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *UnsupportedConsistencyLevelError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "eventual", target.Level)
	})

	t.Run("NewNoLeaderAvailableError", func(t *testing.T) {
		de := NewNoLeaderAvailableError("raft.Propose", 8)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrLeaderNotAvailable, de.Code)

		var target *NoLeaderAvailableError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(8), target.ShardID)
	})

	t.Run("NewPeerNotFoundError", func(t *testing.T) {
		de := NewPeerNotFoundError("raft.Remove", 15)
		require.NotNil(t, de)

		var target *PeerNotFoundError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(15), target.NodeID)
	})

	t.Run("NewEmptyNodeAddressError", func(t *testing.T) {
		de := NewEmptyNodeAddressError("raft.Validate", 6)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *EmptyNodeAddressError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, uint64(6), target.NodeID)
	})

	t.Run("NewFieldNotStringError", func(t *testing.T) {
		de := NewFieldNotStringError("raft.Parse", "name", "int")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInvalidInput, de.Code)

		var target *FieldNotStringError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "name", target.Field)
		assert.Equal(t, "int", target.Actual)
	})
}

// ---------------------------------------------------------------------------
// errors_raft.go — New* constructors with Cause (Unwrap chain tests)
// ---------------------------------------------------------------------------

func TestRaftConstructors_WithCause(t *testing.T) {
	cause := errors.New("underlying failure")

	tests := []struct {
		name     string
		buildFn  func() *dberrors.QBError
		wantCode dberrors.ErrorCode
	}{
		{
			"NewNodeHostCreationError",
			func() *dberrors.QBError { return NewNodeHostCreationError("raft.Init", cause) },
			dberrors.ErrRaft,
		},
		{
			"NewGroupConfigValidationError",
			func() *dberrors.QBError { return NewGroupConfigValidationError("raft.Config", cause) },
			dberrors.ErrValidation,
		},
		{
			"NewRaftConfigCreationError",
			func() *dberrors.QBError { return NewRaftConfigCreationError("raft.Config", cause) },
			dberrors.ErrConfiguration,
		},
		{
			"NewSessionError",
			func() *dberrors.QBError { return NewSessionError("raft.GetSession", cause) },
			dberrors.ErrRaft,
		},
		{
			"NewReadOptionsValidationError",
			func() *dberrors.QBError { return NewReadOptionsValidationError("raft.Read", cause) },
			dberrors.ErrInvalidInput,
		},
		{
			"NewDiscoveryError",
			func() *dberrors.QBError { return NewDiscoveryError("raft.Discover", cause) },
			dberrors.ErrUnavailable,
		},
		{
			"NewClusterDetectionError",
			func() *dberrors.QBError { return NewClusterDetectionError("raft.Detect", cause) },
			dberrors.ErrInternal,
		},
		{
			"NewRaftGroupStartError",
			func() *dberrors.QBError { return NewRaftGroupStartError("raft.Start", cause) },
			dberrors.ErrRaft,
		},
		{
			"NewLeaderElectionError",
			func() *dberrors.QBError { return NewLeaderElectionError("raft.Elect", cause) },
			dberrors.ErrLeaderNotAvailable,
		},
		{
			"NewClusterJoinError",
			func() *dberrors.QBError { return NewClusterJoinError("raft.Join", cause) },
			dberrors.ErrRaft,
		},
		{
			"NewClientContextCanceledError",
			func() *dberrors.QBError { return NewClientContextCanceledError("raft.Call", cause) },
			dberrors.ErrCanceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			de := tt.buildFn()
			require.NotNil(t, de)
			assert.Equal(t, tt.wantCode, de.Code)
			assert.NotEmpty(t, de.Error())

			// Verify Unwrap chain reaches original cause.
			type unwrapper interface{ Unwrap() error }
			inner, ok := de.Err.(unwrapper)
			require.True(t, ok, "inner error should implement Unwrap")
			assert.Equal(t, cause, inner.Unwrap())
		})
	}
}

// ---------------------------------------------------------------------------
// errors_raft.go — MTLSCertReadError
// ---------------------------------------------------------------------------

func TestMTLSCertReadErrorConstructor(t *testing.T) {
	cause := errors.New("file not found")

	t.Run("Constructor", func(t *testing.T) {
		de := NewMTLSCertReadError("raft.TLS", "/etc/cert.pem", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *MTLSCertReadError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "/etc/cert.pem", target.File)
		assert.Equal(t, cause, target.Unwrap())
		assert.Contains(t, target.Error(), "/etc/cert.pem")
	})
}

// ---------------------------------------------------------------------------
// errors_vector.go — IVFClusteringFailedError
// ---------------------------------------------------------------------------

func TestIVFClusteringFailedErrorConstructor(t *testing.T) {
	cause := errors.New("convergence failed")

	t.Run("Error", func(t *testing.T) {
		e := &IVFClusteringFailedError{Cause: cause}
		assert.Equal(t, "k-means clustering failed: convergence failed", e.Error())
	})

	t.Run("Unwrap", func(t *testing.T) {
		e := &IVFClusteringFailedError{Cause: cause}
		assert.Equal(t, cause, e.Unwrap())
	})

	t.Run("Constructor", func(t *testing.T) {
		de := NewIVFClusteringFailedError("vector.Train", cause)
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *IVFClusteringFailedError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, cause, target.Cause)
	})
}

// ---------------------------------------------------------------------------
// seal_errors.go — NoSealedKeyDataError, AutoUnsealRequirementsError,
//                  TPMNotCompiledError
// ---------------------------------------------------------------------------

func TestSealErrorConstructors(t *testing.T) {
	t.Run("NoSealedKeyData_Error", func(t *testing.T) {
		e := &NoSealedKeyDataError{}
		assert.Equal(t, "no sealed key data", e.Error())
	})

	t.Run("NoSealedKeyData_Constructor", func(t *testing.T) {
		de := NewNoSealedKeyDataError("seal.Unseal")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrInternal, de.Code)

		var target *NoSealedKeyDataError
		require.True(t, errors.As(de.Err, &target))
	})

	t.Run("AutoUnsealRequirements_Error", func(t *testing.T) {
		e := &AutoUnsealRequirementsError{}
		assert.Equal(t, "TPM and share stores are required for auto unseal", e.Error())
	})

	t.Run("AutoUnsealRequirements_Constructor", func(t *testing.T) {
		de := NewAutoUnsealRequirementsError("seal.AutoUnseal")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrPreconditionFailed, de.Code)

		var target *AutoUnsealRequirementsError
		require.True(t, errors.As(de.Err, &target))
	})

	t.Run("TPMNotCompiled_Error", func(t *testing.T) {
		e := &TPMNotCompiledError{DevicePath: "/dev/tpm0"}
		assert.Contains(t, e.Error(), "/dev/tpm0")
		assert.Contains(t, e.Error(), "TPM support not compiled")
	})

	t.Run("TPMNotCompiled_Constructor", func(t *testing.T) {
		de := NewTPMNotCompiledError("seal.Init", "/dev/tpmrm0")
		require.NotNil(t, de)
		assert.Equal(t, dberrors.ErrConfiguration, de.Code)

		var target *TPMNotCompiledError
		require.True(t, errors.As(de.Err, &target))
		assert.Equal(t, "/dev/tpmrm0", target.DevicePath)
	})
}

// =============================================================================
// Consolidated from errors_coverage_test.go
// =============================================================================

func TestUnsupportedQueryTypeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnsupportedQueryTypeError{TypeName: "fuzzy"}
		if got := e.Error(); got != "unsupported query type: fuzzy" {
			t.Errorf("got %q, want %q", got, "unsupported query type: fuzzy")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewUnsupportedQueryTypeError("query.Execute", "fuzzy")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
		var target *UnsupportedQueryTypeError
		if !errors.As(de.Err, &target) {
			t.Fatal("expected UnsupportedQueryTypeError")
		}
		if target.TypeName != "fuzzy" {
			t.Errorf("TypeName = %q, want %q", target.TypeName, "fuzzy")
		}
	})
}

func TestUnexpectedResultTypeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnexpectedResultTypeError{TypeName: "float64"}
		if got := e.Error(); got != "unexpected result type: float64" {
			t.Errorf("got %q, want %q", got, "unexpected result type: float64")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewUnexpectedResultTypeError("query.Parse", "float64")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})
}

func TestUnsupportedModeOperationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnsupportedModeOperationError{Operation: "rebalance", Mode: "standalone"}
		if got := e.Error(); got != "rebalance not supported in standalone" {
			t.Errorf("got %q, want %q", got, "rebalance not supported in standalone")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewUnsupportedModeOperationError("server.Op", "rebalance", "standalone")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestShardNotFoundInModeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ShardNotFoundInModeError{ShardID: 42, Mode: "cluster"}
		expected := "shard 42 not found in cluster"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewShardNotFoundInModeError("shard.Get", 42, "cluster")
		if de.Code != dberrors.ErrNotFound {
			t.Errorf("code = %v, want ErrNotFound", de.Code)
		}
	})
}

func TestUnknownServerModeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownServerModeError{Mode: "alien"}
		got := e.Error()
		if got == "" {
			t.Error("expected non-empty error string")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownServerModeError("server.Init", "alien")
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v, want ErrConfiguration", de.Code)
		}
	})
}

func TestServerStateError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ServerStateError{State: "shutting down"}
		if got := e.Error(); got != "shutting down" {
			t.Errorf("got %q, want %q", got, "shutting down")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewServerStateError("server.Handle", "shutting down")
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})
}

func TestNoListenersConfiguredError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NoListenersConfiguredError{Message: "no listeners"}
		if got := e.Error(); got != "no listeners" {
			t.Errorf("got %q, want %q", got, "no listeners")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewNoListenersConfiguredError("server.Start", "no listeners")
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v, want ErrConfiguration", de.Code)
		}
	})
}

func TestUnknownProviderError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &UnknownProviderError{Provider: "vault"}
		if got := e.Error(); got != "unknown provider: vault" {
			t.Errorf("got %q, want %q", got, "unknown provider: vault")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewUnknownProviderError("kms.Init", "vault")
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v, want ErrConfiguration", de.Code)
		}
	})
}

func TestEntityShardPinError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &EntityShardPinError{EntityType: "user", ShardID: 99}
		expected := `entity "user" pinned to unknown shard 99`
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewEntityShardPinError("shard.Route", "user", 99)
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v, want ErrConfiguration", de.Code)
		}
	})
}

func TestEncryptionKeySizeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &EncryptionKeySizeError{Expected: 32, Actual: 16}
		expected := "encryption key must be 32 bytes, got 16 bytes"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewEncryptionKeySizeError("crypto.Init", 32, 16)
		if de.Code != dberrors.ErrConfiguration {
			t.Errorf("code = %v, want ErrConfiguration", de.Code)
		}
	})
}

func TestCertGenerationError(t *testing.T) {
	cause := fmt.Errorf("key gen failed")
	t.Run("Error", func(t *testing.T) {
		e := &CertGenerationError{Step: "generate key", Cause: cause}
		expected := "failed to generate key: key gen failed"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &CertGenerationError{Step: "sign", Cause: cause}
		if e.Unwrap() != cause {
			t.Errorf("Unwrap returned wrong error")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewCertGenerationError("tls.Setup", "generate key", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})
}

func TestDataDirOperationError(t *testing.T) {
	cause := fmt.Errorf("permission denied")
	t.Run("Error", func(t *testing.T) {
		e := &DataDirOperationError{Action: "create", Path: "/data", Cause: cause}
		expected := "failed to create /data: permission denied"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Unwrap", func(t *testing.T) {
		e := &DataDirOperationError{Action: "create", Path: "/data", Cause: cause}
		if e.Unwrap() != cause {
			t.Errorf("Unwrap returned wrong error")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewDataDirOperationError("server.Init", "create", "/data", cause)
		if de.Code != dberrors.ErrInternal {
			t.Errorf("code = %v, want ErrInternal", de.Code)
		}
	})
}

func TestStateMachineClosedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &StateMachineClosedError{}
		if got := e.Error(); got != "state machine is closed" {
			t.Errorf("got %q, want %q", got, "state machine is closed")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewStateMachineClosedError("sm.Update")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v, want ErrUnavailable", de.Code)
		}
	})
}

func TestNilFieldError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &NilFieldError{Field: "config"}
		if got := e.Error(); got != "config must not be nil" {
			t.Errorf("got %q, want %q", got, "config must not be nil")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewNilFieldError("server.Init", "config")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestEmptyFieldError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &EmptyFieldError{Field: "name"}
		if got := e.Error(); got != "name must not be empty" {
			t.Errorf("got %q, want %q", got, "name must not be empty")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewEmptyFieldError("user.Create", "name")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestKeyLengthError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &KeyLengthError{Expected: 32, Actual: 24}
		expected := "expected key length 32, got 24"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewKeyLengthError("crypto.Validate", 32, 24)
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestKeyTooShortError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &KeyTooShortError{Component: "prefix"}
		if got := e.Error(); got != "key too short for prefix" {
			t.Errorf("got %q, want %q", got, "key too short for prefix")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewKeyTooShortError("key.Parse", "prefix")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestFeatureDisabledError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &FeatureDisabledError{Feature: "encryption"}
		if got := e.Error(); got != "encryption not enabled" {
			t.Errorf("got %q, want %q", got, "encryption not enabled")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewFeatureDisabledError("server.Encrypt", "encryption")
		if de.Code != dberrors.ErrPreconditionFailed {
			t.Errorf("code = %v, want ErrPreconditionFailed", de.Code)
		}
	})
}

func TestZeroValueError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &ZeroValueError{Field: "port"}
		if got := e.Error(); got != "port must not be zero" {
			t.Errorf("got %q, want %q", got, "port must not be zero")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewZeroValueError("config.Validate", "port")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}

func TestHTTPStatusError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &HTTPStatusError{StatusCode: 503, Body: "service unavailable"}
		expected := "HTTP 503: service unavailable"
		if got := e.Error(); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewHTTPStatusError("http.Do", dberrors.ErrUnavailable, 503, "service unavailable")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v, want ErrUnavailable", de.Code)
		}
	})
}

func TestRemoteError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &RemoteError{Message: "connection refused"}
		if got := e.Error(); got != "connection refused" {
			t.Errorf("got %q, want %q", got, "connection refused")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewRemoteError("rpc.Call", dberrors.ErrUnavailable, "connection refused")
		if de.Code != dberrors.ErrUnavailable {
			t.Errorf("code = %v, want ErrUnavailable", de.Code)
		}
	})
}

func TestMinimumValueError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		e := &MinimumValueError{Field: "replicas", Minimum: "3"}
		if got := e.Error(); got != "replicas must be at least 3" {
			t.Errorf("got %q, want %q", got, "replicas must be at least 3")
		}
	})
	t.Run("Constructor", func(t *testing.T) {
		de := NewMinimumValueError("config.Validate", "replicas", "3")
		if de.Code != dberrors.ErrInvalidInput {
			t.Errorf("code = %v, want ErrInvalidInput", de.Code)
		}
	})
}
