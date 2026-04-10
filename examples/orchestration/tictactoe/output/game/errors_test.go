package game

import (
	"errors"
	"testing"
)

func TestInvalidPositionError_Message(t *testing.T) {
	tests := []struct {
		name     string
		row, col int
		wantSub  string
	}{
		{"negative row", -1, 0, "row=-1"},
		{"large column", 0, 5, "col=5"},
		{"both out of range", 10, 10, "row=10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &InvalidPositionError{Row: tc.row, Col: tc.col}
			msg := err.Error()
			if msg == "" {
				t.Fatal("expected non-empty error message")
			}
			if !containsSubstring(msg, tc.wantSub) {
				t.Errorf("message %q does not contain %q", msg, tc.wantSub)
			}
		})
	}
}

func TestInvalidPositionError_Implements(t *testing.T) {
	var err error = &InvalidPositionError{Row: -1, Col: 0}
	var target *InvalidPositionError
	if !errors.As(err, &target) {
		t.Fatal("InvalidPositionError does not implement the error interface correctly")
	}
}

func TestCellOccupiedError_Message(t *testing.T) {
	err := &CellOccupiedError{Row: 1, Col: 2, Symbol: X}
	msg := err.Error()
	if !containsSubstring(msg, "1, 2") {
		t.Errorf("message %q missing position info", msg)
	}
	if !containsSubstring(msg, "X") {
		t.Errorf("message %q missing symbol", msg)
	}
}

func TestCellOccupiedError_Implements(t *testing.T) {
	var err error = &CellOccupiedError{Row: 0, Col: 0, Symbol: O}
	var target *CellOccupiedError
	if !errors.As(err, &target) {
		t.Fatal("CellOccupiedError does not implement the error interface correctly")
	}
}

func TestGameOverError_Message(t *testing.T) {
	tests := []struct {
		name    string
		state   State
		wantSub string
	}{
		{"x wins", XWins, "X wins"},
		{"o wins", OWins, "O wins"},
		{"draw", Draw, "draw"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &GameOverError{FinalState: tc.state}
			msg := err.Error()
			if !containsSubstring(msg, tc.wantSub) {
				t.Errorf("message %q does not contain %q", msg, tc.wantSub)
			}
		})
	}
}

func TestGameOverError_Implements(t *testing.T) {
	var err error = &GameOverError{FinalState: Draw}
	var target *GameOverError
	if !errors.As(err, &target) {
		t.Fatal("GameOverError does not implement the error interface correctly")
	}
}

// containsSubstring is a small helper to avoid importing strings in tests.
func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
