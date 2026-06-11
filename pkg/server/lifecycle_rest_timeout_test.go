// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

//go:build rest

package server

import (
	"testing"
	"time"
)

// requestTimeout derives the REST handler timeout from the write timeout,
// clamped into [5s, 25s] with a 5s safety margin. It lives in this
// rest-tagged file because requestTimeout is defined in lifecycle_rest.go,
// which only builds with the rest tag.
func TestRequestTimeout(t *testing.T) {
	tests := []struct {
		name  string
		write time.Duration
		want  time.Duration
	}{
		{"zero defaults to max", 0, 25 * time.Second},
		{"negative defaults to max", -1, 25 * time.Second},
		{"below margin clamps to min", 6 * time.Second, 5 * time.Second},
		{"mid range subtracts margin", 20 * time.Second, 15 * time.Second},
		{"above max clamps to max", 60 * time.Second, 25 * time.Second},
		{"exactly margin+min", 10 * time.Second, 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestTimeout(tt.write); got != tt.want {
				t.Fatalf("requestTimeout(%v) = %v, want %v", tt.write, got, tt.want)
			}
		})
	}
}
