package orchestration

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"sync"
	"time"
)

// =============================================================================
// Launch policy
// =============================================================================

// LaunchPolicy is the policy a measured launch ran under. Its digest is the
// "policy hash" of the launch measurement, so a container started under a
// weaker policy carries a different measured identity than the same image
// and configuration started under a stronger one.
type LaunchPolicy struct {
	// TrustLevel is the resolved integrity tier of the launch.
	TrustLevel TrustLevel `json:"trust_level"`

	// MeasurementPCR is the PCR the launch measurement is extended into.
	MeasurementPCR int `json:"measurement_pcr"`

	// MeasurementBank is the PCR bank the measurement is extended into.
	MeasurementBank string `json:"measurement_bank"`
}

// launchPolicyDomain separates the policy digest from every other digest the
// runtime computes.
const launchPolicyDomain = "quicbaas-launch-policy/v1"

// LaunchPolicyOf returns the launch policy a spec runs under.
func LaunchPolicyOf(spec *ContainerSpec) (LaunchPolicy, error) {
	if spec == nil {
		return LaunchPolicy{}, &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	return LaunchPolicy{
		TrustLevel:      spec.TrustLevel,
		MeasurementPCR:  spec.EffectiveMeasurementPCR(),
		MeasurementBank: spec.EffectiveMeasurementBank(),
	}, nil
}

// Digest returns the hex SHA-256 of the policy's canonical encoding,
// prefixed "sha256:".
func (p LaunchPolicy) Digest() string {
	encoded, err := json.Marshal(p)
	if err != nil {
		// LaunchPolicy holds only strings and integers; Marshal cannot fail.
		panic(fmt.Sprintf("launch policy encoding: %v", err))
	}
	h := sha256.New()
	h.Write([]byte(launchPolicyDomain))
	h.Write([]byte{0})
	h.Write(encoded)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// LaunchPolicyDigest returns the policy hash of a spec's launch.
func LaunchPolicyDigest(spec *ContainerSpec) (string, error) {
	policy, err := LaunchPolicyOf(spec)
	if err != nil {
		return "", err
	}
	return policy.Digest(), nil
}

// =============================================================================
// Node measurement log
// =============================================================================

// MeasurementLogEntry is one measured launch recorded in the node
// measurement log.
type MeasurementLogEntry struct {
	// Sequence is the entry's 0-based position in the log.
	Sequence uint64 `json:"sequence"`

	// Image is the reference the caller asked for.
	Image string `json:"image"`

	// ImageDigest is the content digest that was launched.
	ImageDigest string `json:"image_digest"`

	// ConfigDigest is the deterministic digest of the launch configuration.
	ConfigDigest string `json:"config_digest"`

	// PolicyDigest is the digest of the LaunchPolicy the launch ran under.
	PolicyDigest string `json:"policy_digest"`

	// TrustLabel is the operator-assigned workload identity.
	TrustLabel string `json:"trust_label,omitempty"`

	// PCR and Bank name the register the measurement was extended into.
	PCR  int    `json:"pcr"`
	Bank string `json:"bank"`

	// Measurement is the exact data handed to HostTPM.ExtendPCR.
	Measurement []byte `json:"measurement"`

	// RecordedAt is when the entry was recorded. It is not chained: the
	// chain covers what was measured, not when.
	RecordedAt time.Time `json:"recorded_at"`

	// Chain is the hex SHA-256 over the previous entry's Chain and this
	// entry's content; the first entry chains from 32 zero bytes.
	Chain string `json:"chain"`
}

// MeasurementLogError reports a log that fails verification or replay.
type MeasurementLogError struct {
	Sequence uint64
	Reason   string
}

func (e *MeasurementLogError) Error() string {
	return fmt.Sprintf("measurement log entry %d: %s", e.Sequence, e.Reason)
}

// MeasurementLog is the node's append-only, hash-chained log of measured
// container launches. Every entry matches one HostTPM.ExtendPCR, so
// replaying the log reproduces the application PCR the node quotes.
type MeasurementLog struct {
	mu      sync.Mutex
	entries []MeasurementLogEntry
}

// NewMeasurementLog returns an empty node measurement log.
func NewMeasurementLog() *MeasurementLog {
	return &MeasurementLog{}
}

// measurementLogChainDomain separates the chain digest from other digests.
const measurementLogChainDomain = "quicbaas-measurement-log/v1"

// chainDigest computes an entry's Chain from the previous Chain.
func chainDigest(prev []byte, e *MeasurementLogEntry) []byte {
	h := sha256.New()
	h.Write([]byte(measurementLogChainDomain))
	h.Write([]byte{0})
	h.Write(prev)
	for _, field := range []string{
		fmt.Sprintf("%d", e.Sequence), e.Image, e.ImageDigest, e.ConfigDigest,
		e.PolicyDigest, e.TrustLabel, fmt.Sprintf("%d", e.PCR), e.Bank,
	} {
		h.Write([]byte{0})
		h.Write([]byte(field))
	}
	h.Write([]byte{0})
	h.Write(e.Measurement)
	return h.Sum(nil)
}

// record runs extend and, only if it succeeds, appends entry to the log.
// Both happen under the log's lock, so the log's order is the order of the
// extends and a failed extend leaves no entry.
func (l *MeasurementLog) record(entry MeasurementLogEntry, extend func() error) (MeasurementLogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := extend(); err != nil {
		return MeasurementLogEntry{}, err
	}
	prev := make([]byte, sha256.Size)
	if n := len(l.entries); n > 0 {
		decoded, err := hex.DecodeString(l.entries[n-1].Chain)
		if err != nil {
			return MeasurementLogEntry{}, &MeasurementLogError{Sequence: uint64(n - 1), Reason: "chain is not hex"}
		}
		prev = decoded
	}
	entry.Sequence = uint64(len(l.entries))
	entry.Measurement = append([]byte(nil), entry.Measurement...)
	entry.RecordedAt = time.Now().UTC()
	entry.Chain = hex.EncodeToString(chainDigest(prev, &entry))
	l.entries = append(l.entries, entry)
	return entry, nil
}

// Entries returns a copy of the log's entries in order.
func (l *MeasurementLog) Entries() []MeasurementLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]MeasurementLogEntry, len(l.entries))
	for i, e := range l.entries {
		e.Measurement = append([]byte(nil), e.Measurement...)
		out[i] = e
	}
	return out
}

