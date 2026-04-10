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

package tracing

import (
	"errors"
	"fmt"
)

// Sentinel errors for tracing configuration.
var (
	// ErrConfigNil indicates the tracer configuration is nil.
	ErrConfigNil = errors.New("tracer config cannot be nil")

	// ErrServiceNameRequired indicates the service name is required.
	ErrServiceNameRequired = errors.New("service name is required")

	// ErrShutdownTimeoutNegative indicates the shutdown timeout cannot be negative.
	ErrShutdownTimeoutNegative = errors.New("shutdown timeout cannot be negative")

	// ErrTLSCertRequired indicates a TLS certificate file is required when key is provided.
	ErrTLSCertRequired = errors.New("TLS cert file is required when key file is provided")

	// ErrTLSKeyRequired indicates a TLS key file is required when cert is provided.
	ErrTLSKeyRequired = errors.New("TLS key file is required when cert file is provided")

	// ErrTLSRequired indicates that TLS configuration is required for the OTLP exporter.
	ErrTLSRequired = errors.New("TLS configuration is required for OTLP exporter")
)

// SamplingRatioError indicates an invalid sampling ratio was provided.
type SamplingRatioError struct {
	Ratio float64
}

// Error implements the error interface.
func (e *SamplingRatioError) Error() string {
	return fmt.Sprintf("sampling ratio must be between 0.0 and 1.0, got %f", e.Ratio)
}

// ExporterEndpointError indicates an exporter endpoint is required but not provided.
type ExporterEndpointError struct {
	ExporterType ExporterType
}

// Error implements the error interface.
func (e *ExporterEndpointError) Error() string {
	return fmt.Sprintf("exporter endpoint is required for %s exporter", e.ExporterType)
}

// UnknownExporterError indicates an unknown exporter type was specified.
type UnknownExporterError struct {
	ExporterType ExporterType
}

// Error implements the error interface.
func (e *UnknownExporterError) Error() string {
	return fmt.Sprintf("unknown exporter type: %s", e.ExporterType)
}

// UnsupportedExporterError indicates an unsupported exporter type was specified.
type UnsupportedExporterError struct {
	ExporterType ExporterType
}

// Error implements the error interface.
func (e *UnsupportedExporterError) Error() string {
	return fmt.Sprintf("unsupported exporter type: %s", e.ExporterType)
}

// ResourceCreationError indicates a failure to create an OpenTelemetry resource.
type ResourceCreationError struct {
	Err error
}

// Error implements the error interface.
func (e *ResourceCreationError) Error() string {
	return fmt.Sprintf("failed to create resource: %v", e.Err)
}

// Unwrap returns the underlying error.
func (e *ResourceCreationError) Unwrap() error {
	return e.Err
}

// ExporterCreationError indicates a failure to create a trace exporter.
type ExporterCreationError struct {
	ExporterType string
	Err          error
}

// Error implements the error interface.
func (e *ExporterCreationError) Error() string {
	return fmt.Sprintf("failed to create %s exporter: %v", e.ExporterType, e.Err)
}

// Unwrap returns the underlying error.
func (e *ExporterCreationError) Unwrap() error {
	return e.Err
}

// TLSCAReadError indicates a failure to read the CA certificate file.
type TLSCAReadError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *TLSCAReadError) Error() string {
	return fmt.Sprintf("failed to read CA certificate from %s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying error.
func (e *TLSCAReadError) Unwrap() error {
	return e.Err
}

// TLSCAParseError indicates a failure to parse the CA certificate.
type TLSCAParseError struct {
	Path string
}

// Error implements the error interface.
func (e *TLSCAParseError) Error() string {
	return fmt.Sprintf("failed to add CA certificate from %s to pool", e.Path)
}

// TLSCertLoadError indicates a failure to load the TLS certificate and key.
type TLSCertLoadError struct {
	CertPath string
	KeyPath  string
	Err      error
}

// Error implements the error interface.
func (e *TLSCertLoadError) Error() string {
	return fmt.Sprintf("failed to load client certificate (cert: %s, key: %s): %v", e.CertPath, e.KeyPath, e.Err)
}

// Unwrap returns the underlying error.
func (e *TLSCertLoadError) Unwrap() error {
	return e.Err
}
