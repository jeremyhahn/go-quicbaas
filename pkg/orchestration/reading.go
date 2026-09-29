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
	"encoding/json"
	"math"
	"strconv"
)

// =============================================================================
// Readings — a value that carries how it came to be known
// =============================================================================
//
// A metric is not a number. It is a number TOGETHER WITH the fact that someone
// read it, and dropping the second half is how an unmeasured signal becomes a
// measured zero. A GPU drawing exactly 0 W is impossible, but a utilisation of
// exactly 0.0 is a perfectly legitimate reading of an idle device, and no shape
// check separates it from a collector that failed to read the device and left
// the field alone. So the value and its provenance are ONE type, set together
// at the moment of reading.
//
// THE ZERO VALUE IS THE POINT. Reading's fields are unexported and its zero
// provenance is ProvenanceUnknown, so a Reading nobody constructed is a Reading
// nobody read — and it says so. A struct literal cannot fake a measurement,
// because there is no exported field to set.
//
// Ported from the WP11 fleet admission work (commit 7fd20bf8), where it was
// built for host health; the fleet's device telemetry
// and the ledger's energy figures are the same problem.

// Provenance says how a value came to be known.
type Provenance uint8

const (
	// ProvenanceUnknown means the value was never read. It is the zero value
	// deliberately: anything nobody constructed reads as unknown, which fails
	// closed by default rather than by remembering to check.
	ProvenanceUnknown Provenance = iota

	// ProvenanceMeasured means the value was read directly from a sensor —
	// a GPU's power draw as the driver reports it.
	ProvenanceMeasured

	// ProvenanceDerived means the value was computed from measured values
	// rather than read: a power figure from two readings of an energy
	// counter, a busy fraction from two readings of a busy-time counter, or
	// an energy figure that scales a configured draw by a measured busy time.
	// A distinct provenance because a derived value has limits its sources do
	// not, and a consumer that cannot see the difference cannot allow for
	// them.
	ProvenanceDerived

	// ProvenanceEstimated means the value was computed from a configured
	// figure alone — a declared draw times a wall-clock duration — with
	// nothing on the device read at all. It is what a ledger entry's energy
	// is when no telemetry covered the turn.
	ProvenanceEstimated
)

// provenanceNames is the wire spelling of each provenance. A map rather than
// a switch so the decoder below reads the same table the encoder writes.
var provenanceNames = map[Provenance]string{
	ProvenanceUnknown:   "unknown",
	ProvenanceMeasured:  "measured",
	ProvenanceDerived:   "derived",
	ProvenanceEstimated: "estimated",
}

// provenanceByName inverts provenanceNames.
var provenanceByName = func() map[string]Provenance {
	byName := make(map[string]Provenance, len(provenanceNames))
	for p, name := range provenanceNames {
		byName[name] = p
	}
	return byName
}()

// String implements fmt.Stringer.
func (p Provenance) String() string {
	if name, ok := provenanceNames[p]; ok {
		return name
	}
	return provenanceNames[ProvenanceUnknown]
}

// Known reports whether the provenance describes a value that was established.
func (p Provenance) Known() bool {
	_, named := provenanceNames[p]
	return named && p != ProvenanceUnknown
}

// FromTelemetry reports whether the value came from reading a device, whether
// directly or by derivation — as opposed to being estimated from
// configuration or never established.
func (p Provenance) FromTelemetry() bool {
	return p == ProvenanceMeasured || p == ProvenanceDerived
}

// MarshalText implements encoding.TextMarshaler, so a Provenance travels as
// its name rather than as a number whose meaning lives in this file.
func (p Provenance) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. A name this package does
// not know decodes as unknown: a provenance nobody can vouch for is not one.
func (p *Provenance) UnmarshalText(text []byte) error {
	*p = provenanceByName[string(text)]
	return nil
}

// Reading is one value and the fact of its being read.
//
// Fields are unexported so a Reading can only be built by Measured or Derived.
// Use Value to consume one; it returns the number and whether there is a
// number at all.
type Reading struct {
	value      float64
	provenance Provenance
}

// Measured returns a Reading read directly from a sensor.
//
// A non-finite value is NOT a measurement — NaN is what a failed calculation
// returns, and it compares false against every threshold — so it is recorded
// as unread, which is what it is.
func Measured(value float64) Reading {
	if !isReal(value) {
		return Reading{}
	}
	return Reading{value: value, provenance: ProvenanceMeasured}
}

// Derived returns a Reading computed from other measured values.
func Derived(value float64) Reading {
	if !isReal(value) {
		return Reading{}
	}
	return Reading{value: value, provenance: ProvenanceDerived}
}

// Unread returns a Reading for a signal that could not be read. Explicit
// rather than relying on the zero value, so a collector can SAY it failed at
// the site where it failed.
func Unread() Reading { return Reading{} }

// Value returns the number and whether one was established. Two results, so
// a caller cannot use the number without acknowledging the question.
func (r Reading) Value() (float64, bool) { return r.value, r.provenance.FromTelemetry() }

// Provenance reports how the value came to be known.
func (r Reading) Provenance() Provenance { return r.provenance }

// Known reports whether the value was established at all.
func (r Reading) Known() bool { return r.provenance.FromTelemetry() }

// String renders the reading with its provenance, so a log line cannot show a
// number without showing what kind of number it is.
func (r Reading) String() string {
	if !r.Known() {
		return "unread"
	}
	return strconv.FormatFloat(r.value, 'f', -1, 64) + " (" + r.provenance.String() + ")"
}

// readingJSON is the wire form of a Reading.
type readingJSON struct {
	Value      *float64 `json:"value,omitempty"`
	Provenance string   `json:"provenance"`
}

// MarshalJSON implements json.Marshaler.
//
// The value is OMITTED when unknown rather than written as 0: a wire format
// that serialised an unread signal as zero would hand the ambiguity to every
// consumer that did not also read the provenance field.
func (r Reading) MarshalJSON() ([]byte, error) {
	out := readingJSON{Provenance: provenanceNames[ProvenanceUnknown]}
	if r.Known() {
		v := r.value
		out.Value = &v
		out.Provenance = r.provenance.String()
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler.
//
// A payload claiming a provenance but carrying no value decodes as UNREAD, not
// as zero, and so does one claiming a provenance a reading cannot have. The two
// halves must agree, and when they do not the safe reading is the one that
// admits it knows nothing.
func (r *Reading) UnmarshalJSON(data []byte) error {
	var in readingJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*r = Reading{}
	if in.Value == nil {
		return nil
	}
	switch provenanceByName[in.Provenance] {
	case ProvenanceMeasured:
		*r = Measured(*in.Value)
	case ProvenanceDerived:
		*r = Derived(*in.Value)
	}
	return nil
}

// isReal reports whether a float is an actual number rather than NaN or an
// infinity. NaN compares false against every bound, so a range check written
// as `< 0 || > 1` waves it through.
func isReal(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
