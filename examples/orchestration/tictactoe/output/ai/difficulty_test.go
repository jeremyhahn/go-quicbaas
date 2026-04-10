package ai

import "testing"

func TestDifficulty_String(t *testing.T) {
	tests := []struct {
		name     string
		d        Difficulty
		expected string
	}{
		{"easy", Easy, "easy"},
		{"hard", Hard, "hard"},
		{"unknown", Difficulty(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.String(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
