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

package iam

import (
	"strconv"

	iamcore "github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TenantFieldNotStoredError is returned by TenantClient.Update, before any
// transport call, when the update sets Description, Metadata or Status:
// go-quicbaas stores none of them until go-qrdb's tenant seam row lands
// (#1518). Every server refuses the same fields with the same error.
type TenantFieldNotStoredError = iamcore.TenantFieldNotStoredError

// ErrTenantFieldNotStored is the coded sentinel TenantFieldNotStoredError
// unwraps to; match it with errors.Is.
var ErrTenantFieldNotStored = iamcore.ErrTenantFieldNotStored

// UnknownEnumValueError is returned by an enum UnmarshalJSON when a string
// value is not one of the enum's recognized names.
type UnknownEnumValueError struct {
	// Enum is the human-readable enum name (e.g. "consent status").
	Enum string
	// Value is the unrecognized string value.
	Value string
}

// Error implements the error interface.
func (e *UnknownEnumValueError) Error() string {
	return "unknown " + e.Enum + ": " + e.Value
}

// JSONUnmarshalTypeError is returned by an UnmarshalJSON implementation when
// the raw JSON data cannot be decoded into the target type at all.
type JSONUnmarshalTypeError struct {
	// Target is the Go type name the data was being decoded into.
	Target string
	// Data is the raw JSON that failed to decode.
	Data string
}

// Error implements the error interface.
func (e *JSONUnmarshalTypeError) Error() string {
	return "cannot unmarshal " + e.Data + " into " + e.Target
}

// IntParseError is returned by FlexInt.UnmarshalJSON when a JSON string value
// cannot be parsed as an integer.
type IntParseError struct {
	// Value is the string that failed to parse.
	Value string
}

// Error implements the error interface.
func (e *IntParseError) Error() string {
	return "cannot parse " + strconv.Quote(e.Value) + " as integer"
}

// UserSuspensionUnsupportedError is returned by UserClient.Suspend and
// UserClient.Unsuspend, after input validation and before any transport
// call. User suspension is go-qrdb IAM state, served once the go-qrdb
// suspension seam lands (go-qrdb#801); no transport serves it until then.
type UserSuspensionUnsupportedError struct {
	// Op is the client operation that was refused (e.g. "iam.user.Suspend").
	Op string
}

// Error implements the error interface.
func (e *UserSuspensionUnsupportedError) Error() string {
	return e.Op + ": user suspension is not supported until the go-qrdb suspension seam lands"
}