// VerifyMeasurementLog checks that entries are numbered in order and that
// each entry's Chain covers its content and its predecessor.
func VerifyMeasurementLog(entries []MeasurementLogEntry) error {
	prev := make([]byte, sha256.Size)
	for i := range entries {
		e := &entries[i]
		if e.Sequence != uint64(i) {
			return &MeasurementLogError{Sequence: e.Sequence, Reason: fmt.Sprintf("out of order at position %d", i)}
		}
		want := chainDigest(prev, e)
		got, err := hex.DecodeString(e.Chain)
		if err != nil || !bytes.Equal(got, want) {
			return &MeasurementLogError{Sequence: e.Sequence, Reason: "chain digest does not match"}
		}
		prev = want
	}
	return nil
}

// pcrBankHash returns the hash constructor of a supported PCR bank.
func pcrBankHash(bank string) (func() hash.Hash, error) {
	switch bank {
	case "sha256":
		return sha256.New, nil
	case "sha384":
		return sha512.New384, nil
	case "sha512":
		return sha512.New, nil
	}
	return nil, &InputValidationError{Field: "bank", Message: fmt.Sprintf("unsupported PCR bank %q", bank)}
}

// ReplayMeasurementLog verifies entries and returns the value the PCR at
// pcr in bank holds after a reset to zero and every entry for that register
// is extended in order: PCR = H(PCR || H(measurement)), as
// HostTPM.ExtendPCR does.
func ReplayMeasurementLog(entries []MeasurementLogEntry, pcr int, bank string) ([]byte, error) {
	newHash, err := pcrBankHash(bank)
	if err != nil {
		return nil, err
	}
	if err := VerifyMeasurementLog(entries); err != nil {
		return nil, err
	}
	value := make([]byte, newHash().Size())
	for i := range entries {
		e := &entries[i]
		if e.PCR != pcr || e.Bank != bank {
			continue
		}
		h := newHash()
		h.Write(e.Measurement)
		digest := h.Sum(nil)
		h = newHash()
		h.Write(value)
		h.Write(digest)
		value = h.Sum(nil)
	}
	return value, nil
}
