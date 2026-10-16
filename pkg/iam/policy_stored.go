package iam

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// jsonUnmarshalerType is the interface a type implements to decode itself;
// such a type (time.Time) is a leaf for unknown-key detection.
var jsonUnmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

// decodeStoredPolicy decodes a stored policy record strictly: a key no
// policy field takes, trailing data, or a record that is not a policy
// document is refused with a StoredPolicyDecodeError naming the record.
func decodeStoredPolicy(tenantID, name string, data []byte) (*Policy, *StoredPolicyDecodeError) {
	var policy Policy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&policy)
	if err == nil {
		if _, tokenErr := decoder.Token(); !errors.Is(tokenErr, io.EOF) {
			err = &IAMMessageError{Message: "trailing data after the policy document"}
		}
	}
	if err == nil {
		return &policy, nil
	}
	return nil, &StoredPolicyDecodeError{
		TenantID:    tenantID,
		Name:        name,
		UnknownKeys: unknownJSONKeys(data, reflect.TypeOf(policy)),
		Reason:      err.Error(),
		record:      bytes.Clone(data),
	}
}

// flaggedPolicy is the entry ListPolicies returns in place of a record that
// does not decode strictly: its name and tenant, no rules, and the error.
func flaggedPolicy(loadErr *StoredPolicyDecodeError) *Policy {
	return &Policy{
		APIVersion: PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: loadErr.Name, Tenant: loadErr.TenantID},
		LoadError:  loadErr,
	}
}

// encodeStoredPolicy returns the bytes to store for policy. A flagged
// placeholder carrying its record's original bytes is written back as those
// bytes, so restoring a flagged record leaves it byte-identical and flagged.
func encodeStoredPolicy(policy *Policy) ([]byte, error) {
	if policy.LoadError != nil && policy.LoadError.record != nil {
		return policy.LoadError.record, nil
	}
	return json.Marshal(policy)
}

// warnPolicyNotApplied tells the operator a stored policy record was refused.
func warnPolicyNotApplied(l storageLogger, loadErr *StoredPolicyDecodeError) {
	if l == nil {
		return
	}
	l.Warnf("stored policy not applied: tenant=%q name=%q unknown_keys=%q reason=%q",
		loadErr.TenantID, loadErr.Name, strings.Join(loadErr.UnknownKeys, ","), loadErr.Reason)
}

// unknownJSONKeys lists, as sorted dotted paths, every object key in data
// that no field of t takes. Keys match fields case-insensitively, as
// encoding/json matches them. It returns nil when data is not JSON.
func unknownJSONKeys(data []byte, t reflect.Type) []string {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	var keys []string
	collectUnknownKeys(raw, t, "", &keys)
	sort.Strings(keys)
	return keys
}

// collectUnknownKeys walks value against t, appending the path of each key
// t has no field for.
func collectUnknownKeys(value interface{}, t reflect.Type, path string, keys *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch v := value.(type) {
	case map[string]interface{}:
		if t.Kind() == reflect.Map {
			for k, elem := range v {
				collectUnknownKeys(elem, t.Elem(), joinKeyPath(path, k), keys)
			}
			return
		}
		if t.Kind() != reflect.Struct || reflect.PointerTo(t).Implements(jsonUnmarshalerType) {
			return
		}
		for k, elem := range v {
			field, ok := jsonFieldByKey(t, k)
			if !ok {
				*keys = append(*keys, joinKeyPath(path, k))
				continue
			}
			collectUnknownKeys(elem, field.Type, joinKeyPath(path, k), keys)
		}
	case []interface{}:
		if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
			return
		}
		for i, elem := range v {
			collectUnknownKeys(elem, t.Elem(), path+"["+strconv.Itoa(i)+"]", keys)
		}
	}
}

// jsonFieldByKey finds the exported field of struct type t that encoding/json
// decodes key into.
func jsonFieldByKey(t reflect.Type, key string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if strings.EqualFold(name, key) {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// joinKeyPath appends key to a dotted path.
func joinKeyPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
