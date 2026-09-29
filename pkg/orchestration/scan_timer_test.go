package orchestration

import (
	"strings"
	"testing"
	"time"
)

func TestScanTimerReportsSlowScan(t *testing.T) {
	logger := &recordingLogger{}
	timer := &scanTimer{start: time.Now().Add(-2 * slowScanThreshold)}
	timer.observe(10)
	timer.observe(32)

	timer.warnIfSlow(logger, "jobs", "acme")

	got := logger.atLevel("WARN")
	if !strings.Contains(got, "2 records, 42 value bytes") || !strings.Contains(got, "tenant acme") {
		t.Fatalf("warning %q lacks the scan's shape", got)
	}
}

func TestScanTimerQuietWhenFast(t *testing.T) {
	logger := &recordingLogger{}
	timer := newScanTimer()
	timer.observe(10)

	timer.warnIfSlow(logger, "jobs", "acme")

	if got := logger.atLevel("WARN"); got != "" {
		t.Fatalf("a fast scan was reported: %s", got)
	}
}
