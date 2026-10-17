package iam

import (
	"encoding/json"
	"fmt"
)

const (
	// policyVersionKey is the request key a caller sends when it expects
	// optimistic concurrency on a policy.
	policyVersionKey = "version"

	// policyObjectKey is the request key that wraps a policy document.
	policyObjectKey = "policy"
)

// RefusePolicyVersion returns a *PolicyVersionUnsupportedError when value,
// a decoded "version" field, is present and not zero. nil (absent or JSON
// null) and the number zero pass; anything else, including a string, is
// refused.
func RefusePolicyVersion(value interface{}) error {
	if isZeroPolicyVersion(value) {
		return nil
	}
	return &PolicyVersionUnsupportedError{Version: fmt.Sprint(value)}
}

// RefusePolicyVersionIn checks a decoded request document for a non-zero
// version at the top level and inside a nested "policy" object.
func RefusePolicyVersionIn(doc map[string]interface{}) error {
	if err := RefusePolicyVersion(doc[policyVersionKey]); err != nil {
		return err
	}
	nested, ok := doc[policyObjectKey].(map[string]interface{})
	if !ok {
		return nil
	}
	return RefusePolicyVersion(nested[policyVersionKey])
}

// isZeroPolicyVersion reports whether value is absent or the number zero.
func isZeroPolicyVersion(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case float64:
		return v == 0
	case int:
		return v == 0
	case int64:
		return v == 0
	case json.Number:
		f, err := v.Float64()
		return err == nil && f == 0
	default:
		return false
	}
}
