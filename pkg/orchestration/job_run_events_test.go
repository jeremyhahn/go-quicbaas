// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strconv"
	"testing"
)

// TestJobRun_AFullEventStreamKeepsTheNewestEvent is X15's lost ejection: a job
// whose stream nobody drained dropped every new event once the buffer filled,
// so the ejection it announced was the event thrown away. The newest event
// must be delivered, and it must say how many older ones it displaced.
func TestJobRun_AFullEventStreamKeepsTheNewestEvent(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	for i := 0; i < defaultEventBufferSize; i++ {
		run.Emit(JobStateReviewing, "poll "+strconv.Itoa(i))
	}
	run.Emit(JobStateReviewing, "ejected from merge train #80 (closed)")
	run.Emit(JobStateReviewing, "ejected from merge train #81 (closed)")

	events := drainEvents(run)
	if len(events) != defaultEventBufferSize {
		t.Fatalf("stream held %d events, want the buffer's %d", len(events), defaultEventBufferSize)
	}
	if events[0].Message != "poll 2" {
		t.Fatalf("oldest kept event = %q, want the two oldest coalesced away", events[0].Message)
	}
	last := events[len(events)-1]
	if last.Message != "ejected from merge train #81 (closed)" {
		t.Fatalf("newest event = %q, want the last ejection", last.Message)
	}
	if got := last.Metadata[JobEventMetadataCoalesced]; got != "2" {
		t.Fatalf("coalesced count on the newest event = %q, want 2", got)
	}
	if got := events[len(events)-2].Metadata[JobEventMetadataCoalesced]; got != "1" {
		t.Fatalf("coalesced count on the first displacing event = %q, want 1", got)
	}
}

// TestJobRun_AStreamWithRoomCarriesNoCoalescedCount: nothing displaced,
// nothing said, and a caller's metadata is never written.
func TestJobRun_AStreamWithRoomCarriesNoCoalescedCount(t *testing.T) {
	t.Parallel()

	run := parkTestRun()
	run.Emit(JobStateExecuting, "working")
	events := drainEvents(run)
	if len(events) != 1 {
		t.Fatalf("stream held %d events, want 1", len(events))
	}
	if _, ok := events[0].Metadata[JobEventMetadataCoalesced]; ok {
		t.Fatalf("an event with room was marked coalesced: %v", events[0].Metadata)
	}

	caller := map[string]string{"stage": "prepare"}
	coalesced := withCoalesced(caller, 3)
	if _, ok := caller[JobEventMetadataCoalesced]; ok {
		t.Fatal("withCoalesced wrote the caller's map")
	}
	if coalesced["stage"] != "prepare" || coalesced[JobEventMetadataCoalesced] != "3" {
		t.Fatalf("withCoalesced = %v, want the caller's keys plus the count", coalesced)
	}
}
