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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReading_ZeroValueIsUnread(t *testing.T) {
	t.Parallel()
	var r Reading
	_, ok := r.Value()
	assert.False(t, ok, "a reading nobody constructed must not claim a value")
	assert.False(t, r.Known())
	assert.Equal(t, ProvenanceUnknown, r.Provenance())
	assert.Equal(t, "unread", r.String())
	assert.Equal(t, Unread(), r)
}

func TestReading_MeasuredAndDerivedCarryProvenance(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		reading Reading
		want    Provenance
	}{
		"measured": {Measured(0), ProvenanceMeasured},
		"derived":  {Derived(0.5), ProvenanceDerived},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := tc.reading.Value()
			assert.True(t, ok, "a zero measurement is still a measurement")
			assert.Equal(t, tc.want, tc.reading.Provenance())
		})
	}
	assert.Equal(t, "0.5 (derived)", Derived(0.5).String())
}

func TestReading_NonFiniteIsNotAMeasurement(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		assert.False(t, Measured(v).Known())
		assert.False(t, Derived(v).Known())
	}
}

func TestReading_JSONOmitsUnreadValue(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(Unread())
	require.NoError(t, err)
	assert.JSONEq(t, `{"provenance":"unknown"}`, string(raw))

	raw, err = json.Marshal(Measured(42.5))
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":42.5,"provenance":"measured"}`, string(raw))
}

func TestReading_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, r := range []Reading{Measured(1.25), Derived(0.75), Unread()} {
		raw, err := json.Marshal(r)
		require.NoError(t, err)
		var back Reading
		require.NoError(t, json.Unmarshal(raw, &back))
		assert.Equal(t, r, back)
	}
}

func TestReading_JSONDisagreementDecodesAsUnread(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"provenance without a value": `{"provenance":"measured"}`,
		"estimated is not a reading": `{"value":3,"provenance":"estimated"}`,
		"unknown provenance":         `{"value":3,"provenance":"guessed"}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var r Reading
			require.NoError(t, json.Unmarshal([]byte(payload), &r))
			assert.False(t, r.Known())
		})
	}
}

func TestReading_JSONRejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	var r Reading
	assert.Error(t, json.Unmarshal([]byte(`{"value":"high"}`), &r))
}

func TestProvenance_TextRoundTripAndKnown(t *testing.T) {
	t.Parallel()
	for _, p := range []Provenance{ProvenanceUnknown, ProvenanceMeasured, ProvenanceDerived, ProvenanceEstimated} {
		text, err := p.MarshalText()
		require.NoError(t, err)
		var back Provenance
		require.NoError(t, back.UnmarshalText(text))
		assert.Equal(t, p, back)
	}
	assert.True(t, ProvenanceEstimated.Known())
	assert.False(t, ProvenanceEstimated.FromTelemetry())
	assert.True(t, ProvenanceDerived.FromTelemetry())
	assert.False(t, ProvenanceUnknown.Known())
	assert.False(t, Provenance(200).Known(), "an out-of-range provenance is not one")
	assert.Equal(t, "unknown", Provenance(200).String())

	var p Provenance
	require.NoError(t, p.UnmarshalText([]byte("nonsense")))
	assert.Equal(t, ProvenanceUnknown, p)
}
