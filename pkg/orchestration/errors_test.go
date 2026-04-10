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

package orchestration

import (
	"errors"
	"io"
	"testing"
)

func TestSessionNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &SessionNotFoundError{SessionID: "sess-123"}
		want := "acp: session not found: sess-123"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &SessionNotFoundError{SessionID: "sess-123"}
		if !errors.Is(err, ErrSessionNotFound) {
			t.Error("errors.Is(err, ErrSessionNotFound) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *SessionNotFoundError
		err := error(&SessionNotFoundError{SessionID: "sess-456"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.SessionID != "sess-456" {
			t.Errorf("SessionID = %q, want %q", target.SessionID, "sess-456")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &SessionNotFoundError{SessionID: "x"}
		if errors.Is(err, ErrTransportFailed) {
			t.Error("should not match ErrTransportFailed")
		}
	})
}

func TestTransportError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		cause := io.EOF
		err := &TransportError{Operation: "write", Cause: cause}
		want := "acp: transport failed: write: EOF"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("UnwrapSentinel", func(t *testing.T) {
		err := &TransportError{Operation: "read", Cause: io.EOF}
		if !errors.Is(err, ErrTransportFailed) {
			t.Error("errors.Is(err, ErrTransportFailed) = false, want true")
		}
	})

	t.Run("UnwrapCause", func(t *testing.T) {
		err := &TransportError{Operation: "read", Cause: io.EOF}
		if !errors.Is(err, io.EOF) {
			t.Error("errors.Is(err, io.EOF) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *TransportError
		err := error(&TransportError{Operation: "send", Cause: io.ErrClosedPipe})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Operation != "send" {
			t.Errorf("Operation = %q, want %q", target.Operation, "send")
		}
	})
}

func TestProtocolError(t *testing.T) {
	t.Run("ErrorWithCause", func(t *testing.T) {
		err := &ProtocolError{Message: "bad response", Cause: io.EOF}
		want := "acp: protocol violation: bad response: EOF"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("ErrorWithoutCause", func(t *testing.T) {
		err := &ProtocolError{Message: "missing field"}
		want := "acp: protocol violation: missing field"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("UnwrapSentinel", func(t *testing.T) {
		err := &ProtocolError{Message: "test"}
		if !errors.Is(err, ErrProtocolViolation) {
			t.Error("errors.Is(err, ErrProtocolViolation) = false, want true")
		}
	})

	t.Run("UnwrapCause", func(t *testing.T) {
		err := &ProtocolError{Message: "test", Cause: io.EOF}
		if !errors.Is(err, io.EOF) {
			t.Error("errors.Is(err, io.EOF) = false, want true")
		}
	})

	t.Run("UnwrapNoCauseDoesNotMatchEOF", func(t *testing.T) {
		err := &ProtocolError{Message: "test"}
		if errors.Is(err, io.EOF) {
			t.Error("errors.Is(err, io.EOF) = true, want false")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *ProtocolError
		err := error(&ProtocolError{Message: "parse error", Cause: io.ErrUnexpectedEOF})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Message != "parse error" {
			t.Errorf("Message = %q, want %q", target.Message, "parse error")
		}
	})
}

func TestBackendNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &BackendNotFoundError{Backend: "unknown"}
		want := "acp: backend not found: unknown"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &BackendNotFoundError{Backend: "unknown"}
		if !errors.Is(err, ErrBackendNotFound) {
			t.Error("errors.Is(err, ErrBackendNotFound) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *BackendNotFoundError
		err := error(&BackendNotFoundError{Backend: "foo"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Backend != "foo" {
			t.Errorf("Backend = %q, want %q", target.Backend, "foo")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &BackendNotFoundError{Backend: "x"}
		if errors.Is(err, ErrSessionNotFound) {
			t.Error("should not match ErrSessionNotFound")
		}
	})
}

func TestUnsupportedOperationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &UnsupportedOperationError{Backend: "ollama", Operation: "spawn"}
		want := "acp: unsupported operation: ollama does not support spawn"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &UnsupportedOperationError{Backend: "ollama", Operation: "spawn"}
		if !errors.Is(err, ErrUnsupportedOperation) {
			t.Error("errors.Is(err, ErrUnsupportedOperation) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *UnsupportedOperationError
		err := error(&UnsupportedOperationError{Backend: "test", Operation: "close"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Backend != "test" || target.Operation != "close" {
			t.Errorf("got Backend=%q Operation=%q", target.Backend, target.Operation)
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &UnsupportedOperationError{Backend: "x", Operation: "y"}
		if errors.Is(err, ErrSpawnFailed) {
			t.Error("should not match ErrSpawnFailed")
		}
	})
}

func TestSpawnError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &SpawnError{Command: "claude", Cause: io.EOF}
		want := "acp: spawn failed: claude: EOF"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("UnwrapSentinel", func(t *testing.T) {
		err := &SpawnError{Command: "claude", Cause: io.EOF}
		if !errors.Is(err, ErrSpawnFailed) {
			t.Error("errors.Is(err, ErrSpawnFailed) = false, want true")
		}
	})

	t.Run("UnwrapCause", func(t *testing.T) {
		err := &SpawnError{Command: "claude", Cause: io.EOF}
		if !errors.Is(err, io.EOF) {
			t.Error("errors.Is(err, io.EOF) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *SpawnError
		err := error(&SpawnError{Command: "codex", Cause: io.ErrClosedPipe})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Command != "codex" {
			t.Errorf("Command = %q, want %q", target.Command, "codex")
		}
	})
}

func TestPromptError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &PromptError{SessionID: "sess-1", Cause: io.EOF}
		want := "acp: prompt failed: session sess-1: EOF"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("UnwrapSentinel", func(t *testing.T) {
		err := &PromptError{SessionID: "sess-1", Cause: io.EOF}
		if !errors.Is(err, ErrPromptFailed) {
			t.Error("errors.Is(err, ErrPromptFailed) = false, want true")
		}
	})

	t.Run("UnwrapCause", func(t *testing.T) {
		err := &PromptError{SessionID: "sess-1", Cause: io.EOF}
		if !errors.Is(err, io.EOF) {
			t.Error("errors.Is(err, io.EOF) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *PromptError
		err := error(&PromptError{SessionID: "sess-2", Cause: io.ErrUnexpectedEOF})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.SessionID != "sess-2" {
			t.Errorf("SessionID = %q, want %q", target.SessionID, "sess-2")
		}
	})
}

func TestInputValidationError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &InputValidationError{Field: "prompt", Message: "cannot be empty"}
		want := "acp: invalid input: prompt: cannot be empty"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &InputValidationError{Field: "prompt", Message: "cannot be empty"}
		if !errors.Is(err, ErrInvalidInput) {
			t.Error("errors.Is(err, ErrInvalidInput) = false, want true")
		}
	})

	t.Run("As", func(t *testing.T) {
		var target *InputValidationError
		err := error(&InputValidationError{Field: "model", Message: "required"})
		if !errors.As(err, &target) {
			t.Fatal("errors.As failed")
		}
		if target.Field != "model" || target.Message != "required" {
			t.Errorf("got Field=%q Message=%q", target.Field, target.Message)
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &InputValidationError{Field: "x", Message: "y"}
		if errors.Is(err, ErrPromptFailed) {
			t.Error("should not match ErrPromptFailed")
		}
	})
}
