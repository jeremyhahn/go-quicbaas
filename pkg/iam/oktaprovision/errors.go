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

// Package oktaprovision is a self-contained Okta Management-API client that
// turnkey-onboards a tenant's Okta org: it creates/updates the OAuth app,
// creates role groups, assigns the groups to the app, and configures the groups
// scope/claim/access-policy on the default Custom Authorization Server.
//
// It is an OPTIONAL tenant-onboarding helper and is NOT on the federated-login
// hot path. App label, role-group names, and the groups-claim filter are
// configurable per tenant (no product hardcoding).
package oktaprovision

import (
	"errors"
	"fmt"
	"regexp"
)

// oktaOrgNameRE validates an Okta org-name subdomain label (defence-in-depth
// against SSRF via a crafted org name before it is interpolated into a URL).
//
//   - lowercase alphanumeric and hyphens only; no dots, @, /, or spaces
//   - must start with an alphanumeric character
//   - 1-63 chars (RFC 1035 label bound)
var oktaOrgNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ErrInvalidOrgName indicates an Okta org name failed format validation.
var ErrInvalidOrgName = errors.New("invalid okta org name")

// InvalidOrgNameError is returned when an Okta org name contains characters
// outside the allowed subdomain-label alphabet.
type InvalidOrgNameError struct {
	OrgName string
}

func (e *InvalidOrgNameError) Error() string {
	return fmt.Sprintf("invalid okta org name %q: must match ^[a-z0-9][a-z0-9-]{0,62}$", e.OrgName)
}

func (e *InvalidOrgNameError) Is(target error) bool { return target == ErrInvalidOrgName }

// ValidateOrgName returns nil when orgName is a safe Okta subdomain label, or an
// *InvalidOrgNameError otherwise. It is called before any URL is constructed.
func ValidateOrgName(orgName string) error {
	if !oktaOrgNameRE.MatchString(orgName) {
		return &InvalidOrgNameError{OrgName: orgName}
	}
	return nil
}

// ErrProvisioning indicates that an Okta provisioning operation failed.
var ErrProvisioning = errors.New("okta provisioning failed")

// ProvisioningError represents an Okta provisioning failure with context.
type ProvisioningError struct {
	Operation string
	OrgName   string
	Err       error
}

func (e *ProvisioningError) Error() string {
	return fmt.Sprintf("okta provisioning %s failed for org %q: %v", e.Operation, e.OrgName, e.Err)
}

func (e *ProvisioningError) Unwrap() error { return e.Err }

func (e *ProvisioningError) Is(target error) bool { return target == ErrProvisioning }

// ErrInvalidConfig indicates the provisioner was given invalid configuration.
var ErrInvalidConfig = errors.New("invalid okta provisioner config")

// InvalidConfigError describes a configuration validation failure.
type InvalidConfigError struct {
	Reason string
}

func (e *InvalidConfigError) Error() string {
	return "invalid okta provisioner config: " + e.Reason
}

func (e *InvalidConfigError) Is(target error) bool { return target == ErrInvalidConfig }

// RequestError wraps a low-level HTTP request construction or marshalling
// failure in the Okta Management-API client.
type RequestError struct {
	Op    string
	Cause error
}

func (e *RequestError) Error() string { return e.Op + ": " + e.Cause.Error() }

func (e *RequestError) Unwrap() error { return e.Cause }

// HTTPError represents a non-2xx response from the Okta Management API. ReadErr
// is set when the response body itself could not be read; otherwise Detail
// carries the server-supplied error summary or raw body.
type HTTPError struct {
	StatusCode int
	Detail     string
	ReadErr    error
}

func (e *HTTPError) Error() string {
	if e.ReadErr != nil {
		return fmt.Sprintf("HTTP %d (failed to read body: %v)", e.StatusCode, e.ReadErr)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Detail)
}

func (e *HTTPError) Unwrap() error { return e.ReadErr }
