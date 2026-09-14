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

// This file carries the NPU provider's error contract. It has no build tag so
// that the sentinels resolve in both vector and non-vector builds; callers can
// therefore match on them without compiling the provider itself.
package embedding

import (
	"errors"
	"net/http"
	"strconv"
)

// NPU embedding provider sentinel errors. The typed errors below wrap these so
// callers can match stably with errors.Is while still extracting structured
// detail with errors.As.
var (
	// ErrNPUEndpointRequired is returned when NPUConfig carries no sidecar URL.
	ErrNPUEndpointRequired = errors.New("npu embedding: sidecar endpoint is required")

	// ErrNPUUnavailable is returned when the sidecar cannot be reached at all
	// (connection refused, DNS failure, timeout, context cancellation).
	ErrNPUUnavailable = errors.New("npu embedding: sidecar unavailable")

	// ErrNPUSidecar is returned when the sidecar was reached but answered with
	// a non-success status. The NPU sidecar falls back to CPU internally, so
	// this indicates a genuine failure rather than a device problem.
	ErrNPUSidecar = errors.New("npu embedding: sidecar reported an error")

	// ErrNPUMalformedResponse is returned when the sidecar's reply cannot be
	// decoded or violates the response contract.
	ErrNPUMalformedResponse = errors.New("npu embedding: malformed sidecar response")

	// ErrNPUSecretResolution is returned when the configured credential
	// reference cannot be resolved.
	ErrNPUSecretResolution = errors.New("npu embedding: credential resolution failed")

	// ErrNPUSecretResolverRequired is returned when a non-zero credential
	// reference is configured but no resolver was supplied to resolve it.
	ErrNPUSecretResolverRequired = errors.New("npu embedding: credential reference configured without a resolver")

	// ErrNPUModelMismatch is returned when the sidecar served a model other
	// than the configured one.
	ErrNPUModelMismatch = errors.New("npu embedding: sidecar served a different model")
)

// NPUUnavailableError indicates the sidecar could not be reached. It unwraps to
// both ErrNPUUnavailable and the underlying transport error, so callers may
// match the sentinel or inspect the cause.
type NPUUnavailableError struct {
	// Endpoint is the sidecar base URL that was attempted.
	Endpoint string

	// Err is the underlying transport error.
	Err error
}

