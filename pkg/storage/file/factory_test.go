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

package file

import (
	"testing"

	"github.com/jeremyhahn/go-codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// -----------------------------------------------------------------------------
// Factory tests
// -----------------------------------------------------------------------------

func TestFactory_NewFactory_CreatesFactory(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	require.NotNil(t, factory)
}

func TestFactory_NewFactory_WithNilLogger(t *testing.T) {
	factory := NewFactory(nil)

	require.NotNil(t, factory)
}

func TestFactory_NewFactory_WithCodecOption(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log, WithCodecType(codec.YAML))

	require.NotNil(t, factory)
	assert.Equal(t, codec.YAML, factory.codecType)
}

func TestFactory_BackendType_ReturnsFile(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	assert.Equal(t, "file", factory.BackendType())
}

func TestFactory_Create_CreatesStorage(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	config := &PartitionConfig{
		DataDir: t.TempDir(),
	}

	storage, err := factory.Create("test-partition", config)

	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()

	// Verify storage works
	err = storage.Set([]byte("key"), []byte("value"))
	require.NoError(t, err)

	val, err := storage.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestFactory_Create_WithNilConfig_ReturnsError(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	storage, err := factory.Create("test-partition", nil)

	require.Error(t, err)
	assert.Nil(t, storage)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestFactory_Create_WithEmptyDataDir_ReturnsError(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	config := &PartitionConfig{
		DataDir: "",
	}

	storage, err := factory.Create("test-partition", config)

	require.Error(t, err)
	assert.Nil(t, storage)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "data directory is required")
}

func TestFactory_Create_WithNilLogger_UsesDefaultLogger(t *testing.T) {
	factory := NewFactory(nil)

	config := &PartitionConfig{
		DataDir: t.TempDir(),
	}

	storage, err := factory.Create("test-partition", config)

	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()
}

func TestFactory_Create_MultiplePartitions(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log)

	tempDir := t.TempDir()

	// Create multiple partitions
	config1 := &PartitionConfig{DataDir: tempDir + "/partition1"}
	config2 := &PartitionConfig{DataDir: tempDir + "/partition2"}
	config3 := &PartitionConfig{DataDir: tempDir + "/partition3"}

	storage1, err := factory.Create("partition-1", config1)
	require.NoError(t, err)
	defer storage1.Close()

	storage2, err := factory.Create("partition-2", config2)
	require.NoError(t, err)
	defer storage2.Close()

	storage3, err := factory.Create("partition-3", config3)
	require.NoError(t, err)
	defer storage3.Close()

	// Each storage should be independent
	require.NoError(t, storage1.Set([]byte("key"), []byte("value1")))
	require.NoError(t, storage2.Set([]byte("key"), []byte("value2")))
	require.NoError(t, storage3.Set([]byte("key"), []byte("value3")))

	val1, err := storage1.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val1)

	val2, err := storage2.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val2)

	val3, err := storage3.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value3"), val3)
}
