package orchestration

import (
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// slowScanThreshold is how long a record scan may take before it is reported.
// Listings are interactive: past a second the operator is waiting on it.
const slowScanThreshold = time.Second

// scanTimer measures one KV prefix scan so a slow listing says WHERE its time
// went: waiting for the store to yield the first record (the store's cost) or
// handling the records it yielded (ours).
type scanTimer struct {
	start      time.Time
	firstKey   time.Duration
	records    int
	valueBytes int
}

func newScanTimer() *scanTimer {
	return &scanTimer{start: time.Now()}
}

// observe records one yielded value.
func (t *scanTimer) observe(size int) {
	if t.records == 0 {
		t.firstKey = time.Since(t.start)
	}
	t.records++
	t.valueBytes += size
}

// warnIfSlow reports the scan when it crossed slowScanThreshold.
func (t *scanTimer) warnIfSlow(logger interfaces.Logger, kind, tenantID string) {
	elapsed := time.Since(t.start)
	if elapsed < slowScanThreshold {
		return
	}
	logger.Warnf("orchestration: slow %s scan for tenant %s: %s total, first record after %s, %d records, %d value bytes",
		kind, tenantID, elapsed, t.firstKey, t.records, t.valueBytes)
}