// Error implements the error interface.
func (e *NPUUnavailableError) Error() string {
	msg := ErrNPUUnavailable.Error() + ": " + e.Endpoint
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the NPU sentinel, the shared provider-unavailable sentinel,
// and the cause.
//
// ErrProviderUnavailable is included deliberately: an unreachable sidecar is an
// expected backend-unavailable condition, and callers written against the
// general embedding error contract -- including the service layer's mapping to
// ErrUnavailable -- must classify it as such rather than as an internal error.
// A down sidecar surfacing as a 500 would be both wrong for API callers and
// misleading for monitoring.
func (e *NPUUnavailableError) Unwrap() []error {
	out := []error{ErrNPUUnavailable, ErrProviderUnavailable}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

// NPUSidecarError indicates the sidecar answered with a failure status. Code
// and Message carry the sidecar's structured error payload when present.
type NPUSidecarError struct {
	// Endpoint is the sidecar base URL that produced the error.
	Endpoint string

	// StatusCode is the HTTP status returned by the sidecar.
	StatusCode int

	// Code is the sidecar's machine-readable error code, if it supplied one.
	Code string

	// Message is the sidecar's human-readable explanation, if any.
	Message string
}

// Error implements the error interface.
func (e *NPUSidecarError) Error() string {
	msg := ErrNPUSidecar.Error() + ": " + e.Endpoint +
		": status=" + strconv.Itoa(e.StatusCode)
	if e.Code != "" {
		msg += " code=" + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Unwrap returns the NPU sentinel plus, for a retryable status, the matching
// shared sentinel.
//
// A 5xx means the sidecar is in trouble and a 429 means it is shedding load;
// both are backend conditions rather than internal faults, so they map onto the
// general embedding error contract the way an operator and a dashboard expect.
// A 4xx other than 429 is a genuine request problem and maps to neither.
func (e *NPUSidecarError) Unwrap() []error {
	switch {
	case e.StatusCode == http.StatusTooManyRequests:
		return []error{ErrNPUSidecar, ErrRateLimitExceeded}
	case e.StatusCode >= http.StatusInternalServerError:
		return []error{ErrNPUSidecar, ErrProviderUnavailable}
	default:
		return []error{ErrNPUSidecar}
	}
}

// NPUMalformedResponseError indicates the sidecar's reply violated the response
// contract. Reason explains which part of the contract was broken.
type NPUMalformedResponseError struct {
	// Endpoint is the sidecar base URL that produced the response.
	Endpoint string

	// Reason explains the contract violation.
	Reason string

	// Err is the underlying decode error, when the failure was a decode.
	Err error
}

// Error implements the error interface.
func (e *NPUMalformedResponseError) Error() string {
	msg := ErrNPUMalformedResponse.Error() + ": " + e.Endpoint + ": " + e.Reason
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and the cause for errors.Is and errors.As.
func (e *NPUMalformedResponseError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrNPUMalformedResponse}
	}
	return []error{ErrNPUMalformedResponse, e.Err}
}

// NPUDimensionsMismatchError indicates the sidecar returned vectors of a width
// other than the one the provider was configured to advertise. Returning a
// differently-shaped vector would silently corrupt a vector index, so this is
// always an error rather than a warning.
type NPUDimensionsMismatchError struct {
	// Endpoint is the sidecar base URL that produced the response.
	Endpoint string

	// Expected is the configured dimensionality.
	Expected int

	// Actual is the width the sidecar actually returned.
	Actual int

	// Index is the position of the offending vector in the batch, or -1 when the
	// mismatch is in the sidecar's own declared width rather than a vector.
	Index int
}

// Error implements the error interface.
func (e *NPUDimensionsMismatchError) Error() string {
	msg := ErrDimensionsMismatch.Error() + ": " + e.Endpoint +
		": expected=" + strconv.Itoa(e.Expected) +
		" actual=" + strconv.Itoa(e.Actual)
	if e.Index >= 0 {
		return msg + " index=" + strconv.Itoa(e.Index)
	}
	return msg + " (sidecar declared width)"
}

// Unwrap returns the shared dimensions-mismatch sentinel so that callers
// written against the general embedding error contract keep working.
func (e *NPUDimensionsMismatchError) Unwrap() error {
	return ErrDimensionsMismatch
}

// NPUModelMismatchError indicates the sidecar served a model other than the one
// the provider asked for.
//
// This is an error rather than a note because a vector's meaning depends on the
// model that produced it. Two models of the same width yield vectors that are
// silently incomparable, so accepting the wrong one would poison a vector index
// with no visible symptom until retrieval quality quietly collapsed.
type NPUModelMismatchError struct {
	// Endpoint is the sidecar base URL that produced the response.
	Endpoint string

	// Requested is the model the provider is configured for.
	Requested string

	// Served is the model the sidecar reported serving.
	Served string
}

// Error implements the error interface.
func (e *NPUModelMismatchError) Error() string {
	return ErrNPUModelMismatch.Error() + ": " + e.Endpoint +
		": requested=" + e.Requested + " served=" + e.Served
}

// Unwrap returns the model-mismatch sentinel for errors.Is support.
func (e *NPUModelMismatchError) Unwrap() error {
	return ErrNPUModelMismatch
}

// NPUResultCountError indicates the sidecar returned a different number of
// embeddings than the number of texts submitted, which would misalign results
// with their inputs.
type NPUResultCountError struct {
	// Endpoint is the sidecar base URL that produced the response.
	Endpoint string

	// Expected is the number of texts submitted.
	Expected int

	// Actual is the number of embeddings returned.
	Actual int
}

// Error implements the error interface.
func (e *NPUResultCountError) Error() string {
	return ErrNPUMalformedResponse.Error() + ": " + e.Endpoint +
		": expected " + strconv.Itoa(e.Expected) + " embeddings, got " +
		strconv.Itoa(e.Actual)
}

// Unwrap returns the malformed-response sentinel for errors.Is support.
func (e *NPUResultCountError) Unwrap() error {
	return ErrNPUMalformedResponse
}

// NPUInvalidInputError indicates a caller-supplied text was rejected before any
// request was made. Index identifies the offending element within a batch and
// is -1 for single-text calls.
type NPUInvalidInputError struct {
	// Index is the batch position of the offending text, or -1 for Embed.
	Index int

	// Reason explains why the input was rejected.
	Reason string
}

// Error implements the error interface.
func (e *NPUInvalidInputError) Error() string {
	msg := ErrInvalidInput.Error() + ": " + e.Reason
	if e.Index >= 0 {
		msg += " (index " + strconv.Itoa(e.Index) + ")"
	}
	return msg
}

// Unwrap returns the shared invalid-input sentinel for errors.Is support.
func (e *NPUInvalidInputError) Unwrap() error {
	return ErrInvalidInput
}

// NPUConfigError indicates NPUConfig failed validation. Field names the
// offending field.
type NPUConfigError struct {
	// Field is the configuration field that failed validation.
	Field string

	// Reason explains the failure.
	Reason string

	// sentinel is the error this wraps for errors.Is matching.
	sentinel error
}

// Error implements the error interface.
func (e *NPUConfigError) Error() string {
	base := ErrNPUEndpointRequired
	if e.sentinel != nil {
		base = e.sentinel
	}
	msg := base.Error()
	if e.Field != "" {
		msg += ": field=" + e.Field
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the wrapped sentinel for errors.Is support.
func (e *NPUConfigError) Unwrap() error {
	if e.sentinel != nil {
		return e.sentinel
	}
	return ErrNPUEndpointRequired
}

// NPUSecretResolutionError indicates the configured credential reference could
// not be resolved to a value.
type NPUSecretResolutionError struct {
	// Engine is the secrets engine named by the reference.
	Engine string

	// Path is the secrets-engine path named by the reference.
	Path string

	// Err is the underlying resolver error.
	Err error
}

// Error implements the error interface.
func (e *NPUSecretResolutionError) Error() string {
	msg := ErrNPUSecretResolution.Error() + ": engine=" + e.Engine + " path=" + e.Path
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and the cause for errors.Is and errors.As.
func (e *NPUSecretResolutionError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrNPUSecretResolution}
	}
	return []error{ErrNPUSecretResolution, e.Err}
}
