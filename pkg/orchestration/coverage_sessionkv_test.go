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
	"context"
	"strings"
	"testing"
)

// =============================================================================
// session_log_kv.go — parseSequenceFromKey
// =============================================================================

func TestParseSequenceFromKey(t *testing.T) {
	prefix := "log/t1/s1/events/"

	tests := []struct {
		name    string
		key     string
		wantSeq uint64
		wantOK  bool
	}{
		{"valid", prefix + "00000000000000000042", 42, true},
		{"valid zero", prefix + "00000000000000000000", 0, true},
		{"wrong prefix", "other/00000000000000000042", 0, false},
		{"short suffix", prefix + "42", 0, false},
		{"non-digit suffix", prefix + "0000000000000000004x", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq, ok := parseSequenceFromKey(tt.key, prefix)
			if ok != tt.wantOK || seq != tt.wantSeq {
				t.Fatalf("parseSequenceFromKey(%q) = (%d, %v), want (%d, %v)",
					tt.key, seq, ok, tt.wantSeq, tt.wantOK)
			}
		})
	}
}

// =============================================================================
// session_log_kv.go — decodeEvent (plaintext path)
// =============================================================================

func TestRange_EmptyLogReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: newMemKVStore()})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}

	page, next, rangeErr := log.Range(ctx, "t1", "s1", 1, 0)
	if rangeErr != nil {
		t.Fatalf("unexpected error on empty range: %v", rangeErr)
	}
	if len(page) != 0 || next != 0 {
		t.Fatalf("expected empty result, got len=%d next=%d", len(page), next)
	}
}

// TestRange_CorruptStoredBytes verifies that a stored byte slice that is not
// valid JSON causes Range to return an unmarshal error.
func TestRange_CorruptStoredBytes(t *testing.T) {
	ctx := context.Background()
	kv := newMemKVStore()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}

	// Manually inject a non-JSON value at a valid event key.
	_ = kv.Put(ctx, log.eventKey("t1", "s1", 1), []byte("{not-json"))

	_, _, rangeErr := log.Range(ctx, "t1", "s1", 1, 0)
	if rangeErr == nil {
		t.Fatal("expected unmarshal error for corrupt stored bytes")
	}
	if !strings.Contains(rangeErr.Error(), "unmarshal event") {
		t.Fatalf("unexpected error: %v", rangeErr)
	}
}
