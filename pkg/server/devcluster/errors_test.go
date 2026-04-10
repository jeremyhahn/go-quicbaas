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

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevClusterCauseError(t *testing.T) {
	t.Run("Error formats message with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := &DevClusterCauseError{
			Message: "failed to connect",
			Cause:   cause,
		}

		assert.Equal(t, "failed to connect: connection refused", err.Error())
	})

	t.Run("Unwrap returns underlying cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &DevClusterCauseError{
			Message: "wrapper",
			Cause:   cause,
		}

		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("errors.As matches DevClusterCauseError", func(t *testing.T) {
		cause := errors.New("some cause")
		err := &DevClusterCauseError{
			Message: "operation failed",
			Cause:   cause,
		}

		var target *DevClusterCauseError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "operation failed", target.Message)
		assert.Equal(t, cause, target.Cause)
	})
}

func TestDevClusterMessageError(t *testing.T) {
	t.Run("Error returns message", func(t *testing.T) {
		err := &DevClusterMessageError{Message: "something went wrong"}
		assert.Equal(t, "something went wrong", err.Error())
	})

	t.Run("errors.As matches DevClusterMessageError", func(t *testing.T) {
		err := &DevClusterMessageError{Message: "test message"}

		var target *DevClusterMessageError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "test message", target.Message)
	})

	t.Run("empty message returns empty string", func(t *testing.T) {
		err := &DevClusterMessageError{Message: ""}
		assert.Equal(t, "", err.Error())
	})
}

func TestInvalidNodeCountError(t *testing.T) {
	t.Run("Error formats positive count", func(t *testing.T) {
		err := &InvalidNodeCountError{Count: 0}
		assert.Equal(t, "invalid node count: must be positive, got 0", err.Error())
	})

	t.Run("Error formats negative count", func(t *testing.T) {
		err := &InvalidNodeCountError{Count: -5}
		assert.Equal(t, "invalid node count: must be positive, got -5", err.Error())
	})

	t.Run("errors.As matches InvalidNodeCountError", func(t *testing.T) {
		err := &InvalidNodeCountError{Count: -3}

		var target *InvalidNodeCountError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, -3, target.Count)
	})
}

func TestNodeNotFoundError(t *testing.T) {
	t.Run("Error formats node ID", func(t *testing.T) {
		err := &NodeNotFoundError{NodeID: 42}
		assert.Equal(t, "node not found: node 42", err.Error())
	})

	t.Run("Error formats zero node ID", func(t *testing.T) {
		err := &NodeNotFoundError{NodeID: 0}
		assert.Equal(t, "node not found: node 0", err.Error())
	})

	t.Run("errors.As matches NodeNotFoundError", func(t *testing.T) {
		err := &NodeNotFoundError{NodeID: 7}

		var target *NodeNotFoundError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, uint64(7), target.NodeID)
	})
}

func TestDataDirNodeError(t *testing.T) {
	t.Run("Error formats without subdir", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := &DataDirNodeError{
			NodeID: 3,
			Subdir: "",
			Cause:  cause,
		}

		assert.Equal(t, "failed to create data directory for node 3: permission denied", err.Error())
	})

	t.Run("Error formats with subdir", func(t *testing.T) {
		cause := errors.New("disk full")
		err := &DataDirNodeError{
			NodeID: 1,
			Subdir: "WAL",
			Cause:  cause,
		}

		assert.Equal(t, "failed to create data directory for node 1 WAL: disk full", err.Error())
	})

	t.Run("Unwrap returns underlying cause", func(t *testing.T) {
		cause := errors.New("io error")
		err := &DataDirNodeError{
			NodeID: 2,
			Cause:  cause,
		}

		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("errors.As matches DataDirNodeError", func(t *testing.T) {
		cause := errors.New("test")
		err := &DataDirNodeError{
			NodeID: 5,
			Subdir: "snapshots",
			Cause:  cause,
		}

		var target *DataDirNodeError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, uint64(5), target.NodeID)
		assert.Equal(t, "snapshots", target.Subdir)
	})
}

func TestConfigGenNodeError(t *testing.T) {
	t.Run("Error formats message with cause", func(t *testing.T) {
		cause := errors.New("invalid template")
		err := &ConfigGenNodeError{
			NodeID: 4,
			Cause:  cause,
		}

		assert.Equal(t, "failed to generate node configuration for node 4: invalid template", err.Error())
	})

	t.Run("Unwrap returns underlying cause", func(t *testing.T) {
		cause := errors.New("marshal failure")
		err := &ConfigGenNodeError{
			NodeID: 1,
			Cause:  cause,
		}

		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})

	t.Run("errors.As matches ConfigGenNodeError", func(t *testing.T) {
		cause := errors.New("config error")
		err := &ConfigGenNodeError{
			NodeID: 10,
			Cause:  cause,
		}

		var target *ConfigGenNodeError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, uint64(10), target.NodeID)
	})
}

func TestIntToStr(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		assert.Equal(t, "0", intToStr(0))
	})

	t.Run("positive single digit", func(t *testing.T) {
		assert.Equal(t, "7", intToStr(7))
	})

	t.Run("positive multi digit", func(t *testing.T) {
		assert.Equal(t, "12345", intToStr(12345))
	})

	t.Run("negative single digit", func(t *testing.T) {
		assert.Equal(t, "-1", intToStr(-1))
	})

	t.Run("negative multi digit", func(t *testing.T) {
		assert.Equal(t, "-9876", intToStr(-9876))
	})

	t.Run("large positive", func(t *testing.T) {
		assert.Equal(t, "2147483647", intToStr(2147483647))
	})
}

func TestUint64ToStr(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		assert.Equal(t, "0", uint64ToStr(0))
	})

	t.Run("single digit", func(t *testing.T) {
		assert.Equal(t, "9", uint64ToStr(9))
	})

	t.Run("multi digit", func(t *testing.T) {
		assert.Equal(t, "54321", uint64ToStr(54321))
	})

	t.Run("large value", func(t *testing.T) {
		assert.Equal(t, "18446744073709551615", uint64ToStr(18446744073709551615))
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Run("ErrManagerAlreadyStarted", func(t *testing.T) {
		assert.Contains(t, ErrManagerAlreadyStarted.Error(), "already started")
	})

	t.Run("ErrManagerAlreadyStopped", func(t *testing.T) {
		assert.Contains(t, ErrManagerAlreadyStopped.Error(), "already stopped")
	})

	t.Run("ErrManagerNotStarted", func(t *testing.T) {
		assert.Contains(t, ErrManagerNotStarted.Error(), "not started")
	})

	t.Run("ErrNodeNotFound", func(t *testing.T) {
		assert.Contains(t, ErrNodeNotFound.Error(), "not found")
	})

	t.Run("ErrInvalidNodeCount", func(t *testing.T) {
		assert.Contains(t, ErrInvalidNodeCount.Error(), "invalid node count")
	})

	t.Run("ErrBaseConfigRequired", func(t *testing.T) {
		assert.Contains(t, ErrBaseConfigRequired.Error(), "base config")
	})

	t.Run("ErrDataDirCreationFailed", func(t *testing.T) {
		assert.Contains(t, ErrDataDirCreationFailed.Error(), "data directory")
	})

	t.Run("ErrConfigGenerationFailed", func(t *testing.T) {
		assert.Contains(t, ErrConfigGenerationFailed.Error(), "generate node configuration")
	})
}
