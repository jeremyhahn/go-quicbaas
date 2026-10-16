// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

// PolicyYAMLAPIVersion is the apiVersion given to a policy built from a
// spec-only YAML document.
const PolicyYAMLAPIVersion = "iam.quicbaas.io/v1"

// PolicyYAMLError reports a policy YAML document that does not parse.
type PolicyYAMLError struct {
	Err error
}

// Error implements the error interface.
func (e *PolicyYAMLError) Error() string {
	return "invalid policy yaml: " + e.Err.Error()
}

// Unwrap returns the YAML decoder's error.
func (e *PolicyYAMLError) Unwrap() error {
	return e.Err
}

// ParsePolicyYAML builds a policy from the spec_yaml a client sends with a
// create request. The document is either a full policy (apiVersion, kind,
// metadata, spec) or a spec only. For a full policy, the non-empty fields of
// meta override the document's metadata; for a spec only, meta is the
// metadata. The tenant always comes from meta, never from the document.
//
// A document that does not parse, or that sets a key the policy schema does
// not define, returns an ErrPolicyInvalid-coded error wrapping
// *PolicyYAMLError.
func ParsePolicyYAML(data []byte, meta PolicyMetadata) (*Policy, error) {
	var header struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, newPolicyYAMLError(err)
	}
	if header.APIVersion != "" && header.Kind != "" {
		var full Policy
		if err := decodePolicyYAML(data, &full); err != nil {
			return nil, err
		}
		overridePolicyMetadata(&full.Metadata, meta)
		return &full, nil
	}

	var spec PolicySpec
	if err := decodePolicyYAML(data, &spec); err != nil {
		return nil, err
	}
	return &Policy{
		APIVersion: PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata:   meta,
		Spec:       spec,
	}, nil
}

// decodePolicyYAML decodes data into out and refuses any key out does not
// define. A dropped key would store a policy broader than the document, so
// an unknown key is an error, not a no-op. An empty document decodes to the
// zero value.
func decodePolicyYAML(data []byte, out interface{}) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return newPolicyYAMLError(err)
	}
	return nil
}

func newPolicyYAMLError(err error) error {
	return NewIAMError(ErrPolicyInvalid, "iam.policy.yaml", "invalid spec_yaml",
		&PolicyYAMLError{Err: err})
}

// overridePolicyMetadata copies the non-empty request fields of meta over
// dst, and always copies the tenant.
func overridePolicyMetadata(dst *PolicyMetadata, meta PolicyMetadata) {
	dst.Tenant = meta.Tenant
	if meta.Name != "" {
		dst.Name = meta.Name
	}
	if meta.Description != "" {
		dst.Description = meta.Description
	}
	if len(meta.Labels) > 0 {
		dst.Labels = meta.Labels
	}
	if meta.Priority > 0 {
		dst.Priority = meta.Priority
	}
}
