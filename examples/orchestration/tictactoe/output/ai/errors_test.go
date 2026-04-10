package ai

import "testing"

func TestNoMovesAvailableError_Message(t *testing.T) {
	err := &NoMovesAvailableError{}
	expected := "no moves available: board is full"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestNoMovesAvailableError_ImplementsError(t *testing.T) {
	var err error = &NoMovesAvailableError{}
	if err == nil {
		t.Fatal("expected non-nil error interface")
	}
}

func TestGameNotPlayingError_Message(t *testing.T) {
	err := &GameNotPlayingError{Reason: "X wins"}
	expected := "cannot move: game is not in playing state (X wins)"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGameNotPlayingError_ImplementsError(t *testing.T) {
	var err error = &GameNotPlayingError{Reason: "draw"}
	if err == nil {
		t.Fatal("expected non-nil error interface")
	}
}
