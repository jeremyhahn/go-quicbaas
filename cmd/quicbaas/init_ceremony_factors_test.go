package main

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Each --factor spec becomes one enrolment, in order: a bare name carries no
// params, and name=<json object> carries the decoded object.
func TestParseHolderFactors_NamesAndParams(t *testing.T) {
	factors, err := parseHolderFactors([]string{"passphrase", `fido2={"rp_id":"example.test"}`})

	require.NoError(t, err)
	require.Len(t, factors, 2)
	assert.Equal(t, "passphrase", factors[0].Name)
	assert.Empty(t, factors[0].Params)
	assert.Equal(t, "fido2", factors[1].Name)
	params, err := json.Marshal(factors[1].Params)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rp_id":"example.test"}`, string(params))
}

func TestParseHolderFactors_NoSpecsIsEmpty(t *testing.T) {
	factors, err := parseHolderFactors(nil)

	require.NoError(t, err)
	assert.Empty(t, factors)
}

// A spec without a factor name, or whose params are not a JSON object, is an
// invalid-input error and no partial list is returned.
func TestParseHolderFactors_RejectsBadSpecs(t *testing.T) {
	for name, spec := range map[string]string{
		"empty spec":        "",
		"no factor name":    `={"a":1}`,
		"params not json":   "fido2=not-json",
		"params not object": `fido2=[1,2]`,
	} {
		t.Run(name, func(t *testing.T) {
			factors, err := parseHolderFactors([]string{"passphrase", spec})

			require.Error(t, err)
			var typed *dberrors.QBError
			require.True(t, errors.As(err, &typed), "got %T: %v", err, err)
			assert.Equal(t, dberrors.ErrInvalidInput, typed.Code)
			assert.Nil(t, factors)
		})
	}
}
