package iam

import (
	"encoding/binary"
	"encoding/hex"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// auditLogIDPrefix prefixes every audit log ID.
const auditLogIDPrefix = "audit_"

// auditLogIDEntropyBytes is the size of the random half of an audit log ID.
const auditLogIDEntropyBytes = 8

// auditLogIDEntropy is the randomness generateAuditLogID draws on. It is a
// variable so a test can make the CSPRNG fail without touching the
// process-global RNG state every other test in this package shares.
var auditLogIDEntropy = xkms.Rand

// auditIDGenerator issues audit log IDs that are unique by construction.
//
// An ID is "audit_" followed by 32 hex digits: an 8-byte big-endian
// timestamp and 8 random bytes. The timestamp is the wall clock in Unix
// nanoseconds, raised to one past the last value issued whenever the clock
// has not advanced (or has stepped back), so within a process every ID has
// a strictly greater timestamp than the one before it. Uniqueness inside a
// process therefore never depends on the clock or on the random bytes; the
// random bytes only separate IDs issued by different processes, and IDs
// sort by issue order within a process.
//
// The ID is the key the audit store files an event under, not a secret, so
// the property it must keep is uniqueness. A CSPRNG failure is refused with
// AuditLogIDGenerationError rather than falling back to a weaker ID, and the
// caller does not perform the audited operation.
type auditIDGenerator struct {
	now    func() time.Time
	random func(n int) ([]byte, error)

	// last is the most recent timestamp issued, in Unix nanoseconds.
	last atomic.Int64
}

// defaultAuditIDGenerator serves generateAuditLogID. It reads
// auditLogIDEntropy on every call, so a test that replaces the variable
// reaches it.
var defaultAuditIDGenerator = newAuditIDGenerator(time.Now, func(n int) ([]byte, error) {
	return auditLogIDEntropy(n)
})

func newAuditIDGenerator(now func() time.Time, random func(n int) ([]byte, error)) *auditIDGenerator {
	return &auditIDGenerator{now: now, random: random}
}

// generateAuditLogID returns a new audit log ID, or an
// *AuditLogIDGenerationError when the random source fails.
func generateAuditLogID() (string, error) {
	return defaultAuditIDGenerator.next()
}

// next returns a new audit log ID.
func (g *auditIDGenerator) next() (string, error) {
	random, err := g.random(auditLogIDEntropyBytes)
	if err != nil {
		return "", &AuditLogIDGenerationError{Cause: err}
	}
	var id [16]byte
	binary.BigEndian.PutUint64(id[:8], uint64(g.nextTimestamp()))
	copy(id[8:], random)
	return auditLogIDPrefix + hex.EncodeToString(id[:]), nil
}

// nextTimestamp returns the current time in Unix nanoseconds, or one past
// the last value issued if the clock has not moved beyond it.
func (g *auditIDGenerator) nextTimestamp() int64 {
	now := g.now().UnixNano()
	for {
		last := g.last.Load()
		next := max(now, last+1)
		if g.last.CompareAndSwap(last, next) {
			return next
		}
	}
}
