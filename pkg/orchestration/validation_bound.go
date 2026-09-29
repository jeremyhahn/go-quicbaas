package orchestration

import (
	"strconv"
	"unicode/utf8"
)

// MaxValidationTextBytes bounds each text field a validation result keeps.
//
// A result is persisted on its job record and re-read by every job listing.
// An unbounded one carried the whole stdout of a passing `make test` — 18 MB
// on one burn-in job, 455 MB across the tenant's job history — and every
// Jobs page decoded all of it (4.4 s). The END of a command's output is what
// names the failure, so the bound keeps the tail.
const MaxValidationTextBytes = 16 << 10

// elideHead returns the last MaxValidationTextBytes of kept, prefixed with a
// marker naming how many earlier bytes were dropped. total is how many bytes
// the text had before anything was dropped, which is larger than len(kept)
// when a tailWriter already discarded the front.
func elideHead(kept string, total int) string {
	return elideHeadTo(kept, total, MaxValidationTextBytes)
}

// elideHeadTo is elideHead with an explicit bound.
func elideHeadTo(kept string, total, limit int) string {
	if total <= limit {
		return kept
	}
	tail := kept[len(kept)-min(len(kept), limit):]
	// Never open on the middle of a multi-byte rune.
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return "[… " + strconv.Itoa(total-len(tail)) + " earlier bytes elided]\n" + tail
}

// bounded returns the result with its Message and Details cut to their tails.
func (r ValidationResult) bounded() ValidationResult {
	r.Message = elideHead(r.Message, len(r.Message))
	r.Details = elideHead(r.Details, len(r.Details))
	return r
}

// oversized reports whether either text field exceeds the bound.
func (r ValidationResult) oversized() bool {
	return len(r.Message) > MaxValidationTextBytes || len(r.Details) > MaxValidationTextBytes
}

// boundVerification cuts every oversized verification result and review
// feedback entry on the job to its tail, and reports whether it changed
// anything.
func (s *JobStatus) boundVerification() bool {
	changed := false
	for i, result := range s.Verification {
		if result.oversized() {
			s.Verification[i] = result.bounded()
			changed = true
		}
	}
	if s.Memory != nil && s.Memory.boundFeedback() {
		changed = true
	}
	return changed
}

// tailWriter keeps only the last MaxValidationTextBytes written to it, so a
// command that prints megabytes is never held in memory whole.
type tailWriter struct {
	buf   []byte
	total int
}

// Write keeps the tail of p and never fails.
func (w *tailWriter) Write(p []byte) (int, error) {
	w.total += len(p)
	w.buf = append(w.buf, p...)
	// Compact only once the buffer is twice the bound, so the copy is
	// amortised over at least MaxValidationTextBytes of writes.
	if len(w.buf) > 2*MaxValidationTextBytes {
		w.buf = append(w.buf[:0], w.buf[len(w.buf)-MaxValidationTextBytes:]...)
	}
	return len(p), nil
}

// String is the kept tail, marked when earlier bytes were dropped.
func (w *tailWriter) String() string {
	return elideHead(string(w.buf), w.total)
}
