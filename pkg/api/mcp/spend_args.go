//go:build mcp

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

package mcp

import (
	"math"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Spend limits as tool arguments (go-quicbaas#1058, ruling F5)
// =============================================================================
//
// One limits shape is carried by an account, a project and a tenant; this is
// its one schema and its one parser, so the three tools cannot drift.

// spendLimitsSchema describes a spend-limits argument.
func spendLimitsSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"properties": map[string]interface{}{
			"windows": map[string]interface{}{
				"type": "array",
				"description": "Sliding windows. Each meters tokens, list-price dollars " +
					"(max_equivalent_micros, millionths of a US dollar) or both.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":                  map[string]interface{}{"type": "string"},
						"size":                  map[string]interface{}{"description": "Window length: a Go duration (\"5h\", \"168h\") or nanoseconds"},
						"max_tokens":            map[string]interface{}{"type": "integer", "minimum": 0},
						"max_equivalent_micros": map[string]interface{}{"type": "integer", "minimum": 0},
					},
					"required": []string{"name", "size"},
				},
			},
		},
	}
}

// SpendLimitsArgumentError is a spend-limits argument that cannot be read.
type SpendLimitsArgumentError struct {
	// Key is the tool argument ("spend", "limits", "spend.breaker").
	Key string

	// Index is the offending window, or -1 when the error is not about one.
	Index int

	// Field names the offending member ("max_tokens", "failure_threshold"),
	// or is empty when the error is about the argument's shape.
	Field string

	Message string
}

// Error implements the error interface.
func (e *SpendLimitsArgumentError) Error() string {
	where := e.Key
	if e.Index >= 0 {
		where += ".windows[" + strconv.Itoa(e.Index) + "]"
	}
	if e.Field != "" {
		where += "." + e.Field
	}
	return "mcp: invalid " + where + ": " + e.Message
}

// spendCountMessage is why a count or cap argument was refused.
const spendCountMessage = "must be a whole number, zero or more"

// spendCount reads a non-negative whole-number member: a token cap, a dollar
// cap, a failure threshold. An absent member is zero, which means "no cap".
// Anything else that is not a whole number — a string, a fraction, a value
// past int64 — is refused (ok=false) rather than read as zero: on an update, a
// zero replaces, and so silently removes, the cap the caller meant to keep
// (go-quicbaas#1058 review).
func spendCount(fields map[string]interface{}, name string) (value int64, ok bool) {
	raw, present := fields[name]
	if !present || raw == nil {
		return 0, true
	}
	n, ok := wholeNumber(raw)
	if !ok || n < 0 {
		return 0, false
	}
	return n, true
}

// getSpendLimits reads a spend-limits argument. present is false when the
// caller did not send one, which on an update leaves the limits unchanged.
func getSpendLimits(args map[string]interface{}, key string) (limits orchestration.SpendLimits, present bool, err error) {
	raw, present := args[key]
	if !present || raw == nil {
		return orchestration.SpendLimits{}, false, nil
	}
	fields, ok := raw.(map[string]interface{})
	if !ok {
		return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: -1, Message: "must be an object"}
	}
	var rawWindows []interface{}
	if raw, has := fields["windows"]; has && raw != nil {
		list, ok := raw.([]interface{})
		if !ok {
			return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: -1, Message: "windows must be an array"}
		}
		rawWindows = list
	}
	for i, item := range rawWindows {
		window, ok := item.(map[string]interface{})
		if !ok {
			return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: i, Message: "must be an object"}
		}
		size, err := spendWindowSize(window["size"])
		if err != nil {
			return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: i, Message: err.Error()}
		}
		maxTokens, ok := spendCount(window, "max_tokens")
		if !ok {
			return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: i, Field: "max_tokens", Message: spendCountMessage}
		}
		maxMicros, ok := spendCount(window, "max_equivalent_micros")
		if !ok {
			return orchestration.SpendLimits{}, true, &SpendLimitsArgumentError{Key: key, Index: i, Field: "max_equivalent_micros", Message: spendCountMessage}
		}
		limits.Windows = append(limits.Windows, orchestration.QuotaWindow{
			Name:                getStringFromMap(window, "name"),
			Size:                size,
			MaxTokens:           maxTokens,
			MaxEquivalentMicros: maxMicros,
		})
	}
	return limits, true, nil
}

