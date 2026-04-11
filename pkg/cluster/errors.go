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

package cluster

import (
	"errors"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Inner sentinel errors for QBError Err fields.
var (
	errNoLeader        = errors.New("no leader available")
	errNoHealthyNodes  = errors.New("no healthy nodes available")
	errClusterNotInit  = errors.New("cluster not initialized")
	errNodeNotFound    = errors.New("node not found")
	errContextCanceled = errors.New("context canceled")
)

// Cluster-specific error types.
var (
	// ErrNoLeader is returned when no leader is currently available.
	ErrNoLeader = &dragonerrors.QBError{
		Code: dragonerrors.ErrLeaderNotAvailable,
		Op:   "cluster.Leader",
		Err:  errNoLeader,
	}

	// ErrNoHealthyNodes is returned when no healthy nodes are available.
	ErrNoHealthyNodes = &dragonerrors.QBError{
		Code: dragonerrors.ErrUnavailable,
		Op:   "cluster.NonLeaderNodes",
		Err:  errNoHealthyNodes,
	}

	// ErrClusterNotInitialized is returned when the cluster info is not yet initialized.
	ErrClusterNotInitialized = &dragonerrors.QBError{
		Code: dragonerrors.ErrClusterNotReady,
		Op:   "cluster.Info",
		Err:  errClusterNotInit,
	}

	// ErrNodeNotFound is returned when a requested node is not found.
	ErrNodeNotFound = &dragonerrors.QBError{
		Code: dragonerrors.ErrNotFound,
		Op:   "cluster.Node",
		Err:  errNodeNotFound,
	}

	// ErrContextCanceled is returned when the context is canceled.
	ErrContextCanceled = &dragonerrors.QBError{
		Code: dragonerrors.ErrCanceled,
		Op:   "cluster.Info",
		Err:  errContextCanceled,
	}
)

// NewNoLeaderError creates a new no leader error with additional context.
func NewNoLeaderError(op string) *dragonerrors.QBError {
	return &dragonerrors.QBError{
		Code:  dragonerrors.ErrLeaderNotAvailable,
		Op:    op,
		Err:   errNoLeader,
		Retry: true,
	}
}

// NewNoHealthyNodesError creates a new no healthy nodes error with additional context.
func NewNoHealthyNodesError(op string, totalNodes int) *dragonerrors.QBError {
	return &dragonerrors.QBError{
		Code: dragonerrors.ErrUnavailable,
		Op:   op,
		Err:  errNoHealthyNodes,
		Context: map[string]interface{}{
			"total_nodes": totalNodes,
		},
		Retry: true,
	}
}

// NewNodeNotFoundError creates a new node not found error.
func NewNodeNotFoundError(op string, nodeID uint64) *dragonerrors.QBError {
	return &dragonerrors.QBError{
		Code: dragonerrors.ErrNotFound,
		Op:   op,
		Err:  errNodeNotFound,
		Context: map[string]interface{}{
			"node_id": nodeID,
		},
	}
}

// NewClusterNotInitializedError creates a new cluster not initialized error.
func NewClusterNotInitializedError(op string) *dragonerrors.QBError {
	return &dragonerrors.QBError{
		Code:  dragonerrors.ErrClusterNotReady,
		Op:    op,
		Err:   errClusterNotInit,
		Retry: true,
	}
}

// IsNoLeader returns true if the error indicates no leader is available.
func IsNoLeader(err error) bool {
	if err == nil {
		return false
	}
	var de *dragonerrors.QBError
	if errors.As(err, &de) {
		return de.Code == dragonerrors.ErrLeaderNotAvailable
	}
	return false
}

// IsNoHealthyNodes returns true if the error indicates no healthy nodes are available.
func IsNoHealthyNodes(err error) bool {
	if err == nil {
		return false
	}
	var de *dragonerrors.QBError
	if errors.As(err, &de) {
		return de.Code == dragonerrors.ErrUnavailable
	}
	return false
}

// IsClusterNotInitialized returns true if the error indicates the cluster is not initialized.
func IsClusterNotInitialized(err error) bool {
	if err == nil {
		return false
	}
	var de *dragonerrors.QBError
	if errors.As(err, &de) {
		return de.Code == dragonerrors.ErrClusterNotReady
	}
	return false
}
