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

	// AllowUnsigned names the allow-unsigned grant that admitted an
	// unsigned image; empty for a signed image. It is omitted when empty,
	// so a signed launch's digest does not depend on the field existing.
	AllowUnsigned string `json:"allow_unsigned,omitempty"`
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

	// Kind is MeasurementLogLaunch for a measured launch, or
	// MeasurementLogAnchor for the value a register held when the log first
	// used it. An anchor carries Base and no launch fields.
	Kind string `json:"kind"`

	// Base is the register's value at the anchor, before any logged extend.
	Base []byte `json:"base,omitempty"`

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

	// AllowUnsigned names the allow-unsigned grant that admitted an
	// unsigned image; empty for a signed launch. It is the audit record of
	// the grant, and PolicyDigest commits to it as well.
	AllowUnsigned string `json:"allow_unsigned,omitempty"`

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

// Measurement log entry kinds.
const (
	// MeasurementLogLaunch is a measured container launch.
	MeasurementLogLaunch = "launch"

	// MeasurementLogAnchor records the value a register held when the log
	// first extended it: a node agent that restarts finds the application
	// PCR already extended by launches its new log never saw. Replay starts
	// from the anchor; the reference values still judge the quoted value.
	MeasurementLogAnchor = "anchor"
)

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
		fmt.Sprintf("%d", e.Sequence), e.Kind, hex.EncodeToString(e.Base), e.Image, e.ImageDigest, e.ConfigDigest,
		e.PolicyDigest, e.TrustLabel, e.AllowUnsigned, fmt.Sprintf("%d", e.PCR), e.Bank,
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
// extends and a failed extend leaves no entry. The first time the log uses
// entry's register, readBase supplies the register's current value, which
// is appended as an anchor ahead of the launch.
func (l *MeasurementLog) record(entry MeasurementLogEntry, readBase func() ([]byte, error), extend func() error) (MeasurementLogEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var anchor *MeasurementLogEntry
	if !l.usesRegister(entry.PCR, entry.Bank) {
		base, err := readBase()
		if err != nil {
			return MeasurementLogEntry{}, err
		}
		anchor = &MeasurementLogEntry{
			Kind: MeasurementLogAnchor,
			Base: append([]byte(nil), base...),
			PCR:  entry.PCR,
			Bank: entry.Bank,
		}
	}
	if err := extend(); err != nil {
		return MeasurementLogEntry{}, err
	}
	if anchor != nil {
		l.appendLocked(*anchor)
	}
	entry.Kind = MeasurementLogLaunch
	entry.Measurement = append([]byte(nil), entry.Measurement...)
	return l.appendLocked(entry), nil
}

// usesRegister reports whether the log already holds an entry for a
// register. The caller holds l.mu.
func (l *MeasurementLog) usesRegister(pcr int, bank string) bool {
	for i := range l.entries {
		if l.entries[i].PCR == pcr && l.entries[i].Bank == bank {
			return true
		}
	}
	return false
}

// appendLocked numbers, timestamps and chains entry onto the log. The
// caller holds l.mu.
func (l *MeasurementLog) appendLocked(entry MeasurementLogEntry) MeasurementLogEntry {
	prev := make([]byte, sha256.Size)
	if n := len(l.entries); n > 0 {
		// Chains this log wrote are always hex.
		prev, _ = hex.DecodeString(l.entries[n-1].Chain)
	}
	entry.Sequence = uint64(len(l.entries))
	entry.RecordedAt = time.Now().UTC()
	entry.Chain = hex.EncodeToString(chainDigest(prev, &entry))
	l.entries = append(l.entries, entry)
	return entry
}

// Entries returns a copy of the log's entries in order.
func (l *MeasurementLog) Entries() []MeasurementLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]MeasurementLogEntry, len(l.entries))
	for i, e := range l.entries {
		e.Measurement = append([]byte(nil), e.Measurement...)
		e.Base = append([]byte(nil), e.Base...)
		out[i] = e
	}
	return out
}

// ChainMeasurementLog returns a copy of entries numbered from 0 and chained
// as a node's log chains them. It is for building a log outside a running
// node, such as a fixture or an import; it does not judge the entries.
func ChainMeasurementLog(entries []MeasurementLogEntry) []MeasurementLogEntry {
	out := make([]MeasurementLogEntry, len(entries))
	prev := make([]byte, sha256.Size)
	for i, e := range entries {
		e.Sequence = uint64(i)
		e.Measurement = append([]byte(nil), e.Measurement...)
		e.Base = append([]byte(nil), e.Base...)
		chain := chainDigest(prev, &e)
		e.Chain = hex.EncodeToString(chain)
		out[i] = e
		prev = chain
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
// pcr in bank holds after every entry for that register is applied in
// order. The register starts at zero, or at the Base of its anchor, which
// must precede every launch on it; each launch extends it as
// HostTPM.ExtendPCR does: PCR = H(PCR || H(measurement)).
func ReplayMeasurementLog(entries []MeasurementLogEntry, pcr int, bank string) ([]byte, error) {
	newHash, err := pcrBankHash(bank)
	if err != nil {
		return nil, err
	}
	if err := VerifyMeasurementLog(entries); err != nil {
		return nil, err
	}
	value := make([]byte, newHash().Size())
	started := false
	for i := range entries {
		e := &entries[i]
		if e.PCR != pcr || e.Bank != bank {
			continue
		}
		switch e.Kind {
		case MeasurementLogAnchor:
			if started {
				return nil, &MeasurementLogError{Sequence: e.Sequence, Reason: "anchor after the register was first used"}
			}
			if len(e.Base) != len(value) {
				return nil, &MeasurementLogError{Sequence: e.Sequence, Reason: "anchor base is not a " + bank + " digest"}
			}
			value = append([]byte(nil), e.Base...)
		case MeasurementLogLaunch:
			h := newHash()
			h.Write(e.Measurement)
			digest := h.Sum(nil)
			h = newHash()
			h.Write(value)
			h.Write(digest)
			value = h.Sum(nil)
		default:
			return nil, &MeasurementLogError{Sequence: e.Sequence, Reason: fmt.Sprintf("unknown entry kind %q", e.Kind)}
		}
		started = true
	}
	return value, nil
}
