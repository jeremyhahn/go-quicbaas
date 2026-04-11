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

package server

import (
	"errors"
	"strconv"
)

// Server package errors.
var (
	// ErrDataDirRequired indicates a data directory path is required but was not provided.
	ErrDataDirRequired = errors.New("data directory is required")

	// ErrEmbeddingNotCompiled is returned when embedding operations are attempted
	// but the binary was not compiled with the vector build tag.
	ErrEmbeddingNotCompiled = errors.New("embedding support not compiled - build with -tags vector")

	// ErrRAGNotCompiled is returned when RAG operations are attempted
	// but the binary was not compiled with the rag build tag.
	ErrRAGNotCompiled = errors.New("rag support not compiled - build with -tags rag")

	// ErrServerNotRunning indicates an operation was attempted on a stopped server.
	ErrServerNotRunning = errors.New("server is not running")

	// ErrServerAlreadyRunning indicates the server is already running.
	ErrServerAlreadyRunning = errors.New("server is already running")

	// ErrInvalidServerMode indicates an invalid server mode was specified.
	ErrInvalidServerMode = errors.New("invalid server mode")

	// ErrMCPShutdownFailed indicates the MCP server failed to shut down gracefully.
	ErrMCPShutdownFailed = errors.New("failed to shutdown MCP server")

	// ErrRAGCloseFailed indicates the RAG system failed to close gracefully.
	ErrRAGCloseFailed = errors.New("failed to close RAG system")

	// ErrInsecureBootstrapPassword indicates the bootstrap admin password does not
	// meet production security requirements. The password must be at least 12
	// characters and must not be set to default values like "admin".
	// Set auth.insecure_dev_mode=true to bypass this check for development/testing.
	ErrInsecureBootstrapPassword = errors.New("insecure bootstrap admin password")

	// ErrShardedClientClosed indicates ForShard was called after Close.
	ErrShardedClientClosed = errors.New("sharded client is closed")

	// ErrUnsupportedTransport indicates the configured qrdb transport type is not recognized.
	ErrUnsupportedTransport = errors.New("unsupported qrdb transport type")

	// ErrTransportCreationFailed indicates the qrdb SDK transport could not be created.
	ErrTransportCreationFailed = errors.New("qrdb transport creation failed")

	// ErrNetworkTransportMissingAddress indicates a network transport was configured without an address.
	ErrNetworkTransportMissingAddress = errors.New("address is required for network qrdb transport")

	// ErrUnixTransportMissingSocket indicates a unix transport was configured without a socket path.
	ErrUnixTransportMissingSocket = errors.New("unix socket path is required for unix qrdb transport")

	// ErrQRDBClientNotInitialized indicates GetKVStore was called before the
	// qrdb SDK client was initialized. This is a programming error: the SDK
	// must be initialized during server startup before any KVStore access.
	ErrQRDBClientNotInitialized = errors.New("qrdb SDK client not initialized")

	// ErrShardClientFailed indicates ForShard failed to return a client for
	// the requested shard ID.
	ErrShardClientFailed = errors.New("failed to get qrdb client for shard")

	// ErrIAMComponentInit indicates an IAM component failed to initialize.
	ErrIAMComponentInit = errors.New("IAM component initialization failed")
)

// ServerCauseError wraps a server operation error with an underlying cause.
type ServerCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ServerCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ServerCauseError) Unwrap() error {
	return e.Cause
}

// ServerMessageError provides a simple typed error message for the server package.
type ServerMessageError struct {
	Message string
}

// Error implements the error interface.
func (e *ServerMessageError) Error() string {
	return e.Message
}

// InsecureBootstrapPasswordError indicates the bootstrap admin password is
// not strong enough for production use.
type InsecureBootstrapPasswordError struct {
	Reason string
}

// Error implements the error interface.
func (e *InsecureBootstrapPasswordError) Error() string {
	return "insecure bootstrap admin password: " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InsecureBootstrapPasswordError) Unwrap() error {
	return ErrInsecureBootstrapPassword
}

// ShardedClientClosedError is returned when ForShard is called after Close.
type ShardedClientClosedError struct{}

// Error implements the error interface.
func (e *ShardedClientClosedError) Error() string {
	return "sharded client is closed"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ShardedClientClosedError) Unwrap() error {
	return ErrShardedClientClosed
}

// UnsupportedTransportError is returned when the configured qrdb transport
// type is not a recognized transport name.
type UnsupportedTransportError struct {
	Transport string
}

// Error implements the error interface.
func (e *UnsupportedTransportError) Error() string {
	return "unsupported qrdb transport type: " + e.Transport
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnsupportedTransportError) Unwrap() error {
	return ErrUnsupportedTransport
}