// optionalSpendLimits reads an update's spend limits as the service's "nil
// means unchanged" pointer.
func optionalSpendLimits(args map[string]interface{}, key string) (*orchestration.SpendLimits, error) {
	limits, present, err := getSpendLimits(args, key)
	if err != nil || !present {
		return nil, err
	}
	return &limits, nil
}

// spendWindowSizeError is a window size that is neither a duration nor a
// number of nanoseconds.
type spendWindowSizeError struct{ value string }

func (e *spendWindowSizeError) Error() string {
	return "size " + strconv.Quote(e.value) + " is neither a Go duration nor nanoseconds"
}

// spendWindowSize reads a window length given as a Go duration string or as
// nanoseconds (the form the tools return it in).
func spendWindowSize(raw interface{}) (time.Duration, error) {
	switch v := raw.(type) {
	case float64:
		n, ok := wholeNumber(v)
		if !ok {
			return 0, &spendWindowSizeError{value: strconv.FormatFloat(v, 'g', -1, 64)}
		}
		return time.Duration(n), nil
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d, nil
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Duration(n), nil
		}
		return 0, &spendWindowSizeError{value: v}
	default:
		return 0, &spendWindowSizeError{value: ""}
	}
}

// accountSpendSchema describes an account's spend-policy argument.
func accountSpendSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
		"properties": map[string]interface{}{
			"quota": spendLimitsSchema("The account's sliding windows"),
			"breaker": map[string]interface{}{
				"type":        "object",
				"description": "Take the account out of service after failure_threshold failures within window, for cooldown",
				"properties": map[string]interface{}{
					"window":            map[string]interface{}{"description": "A Go duration or nanoseconds"},
					"failure_threshold": map[string]interface{}{"type": "integer", "minimum": 1},
					"cooldown":          map[string]interface{}{"description": "A Go duration or nanoseconds"},
				},
			},
		},
	}
}

// getAccountSpendPolicy reads an account's spend-policy argument. present is
// false when the caller did not send one.
func getAccountSpendPolicy(args map[string]interface{}, key string) (policy orchestration.AccountSpendPolicy, present bool, err error) {
	raw, present := args[key]
	if !present || raw == nil {
		return orchestration.AccountSpendPolicy{}, false, nil
	}
	fields, ok := raw.(map[string]interface{})
	if !ok {
		return orchestration.AccountSpendPolicy{}, true, &SpendLimitsArgumentError{Key: key, Index: -1, Message: "must be an object"}
	}
	policy.Quota, _, err = getSpendLimits(fields, "quota")
	if err != nil {
		return orchestration.AccountSpendPolicy{}, true, err
	}
	if breakerRaw, has := fields["breaker"]; has && breakerRaw != nil {
		breaker, ok := breakerRaw.(map[string]interface{})
		if !ok {
			return orchestration.AccountSpendPolicy{}, true, &SpendLimitsArgumentError{Key: key + ".breaker", Index: -1, Message: "must be an object"}
		}
		window, err := spendWindowSize(breaker["window"])
		if err != nil {
			return orchestration.AccountSpendPolicy{}, true, &SpendLimitsArgumentError{Key: key + ".breaker.window", Index: -1, Message: err.Error()}
		}
		cooldown, err := spendWindowSize(breaker["cooldown"])
		if err != nil {
			return orchestration.AccountSpendPolicy{}, true, &SpendLimitsArgumentError{Key: key + ".breaker.cooldown", Index: -1, Message: err.Error()}
		}
		threshold, ok := spendCount(breaker, "failure_threshold")
		if !ok || threshold > math.MaxInt32 {
			return orchestration.AccountSpendPolicy{}, true, &SpendLimitsArgumentError{Key: key + ".breaker", Index: -1, Field: "failure_threshold", Message: spendCountMessage}
		}
		policy.Breaker = &orchestration.BreakerPolicy{
			Window:           window,
			FailureThreshold: int(threshold),
			Cooldown:         cooldown,
		}
	}
	return policy, true, nil
}

// optionalAccountSpendPolicy reads an update's spend policy as the service's
// "nil means unchanged" pointer.
func optionalAccountSpendPolicy(args map[string]interface{}, key string) (*orchestration.AccountSpendPolicy, error) {
	policy, present, err := getAccountSpendPolicy(args, key)
	if err != nil || !present {
		return nil, err
	}
	return &policy, nil
}
