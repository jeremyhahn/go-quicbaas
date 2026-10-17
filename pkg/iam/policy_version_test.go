package iam

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefusePolicyVersion_ZeroOrAbsentPasses(t *testing.T) {
	for name, value := range map[string]interface{}{
		"absent":         nil,
		"float zero":     float64(0),
		"int zero":       0,
		"int64 zero":     int64(0),
		"json zero":      json.Number("0"),
		"json zero frac": json.Number("0.0"),
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, RefusePolicyVersion(value))
		})
	}
}

func TestRefusePolicyVersion_NonZeroRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		value interface{}
		text  string
	}{
		"float":       {value: float64(3), text: "3"},
		"int":         {value: 2, text: "2"},
		"int64":       {value: int64(-1), text: "-1"},
		"json number": {value: json.Number("7"), text: "7"},
		"bad number":  {value: json.Number("x"), text: "x"},
		"string":      {value: "1", text: "1"},
		"bool":        {value: true, text: "true"},
	} {
		t.Run(name, func(t *testing.T) {
			err := RefusePolicyVersion(tc.value)
			var versionErr *PolicyVersionUnsupportedError
			require.True(t, errors.As(err, &versionErr), "got %v", err)
			assert.Equal(t, tc.text, versionErr.Version)
			assert.Contains(t, err.Error(), "policy version is not supported: got "+tc.text)
		})
	}
}

func TestRefusePolicyVersionIn(t *testing.T) {
	for name, tc := range map[string]struct {
		doc     map[string]interface{}
		refused bool
	}{
		"no version":           {doc: map[string]interface{}{"name": "p1"}},
		"zero at top level":    {doc: map[string]interface{}{"version": float64(0)}},
		"zero inside policy":   {doc: map[string]interface{}{"policy": map[string]interface{}{"version": float64(0)}}},
		"policy not an object": {doc: map[string]interface{}{"policy": "p1"}},
		"top level":            {doc: map[string]interface{}{"version": float64(4)}, refused: true},
		"inside policy":        {doc: map[string]interface{}{"policy": map[string]interface{}{"version": float64(4)}}, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := RefusePolicyVersionIn(tc.doc)
			if !tc.refused {
				assert.NoError(t, err)
				return
			}
			var versionErr *PolicyVersionUnsupportedError
			require.True(t, errors.As(err, &versionErr), "got %v", err)
			assert.Equal(t, "4", versionErr.Version)
		})
	}
}
