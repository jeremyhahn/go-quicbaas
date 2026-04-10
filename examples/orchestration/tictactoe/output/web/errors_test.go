package web

import (
	"errors"
	"fmt"
	"testing"
)

func TestInvalidDifficultyError_Message(t *testing.T) {
	err := &InvalidDifficultyError{Value: "nightmare"}
	expected := `invalid difficulty: "nightmare" (must be "easy" or "hard")`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestInvalidDifficultyError_ImplementsError(t *testing.T) {
	var err error = &InvalidDifficultyError{Value: "test"}
	if err == nil {
		t.Fatal("expected non-nil error interface")
	}
}

func TestServerStartError_Message(t *testing.T) {
	inner := fmt.Errorf("bind: address already in use")
	err := &ServerStartError{Addr: ":8080", Err: inner}
	expected := "failed to start server on :8080: bind: address already in use"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestServerStartError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("test error")
	err := &ServerStartError{Addr: ":9090", Err: inner}
	if !errors.Is(err, inner) {
		t.Error("Unwrap should return the inner error")
	}
}
