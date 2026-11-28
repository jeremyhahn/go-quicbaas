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

package images

import "fmt"

// RegistryError is a non-2xx answer from the registry. Code and Message come
// from the first entry of the OCI error envelope; Code is "UNKNOWN" when the
// body is not an envelope.
type RegistryError struct {
	// Status is the HTTP status code.
	Status int
	// Code is the OCI error code, such as MANIFEST_UNKNOWN.
	Code string
	// Message is the registry's message for the error.
	Message string
}

// Error implements error.
func (e *RegistryError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("images: registry returned %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("images: registry returned %d %s: %s", e.Status, e.Code, e.Message)
}

// DigestMismatchError reports content whose digest is not the one asked for
// or announced.
type DigestMismatchError struct {
	// Want is the expected digest.
	Want string
	// Got is the digest of the content received.
	Got string
}

// Error implements error.
func (e *DigestMismatchError) Error() string {
	return fmt.Sprintf("images: digest mismatch: want %s, got %s", e.Want, e.Got)
}

// InvalidArgumentError reports an argument the client rejects before it
// sends any request.
type InvalidArgumentError struct {
	// Field names the argument.
	Field string
	// Reason says what is wrong with it.
	Reason string
}

// Error implements error.
func (e *InvalidArgumentError) Error() string {
	return fmt.Sprintf("images: invalid %s: %s", e.Field, e.Reason)
}

// ResponseTooLargeError reports a manifest or listing response larger than
// the client reads.
type ResponseTooLargeError struct {
	// Limit is the largest body, in bytes, the client accepts.
	Limit int64
}

// Error implements error.
func (e *ResponseTooLargeError) Error() string {
	return fmt.Sprintf("images: response body exceeds %d bytes", e.Limit)
}
