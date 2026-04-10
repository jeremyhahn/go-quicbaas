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

package devcluster

import "errors"

// Package-level errors for devcluster operations.
var (
	// ErrManagerAlreadyStarted indicates the manager has already been started.
	ErrManagerAlreadyStarted = errors.New("devcluster manager already started")

	// ErrManagerAlreadyStopped indicates the manager has already been stopped.
	ErrManagerAlreadyStopped = errors.New("devcluster manager already stopped")

	// ErrManagerNotStarted indicates the manager has not been started.
	ErrManagerNotStarted = errors.New("devcluster manager not started")

	// ErrNodeNotFound indicates the requested node was not found.
	ErrNodeNotFound = errors.New("node not found")

	// ErrInvalidNodeCount indicates an invalid node count was specified.
	ErrInvalidNodeCount = errors.New("invalid node count")

	// ErrBaseConfigRequired indicates the base config is required but was not provided.
	ErrBaseConfigRequired = errors.New("base config is required")

	// ErrDataDirCreationFailed indicates the data directory creation failed.
	ErrDataDirCreationFailed = errors.New("failed to create data directory")

	// ErrConfigGenerationFailed indicates config generation failed.
	ErrConfigGenerationFailed = errors.New("failed to generate node configuration")
)

// DevClusterCauseError wraps a devcluster operation error with an underlying cause.
type DevClusterCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *DevClusterCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DevClusterCauseError) Unwrap() error {
	return e.Cause
}

// DevClusterMessageError provides a simple typed error message for the devcluster package.
type DevClusterMessageError struct {
	Message string
}

// Error implements the error interface.
func (e *DevClusterMessageError) Error() string {
	return e.Message
}

// InvalidNodeCountError indicates an invalid node count value was provided.
type InvalidNodeCountError struct {
	Count int
}

// Error implements the error interface.
func (e *InvalidNodeCountError) Error() string {
	return "invalid node count: must be positive, got " + intToStr(e.Count)
}

// NodeNotFoundError indicates a specific node was not found by ID.
type NodeNotFoundError struct {
	NodeID uint64
}

// Error implements the error interface.
func (e *NodeNotFoundError) Error() string {
	return "node not found: node " + uint64ToStr(e.NodeID)
}

// DataDirNodeError indicates a data directory creation failure for a specific node.
type DataDirNodeError struct {
	NodeID uint64
	Subdir string
	Cause  error
}

// Error implements the error interface.
func (e *DataDirNodeError) Error() string {
	msg := "failed to create data directory for node " + uint64ToStr(e.NodeID)
	if e.Subdir != "" {
		msg += " " + e.Subdir
	}
	return msg + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *DataDirNodeError) Unwrap() error {
	return e.Cause
}

// ConfigGenNodeError indicates a config generation failure for a specific node.
type ConfigGenNodeError struct {
	NodeID uint64
	Cause  error
}

// Error implements the error interface.
func (e *ConfigGenNodeError) Error() string {
	return "failed to generate node configuration for node " + uint64ToStr(e.NodeID) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *ConfigGenNodeError) Unwrap() error {
	return e.Cause
}

// intToStr converts an int to a string without importing strconv.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// uint64ToStr converts a uint64 to a string without importing strconv.
func uint64ToStr(n uint64) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
