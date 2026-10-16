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

// Package policyfields refuses SDK policy create and update requests that set
// structured fields a transport cannot carry to the server: rules (with their
// subjects and conditions), policy targets and policy conditions, and a
// policy-level effect or enabled value other than deny and true. A transport
// that cannot send a field must refuse the request rather than drop the
// field, because storing only part of a policy grants more than the policy
// says. Policy content sent as spec_yaml alone passes.
package policyfields

import (
	"encoding/json"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Refuse returns an INVALID_INPUT error naming the first structured policy
// field set in request, or nil when request carries none. op names the
// calling operation and transport the transport that cannot carry the field.
// request is the policy request body as a map, in the SDK's JSON field names.
func Refuse(op, transport string, request map[string]interface{}) error {
	fields, err := normalize(request)
	if err != nil {
		return errors.Wrap(err, errors.CodeInvalidInput, op, "policy request is not JSON-encodable")
	}
	field := structuredField(fields)
	if field == "" {
		return nil
	}
	if field == "effect" || field == "enabled" {
		return errors.NewInvalidInput(op, field+" cannot be sent over "+transport+
			"; stored policies are enabled and deny by default")
	}
	return errors.NewInvalidInput(op, field+" cannot be sent over "+transport+
		"; describe the policy in spec_yaml instead")
}

// normalize round-trips request through JSON so typed SDK values held in the
// map (for example []iam.Rule) are read as generic slices and maps.
func normalize(request map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// structuredField returns the most specific path of the first structured
// field set in fields, or "" when none is set.
func structuredField(fields map[string]interface{}) string {
	if field := conditionsField("conditions", fields["conditions"]); field != "" {
		return field
	}
	if len(list(fields["targets"])) > 0 {
		return "targets"
	}
	rules := list(fields["rules"])
	for i, raw := range rules {
		rule, _ := raw.(map[string]interface{})
		path := "rules[" + strconv.Itoa(i) + "]"
		if len(list(rule["subjects"])) > 0 {
			return path + ".subjects"
		}
		if field := conditionsField(path+".conditions", rule["conditions"]); field != "" {
			return field
		}
	}
	if len(rules) > 0 {
		return "rules"
	}
	return policyLevelField(fields)
}

// policyLevelField names a policy-level effect or enabled value the server
// cannot store. Every stored policy is enabled and denies by default, so only
// EffectDeny (the zero value, omitted from JSON) and enabled=true match what
// is stored. A create's Enabled is a plain bool with omitempty, so its zero
// value never reaches the map and cannot be told from unset.
func policyLevelField(fields map[string]interface{}) string {
	if effect, ok := fields["effect"]; ok && effect != float64(0) {
		return "effect"
	}
	if enabled, ok := fields["enabled"]; ok && enabled != true {
		return "enabled"
	}
	return ""
}

// conditionsField returns path when the condition list is set, or the path
// of the first condition's sub_conditions when one nests further.
func conditionsField(path string, raw interface{}) string {
	conditions := list(raw)
	for i, c := range conditions {
		condition, _ := c.(map[string]interface{})
		if len(list(condition["sub_conditions"])) > 0 {
			return path + "[" + strconv.Itoa(i) + "].sub_conditions"
		}
	}
	if len(conditions) > 0 {
		return path
	}
	return ""
}

// list returns raw as a JSON array, or nil when it is not one.
func list(raw interface{}) []interface{} {
	items, _ := raw.([]interface{})
	return items
}