// TransportCreationError is returned when a qrdb SDK transport fails to initialize.
type TransportCreationError struct {
	Transport string
	Cause     error
}

// Error implements the error interface.
func (e *TransportCreationError) Error() string {
	return "qrdb " + e.Transport + " transport creation failed: " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TransportCreationError) Unwrap() error {
	return ErrTransportCreationFailed
}

// NetworkTransportMissingAddressError is returned when a network-based
// transport (gRPC, REST, QUIC, MCP) is configured without a target address.
type NetworkTransportMissingAddressError struct {
	Transport string
}

// Error implements the error interface.
func (e *NetworkTransportMissingAddressError) Error() string {
	return "address is required for " + e.Transport + " qrdb transport"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NetworkTransportMissingAddressError) Unwrap() error {
	return ErrNetworkTransportMissingAddress
}

// UnixTransportMissingSocketError is returned when the unix transport is
// configured without a socket path.
type UnixTransportMissingSocketError struct{}

// Error implements the error interface.
func (e *UnixTransportMissingSocketError) Error() string {
	return "unix socket path is required for unix qrdb transport"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnixTransportMissingSocketError) Unwrap() error {
	return ErrUnixTransportMissingSocket
}

// QRDBClientNotInitializedError is returned (as a panic) when GetKVStore
// is called before the qrdb SDK client has been initialized. This is a
// programming error indicating incorrect server startup sequencing.
type QRDBClientNotInitializedError struct{}

// Error implements the error interface.
func (e *QRDBClientNotInitializedError) Error() string {
	return "GetKVStore called before qrdb SDK client initialized"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *QRDBClientNotInitializedError) Unwrap() error {
	return ErrQRDBClientNotInitialized
}

// ShardClientError is returned (as a panic) when ForShard fails to return
// a client for the requested shard ID.
type ShardClientError struct {
	ShardID uint64
	Cause   error
}

// Error implements the error interface.
func (e *ShardClientError) Error() string {
	return "failed to get qrdb client for shard: " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ShardClientError) Unwrap() error {
	return ErrShardClientFailed
}

// StorageInitError indicates a failure during storage initialization.
type StorageInitError struct {
	Backend string
	Err     error
}

// Error implements the error interface.
func (e *StorageInitError) Error() string {
	return "storage init (" + e.Backend + "): " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *StorageInitError) Unwrap() error {
	return e.Err
}

// DatabaseStartError indicates a failure during database subsystem initialization.
type DatabaseStartError struct {
	Err error
}

// Error implements the error interface.
func (e *DatabaseStartError) Error() string {
	return "database subsystem init: " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *DatabaseStartError) Unwrap() error {
	return e.Err
}

// IAMComponentInitError indicates an IAM component failed during initialization.
type IAMComponentInitError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *IAMComponentInitError) Error() string {
	return "failed to create " + e.Component + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *IAMComponentInitError) Unwrap() error {
	return ErrIAMComponentInit
}

// BootstrapPasswordRequiredError indicates the bootstrap admin password is
// required in production mode but was not provided.
type BootstrapPasswordRequiredError struct{}

// Error implements the error interface.
func (e *BootstrapPasswordRequiredError) Error() string {
	return "bootstrap_admin_password is required in production mode"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BootstrapPasswordRequiredError) Unwrap() error {
	return ErrInsecureBootstrapPassword
}

// BootstrapPasswordNotAllowedError indicates the bootstrap admin password
// is a known insecure value that is not permitted in production mode.
// The actual password value is never included in error messages for security.
type BootstrapPasswordNotAllowedError struct{}

// Error implements the error interface.
func (e *BootstrapPasswordNotAllowedError) Error() string {
	return "bootstrap admin password is not allowed"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BootstrapPasswordNotAllowedError) Unwrap() error {
	return ErrInsecureBootstrapPassword
}

// BootstrapPasswordMatchesUsernameError indicates the bootstrap admin password
// matches the username, which is not permitted.
type BootstrapPasswordMatchesUsernameError struct{}

// Error implements the error interface.
func (e *BootstrapPasswordMatchesUsernameError) Error() string {
	return "bootstrap admin password must not match username"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BootstrapPasswordMatchesUsernameError) Unwrap() error {
	return ErrInsecureBootstrapPassword
}

// BootstrapPasswordTooShortError indicates the bootstrap admin password
// does not meet the minimum length requirement.
type BootstrapPasswordTooShortError struct {
	MinLength int
}

// Error implements the error interface.
func (e *BootstrapPasswordTooShortError) Error() string {
	return "bootstrap admin password must be at least " + strconv.Itoa(e.MinLength) + " characters"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BootstrapPasswordTooShortError) Unwrap() error {
	return ErrInsecureBootstrapPassword
}
