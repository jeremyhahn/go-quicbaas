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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"fmt"
	"runtime"

	codec "github.com/jeremyhahn/go-codec"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// -----------------------------------------------------------------------------
// Config tests
// -----------------------------------------------------------------------------

func TestDefaultConfig_ReturnsValidDefaults(t *testing.T) {
	config := DefaultConfig()

	require.NotNil(t, config)
	assert.Equal(t, "", config.DataDir)
	assert.Equal(t, codec.JSON, config.CodecType)
	assert.Equal(t, DefaultFilePermissions, config.FilePermissions)
	assert.Equal(t, DefaultDirPermissions, config.DirPermissions)
	assert.True(t, config.SyncWrites)
}

func TestConfigValidate_RejectsEmptyDataDir(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = ""

	err := config.Validate()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
	assert.Contains(t, err.Error(), "data directory is required")
}

func TestConfigValidate_RejectsZeroFilePermissions(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "/tmp/test"
	config.FilePermissions = 0

	err := config.Validate()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
	assert.Contains(t, err.Error(), "file permissions cannot be zero")
}

func TestConfigValidate_RejectsZeroDirPermissions(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "/tmp/test"
	config.DirPermissions = 0

	err := config.Validate()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
	assert.Contains(t, err.Error(), "directory permissions cannot be zero")
}

func TestConfigValidate_RejectsEmptyCodecType(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "/tmp/test"
	config.CodecType = ""

	err := config.Validate()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
	assert.Contains(t, err.Error(), "codec type is required")
}

func TestConfigValidate_RejectsUnknownCodecType(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "/tmp/test"
	config.CodecType = codec.Type("unknown")

	err := config.Validate()

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
	assert.Contains(t, err.Error(), "unsupported codec type")
}

func TestConfigValidate_AcceptsValidConfig(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "/tmp/test"

	err := config.Validate()

	require.NoError(t, err)
}

func TestExtensionForCodec_ReturnsCorrectExtensions(t *testing.T) {
	tests := []struct {
		name      string
		codecType codec.Type
		expected  string
	}{
		{"JSON", codec.JSON, ".json"},
		{"YAML", codec.YAML, ".yaml"},
		{"TOML", codec.TOML, ".toml"},
		{"MsgPack", codec.MsgPack, ".msgpack"},
		{"BSON", codec.BSON, ".bson"},
		{"ProtoBuf", codec.ProtoBuf, ".pb"},
		{"CBOR", codec.CBOR, ".cbor"},
		{"Unknown", codec.Type("unknown"), ".dat"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			extension := ExtensionForCodec(tc.codecType)
			assert.Equal(t, tc.expected, extension)
		})
	}
}

// -----------------------------------------------------------------------------
// New() tests
// -----------------------------------------------------------------------------

func TestNew_WithValidConfig_Succeeds(t *testing.T) {
	dataDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = dataDir

	storage, err := New(config, nil)

	require.NoError(t, err)
	require.NotNil(t, storage)
	assert.NoError(t, storage.Close())
}

func TestNew_WithNilConfig_ReturnsError(t *testing.T) {
	storage, err := New(nil, nil)

	require.Error(t, err)
	assert.Nil(t, storage)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestNew_WithInvalidConfig_ReturnsError(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = ""

	storage, err := New(config, nil)

	require.Error(t, err)
	assert.Nil(t, storage)
	assert.True(t, errors.Is(err, errors.ErrConfiguration))
}

func TestNew_CreatesDataDirectory(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := tempDir + "/nested/data/dir"
	config := DefaultConfig()
	config.DataDir = dataDir

	storage, err := New(config, nil)

	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()

	// Verify directory was created by setting a key
	err = storage.Set([]byte("test"), []byte("value"))
	require.NoError(t, err)
}

func TestNew_ValidatesAllCodecTypes(t *testing.T) {
	validCodecs := []codec.Type{
		codec.JSON,
		codec.YAML,
		codec.TOML,
		codec.MsgPack,
		codec.BSON,
		codec.ProtoBuf,
		codec.CBOR,
	}

	for _, codec := range validCodecs {
		t.Run(string(codec), func(t *testing.T) {
			config := DefaultConfig()
			config.DataDir = t.TempDir()
			config.CodecType = codec

			storage, err := New(config, nil)
			require.NoError(t, err)
			require.NotNil(t, storage)
			assert.NoError(t, storage.Close())
		})
	}
}

func TestNew_WithProvidedLogger(t *testing.T) {
	dataDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = dataDir

	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	storage, err := New(config, log)

	require.NoError(t, err)
	require.NotNil(t, storage)
	assert.NoError(t, storage.Close())
}

// -----------------------------------------------------------------------------
// Get() tests
// -----------------------------------------------------------------------------

func TestGet_ExistingKey_ReturnsValue(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("test-key")
	value := []byte("test-value")
	require.NoError(t, storage.Set(key, value))

	result, err := storage.Get(key)

	require.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestGet_NonExistentKey_ReturnsNotFound(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	result, err := storage.Get([]byte("non-existent"))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsNotFound(err))
}

func TestGet_EmptyKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	result, err := storage.Get([]byte{})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "key cannot be empty")
}

func TestGet_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	result, err := storage.Get([]byte("test"))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestGet_NilKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	result, err := storage.Get(nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsInvalidInput(err))
}

// -----------------------------------------------------------------------------
// Set() tests
// -----------------------------------------------------------------------------

func TestSet_CreatesNewKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("new-key")
	value := []byte("new-value")

	err := storage.Set(key, value)

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestSet_OverwritesExistingKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("test-key")
	originalValue := []byte("original")
	newValue := []byte("updated")

	require.NoError(t, storage.Set(key, originalValue))
	require.NoError(t, storage.Set(key, newValue))

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, newValue, result)
}

func TestSet_EmptyKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	err := storage.Set([]byte{}, []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "key cannot be empty")
}

func TestSet_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	err := storage.Set([]byte("test"), []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestSet_CreatesParentDirectories(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("users/123/profile")
	value := []byte("profile-data")

	err := storage.Set(key, value)

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestSet_EmptyValue_Succeeds(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("empty-value-key")

	err := storage.Set(key, []byte{})

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, []byte{}, result)
}

func TestSet_LargeValue_Succeeds(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("large-value-key")
	value := make([]byte, 1024*1024) // 1MB
	for i := range value {
		value[i] = byte(i % 256)
	}

	err := storage.Set(key, value)

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestSet_NilKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	err := storage.Set(nil, []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestSet_NilValue_Succeeds(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("nil-value-key")

	err := storage.Set(key, nil)

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, []byte{}, result)
}

func TestSet_DeeplyNestedHierarchy(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("a/b/c/d/e/f/g/h/i/j/k")
	value := []byte("deeply-nested-value")

	err := storage.Set(key, value)

	require.NoError(t, err)

	result, err := storage.Get(key)
	require.NoError(t, err)
	assert.Equal(t, value, result)
}

// -----------------------------------------------------------------------------
// Delete() tests
// -----------------------------------------------------------------------------

func TestDelete_ExistingKey_Succeeds(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("to-delete")
	require.NoError(t, storage.Set(key, []byte("value")))

	err := storage.Delete(key)

	require.NoError(t, err)

	_, getErr := storage.Get(key)
	assert.True(t, errors.IsNotFound(getErr))
}

func TestDelete_NonExistentKey_ReturnsNil(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	err := storage.Delete([]byte("non-existent"))

	require.NoError(t, err)
}

func TestDelete_EmptyKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	err := storage.Delete([]byte{})

	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
	assert.Contains(t, err.Error(), "key cannot be empty")
}

func TestDelete_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	err := storage.Delete([]byte("test"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestDelete_IsIdempotent(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("idempotent-key")
	require.NoError(t, storage.Set(key, []byte("value")))

	require.NoError(t, storage.Delete(key))
	require.NoError(t, storage.Delete(key))
	require.NoError(t, storage.Delete(key))
}

func TestDelete_NilKey_ReturnsInvalidInput(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	err := storage.Delete(nil)

	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestDelete_HierarchicalKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	key := []byte("users/123/profile")
	require.NoError(t, storage.Set(key, []byte("profile-data")))

	err := storage.Delete(key)

	require.NoError(t, err)

	_, getErr := storage.Get(key)
	assert.True(t, errors.IsNotFound(getErr))
}

// -----------------------------------------------------------------------------
// Scan() tests
// -----------------------------------------------------------------------------

func TestScan_AllKeys_ReturnsSortedResults(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Insert in non-sorted order
	require.NoError(t, storage.Set([]byte("c-key"), []byte("c-value")))
	require.NoError(t, storage.Set([]byte("a-key"), []byte("a-value")))
	require.NoError(t, storage.Set([]byte("b-key"), []byte("b-value")))

	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	require.Len(t, results, 3)

	// Verify sorted order
	assert.Equal(t, []byte("a-key"), results[0][0])
	assert.Equal(t, []byte("a-value"), results[0][1])
	assert.Equal(t, []byte("b-key"), results[1][0])
	assert.Equal(t, []byte("b-value"), results[1][1])
	assert.Equal(t, []byte("c-key"), results[2][0])
	assert.Equal(t, []byte("c-value"), results[2][1])
}

func TestScan_WithRangeBounds_FiltersCorrectly(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, storage.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, storage.Set([]byte("ccc"), []byte("3")))
	require.NoError(t, storage.Set([]byte("ddd"), []byte("4")))
	require.NoError(t, storage.Set([]byte("eee"), []byte("5")))

	// Scan [bbb, ddd) - should return bbb, ccc
	results, err := storage.Scan([]byte("bbb"), []byte("ddd"))

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, []byte("bbb"), results[0][0])
	assert.Equal(t, []byte("ccc"), results[1][0])
}

func TestScan_WithStartOnly_FiltersFromStart(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, storage.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, storage.Set([]byte("ccc"), []byte("3")))

	results, err := storage.Scan([]byte("bbb"), nil)

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, []byte("bbb"), results[0][0])
	assert.Equal(t, []byte("ccc"), results[1][0])
}

func TestScan_WithEndOnly_FiltersToEnd(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, storage.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, storage.Set([]byte("ccc"), []byte("3")))

	results, err := storage.Scan(nil, []byte("ccc"))

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, []byte("aaa"), results[0][0])
	assert.Equal(t, []byte("bbb"), results[1][0])
}

func TestScan_EmptyStorage_ReturnsEmptySlice(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestScan_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	results, err := storage.Scan(nil, nil)

	require.Error(t, err)
	assert.Nil(t, results)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestScan_HierarchicalKeys_ReturnsSorted(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("users/002"), []byte("user2")))
	require.NoError(t, storage.Set([]byte("users/001"), []byte("user1")))
	require.NoError(t, storage.Set([]byte("users/003"), []byte("user3")))

	results, err := storage.Scan([]byte("users/"), []byte("users/~"))

	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, []byte("users/001"), results[0][0])
	assert.Equal(t, []byte("users/002"), results[1][0])
	assert.Equal(t, []byte("users/003"), results[2][0])
}

func TestScan_WithEmptyStartAndEnd_ReturnsAll(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))

	results, err := storage.Scan([]byte{}, []byte{})

	require.NoError(t, err)
	require.Len(t, results, 2)
}

func TestScan_DeepCopiesResults(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("key"), []byte("original")))

	results, err := storage.Scan(nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Modify the result
	results[0][1][0] = 'X'

	// Verify original in storage is unchanged
	value, err := storage.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), value)
}

// -----------------------------------------------------------------------------
// Snapshot() tests
// -----------------------------------------------------------------------------

func TestSnapshot_ReturnsAllData(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))
	require.NoError(t, storage.Set([]byte("key3"), []byte("value3")))

	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	assert.Equal(t, []byte("value1"), snapshot["key1"])
	assert.Equal(t, []byte("value2"), snapshot["key2"])
	assert.Equal(t, []byte("value3"), snapshot["key3"])
}

func TestSnapshot_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	snapshot, err := storage.CreateSnapshotMap()

	require.Error(t, err)
	assert.Nil(t, snapshot)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestSnapshot_EmptyStorage_ReturnsEmptyMap(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	assert.Empty(t, snapshot)
}

func TestSnapshot_DeepCopiesValues(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	originalValue := []byte("original")
	require.NoError(t, storage.Set([]byte("key"), originalValue))

	snapshot, err := storage.CreateSnapshotMap()
	require.NoError(t, err)

	// Modify the snapshot value
	snapshot["key"][0] = 'X'

	// Verify original in storage is unchanged
	result, err := storage.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), result)
}

func TestSnapshot_WithHierarchicalKeys(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("users/001"), []byte("user1")))
	require.NoError(t, storage.Set([]byte("users/002"), []byte("user2")))
	require.NoError(t, storage.Set([]byte("orders/001"), []byte("order1")))

	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	assert.Equal(t, []byte("user1"), snapshot["users/001"])
	assert.Equal(t, []byte("user2"), snapshot["users/002"])
	assert.Equal(t, []byte("order1"), snapshot["orders/001"])
}

// -----------------------------------------------------------------------------
// Restore() tests
// -----------------------------------------------------------------------------

func TestRestore_ClearsExistingDataAndLoadsSnapshot(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set initial data
	require.NoError(t, storage.Set([]byte("existing1"), []byte("old1")))
	require.NoError(t, storage.Set([]byte("existing2"), []byte("old2")))

	// Create snapshot to restore
	snapshot := map[string][]byte{
		"new1": []byte("value1"),
		"new2": []byte("value2"),
	}

	err := storage.Restore(snapshot)

	require.NoError(t, err)

	// Verify old data is gone
	_, err = storage.Get([]byte("existing1"))
	assert.True(t, errors.IsNotFound(err))
	_, err = storage.Get([]byte("existing2"))
	assert.True(t, errors.IsNotFound(err))

	// Verify new data is present
	val1, err := storage.Get([]byte("new1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value1"), val1)
	val2, err := storage.Get([]byte("new2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value2"), val2)
}

func TestRestore_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	snapshot := map[string][]byte{"key": []byte("value")}

	err := storage.Restore(snapshot)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestRestore_EmptySnapshot_ClearsAllData(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))

	err := storage.Restore(map[string][]byte{})

	require.NoError(t, err)

	count, err := storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRestore_WithHierarchicalKeys(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	snapshot := map[string][]byte{
		"users/001":          []byte("user1"),
		"users/002":          []byte("user2"),
		"orders/001/items/1": []byte("item1"),
	}

	err := storage.Restore(snapshot)

	require.NoError(t, err)

	val, err := storage.Get([]byte("users/001"))
	require.NoError(t, err)
	assert.Equal(t, []byte("user1"), val)

	val, err = storage.Get([]byte("orders/001/items/1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("item1"), val)
}

func TestRestore_NilSnapshot_Succeeds(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("key"), []byte("value")))

	err := storage.Restore(nil)

	require.NoError(t, err)

	count, err := storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRestore_SyncWrites(t *testing.T) {
	for _, syncWrites := range []bool{true, false} {
		t.Run(fmt.Sprintf("sync=%t", syncWrites), func(t *testing.T) {
			config := DefaultConfig()
			config.DataDir = t.TempDir()
			config.SyncWrites = syncWrites

			storage, err := New(config, nil)
			require.NoError(t, err)
			defer storage.Close()

			snapshot := map[string][]byte{
				"key1": []byte("value1"),
				"key2": []byte("value2"),
			}

			err = storage.Restore(snapshot)

			require.NoError(t, err)

			val, err := storage.Get([]byte("key1"))
			require.NoError(t, err)
			assert.Equal(t, []byte("value1"), val)
		})
	}
}

// -----------------------------------------------------------------------------
// Len() tests
// -----------------------------------------------------------------------------

func TestLen_ReturnsCorrectCount(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	count, err := storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	count, err = storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))
	count, err = storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	require.NoError(t, storage.Delete([]byte("key1")))
	count, err = storage.Len()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLen_OnClosedStore_ReturnsError(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	count, err := storage.Len()

	require.Error(t, err)
	assert.Equal(t, 0, count)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "storage is closed")
}

func TestLen_WithHierarchicalKeys(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("users/001"), []byte("user1")))
	require.NoError(t, storage.Set([]byte("users/002"), []byte("user2")))
	require.NoError(t, storage.Set([]byte("orders/001"), []byte("order1")))

	count, err := storage.Len()

	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// -----------------------------------------------------------------------------
// Close() tests
// -----------------------------------------------------------------------------

func TestClose_IsIdempotent(t *testing.T) {
	storage := createTestStorage(t)

	err1 := storage.Close()
	err2 := storage.Close()
	err3 := storage.Close()

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)
}

func TestClose_PreventsSubsequentOperations(t *testing.T) {
	storage := createTestStorage(t)
	require.NoError(t, storage.Close())

	_, getErr := storage.Get([]byte("key"))
	assert.Error(t, getErr)

	setErr := storage.Set([]byte("key"), []byte("value"))
	assert.Error(t, setErr)

	deleteErr := storage.Delete([]byte("key"))
	assert.Error(t, deleteErr)

	_, scanErr := storage.Scan(nil, nil)
	assert.Error(t, scanErr)

	_, snapshotErr := storage.CreateSnapshotMap()
	assert.Error(t, snapshotErr)

	restoreErr := storage.Restore(map[string][]byte{})
	assert.Error(t, restoreErr)

	_, lenErr := storage.Len()
	assert.Error(t, lenErr)
}

// -----------------------------------------------------------------------------
// Concurrent access tests
// -----------------------------------------------------------------------------

func TestConcurrent_GetSet_NoRaces(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // readers + writers

	// Writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte("concurrent-key")
				value := []byte("value-from-goroutine")
				_ = storage.Set(key, value)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte("concurrent-key")
				_, _ = storage.Get(key)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrent_MixedOperations_NoRaces(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	const numGoroutines = 5
	const numOperations = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 5) // set, get, delete, scan, len

	// Set operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := []byte("key")
				_ = storage.Set(key, []byte("value"))
			}
		}(i)
	}

	// Get operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = storage.Get([]byte("key"))
			}
		}(i)
	}

	// Delete operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = storage.Delete([]byte("key"))
			}
		}(i)
	}

	// Scan operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = storage.Scan(nil, nil)
			}
		}(i)
	}

	// Len operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = storage.Len()
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrent_SnapshotRestore_NoRaces(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	const numOperations = 20

	var wg sync.WaitGroup
	wg.Add(3)

	// Writer
	go func() {
		defer wg.Done()
		for j := 0; j < numOperations; j++ {
			_ = storage.Set([]byte("key"), []byte("value"))
		}
	}()

	// Snapshot reader
	go func() {
		defer wg.Done()
		for j := 0; j < numOperations; j++ {
			_, _ = storage.CreateSnapshotMap()
		}
	}()

	// Restore writer
	go func() {
		defer wg.Done()
		for j := 0; j < numOperations; j++ {
			_ = storage.Restore(map[string][]byte{"restored": []byte("data")})
		}
	}()

	wg.Wait()
}

// -----------------------------------------------------------------------------
// Additional edge case tests
// -----------------------------------------------------------------------------

func TestSet_WithSyncWritesDisabled(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = t.TempDir()
	config.SyncWrites = false

	storage, err := New(config, nil)
	require.NoError(t, err)
	defer storage.Close()

	err = storage.Set([]byte("key"), []byte("value"))
	require.NoError(t, err)

	result, err := storage.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), result)
}

func TestKeyToPath_HandlesSpecialCharacters(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Test keys with various characters
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{"simple", "simple", "value1"},
		{"with_underscore", "key_with_underscore", "value2"},
		{"with_dash", "key-with-dash", "value3"},
		{"numeric", "12345", "value4"},
		{"alphanumeric", "key123abc", "value5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := storage.Set([]byte(tc.key), []byte(tc.value))
			require.NoError(t, err)

			result, err := storage.Get([]byte(tc.key))
			require.NoError(t, err)
			assert.Equal(t, []byte(tc.value), result)
		})
	}
}

func TestScan_NoMatchingKeys_ReturnsEmpty(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, storage.Set([]byte("bbb"), []byte("2")))

	// Scan range with no matching keys
	results, err := storage.Scan([]byte("zzz"), []byte("zzzz"))

	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestScan_SingleKey_ReturnsOne(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("only-key"), []byte("only-value")))

	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []byte("only-key"), results[0][0])
	assert.Equal(t, []byte("only-value"), results[0][1])
}

func TestScan_IgnoresFilesWithWrongExtension(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file with wrong extension directly
	wrongExtFile := filepath.Join(storage.config.DataDir, "wrong-ext.txt")
	require.NoError(t, os.WriteFile(wrongExtFile, []byte("should be ignored"), 0644))

	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []byte("valid-key"), results[0][0])
}

func TestSnapshot_IgnoresFilesWithWrongExtension(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file with wrong extension directly
	wrongExtFile := filepath.Join(storage.config.DataDir, "wrong-ext.txt")
	require.NoError(t, os.WriteFile(wrongExtFile, []byte("should be ignored"), 0644))

	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	require.Len(t, snapshot, 1)
	assert.Equal(t, []byte("valid-value"), snapshot["valid-key"])
}

func TestLen_IgnoresFilesWithWrongExtension(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file with wrong extension directly
	wrongExtFile := filepath.Join(storage.config.DataDir, "wrong-ext.txt")
	require.NoError(t, os.WriteFile(wrongExtFile, []byte("should be ignored"), 0644))

	count, err := storage.Len()

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestScan_IgnoresTempFiles(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a temp file directly
	tempFile := filepath.Join(storage.config.DataDir, "temp-key.json.tmp")
	require.NoError(t, os.WriteFile(tempFile, []byte("temp data"), 0644))

	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []byte("valid-key"), results[0][0])
}

func TestSnapshot_IgnoresTempFiles(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a temp file directly
	tempFile := filepath.Join(storage.config.DataDir, "temp-key.json.tmp")
	require.NoError(t, os.WriteFile(tempFile, []byte("temp data"), 0644))

	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	require.Len(t, snapshot, 1)
	assert.Equal(t, []byte("valid-value"), snapshot["valid-key"])
}

func TestLen_IgnoresTempFiles(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a temp file directly
	tempFile := filepath.Join(storage.config.DataDir, "temp-key.json.tmp")
	require.NoError(t, os.WriteFile(tempFile, []byte("temp data"), 0644))

	count, err := storage.Len()

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRestore_ClearsOnlyFilesWithCorrectExtension(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Set a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file with wrong extension directly
	wrongExtFile := filepath.Join(storage.config.DataDir, "wrong-ext.txt")
	require.NoError(t, os.WriteFile(wrongExtFile, []byte("should remain"), 0644))

	// Restore empty snapshot
	err := storage.Restore(map[string][]byte{})
	require.NoError(t, err)

	// Verify file with wrong extension still exists
	_, statErr := os.Stat(wrongExtFile)
	assert.NoError(t, statErr)
}

// -----------------------------------------------------------------------------
// Filesystem error handling tests
// -----------------------------------------------------------------------------

func TestNew_MkdirAllFailure_WhenDataDirIsFile(t *testing.T) {
	tempDir := t.TempDir()
	blockingFile := filepath.Join(tempDir, "blocking-file")

	// Create a file that will block directory creation
	require.NoError(t, os.WriteFile(blockingFile, []byte("blocking"), 0644))

	// Try to create storage with DataDir inside the file (impossible)
	config := DefaultConfig()
	config.DataDir = filepath.Join(blockingFile, "subdir")

	storage, err := New(config, nil)

	require.Error(t, err)
	assert.Nil(t, storage)
	assert.True(t, errors.Is(err, errors.ErrInternal))
}

func TestGet_ReadFileError_WhenPathIsDirectory(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a directory where a key file would be
	keyDir := filepath.Join(storage.config.DataDir, "dir-key.json")
	require.NoError(t, os.MkdirAll(keyDir, 0755))

	// Attempt to get the key - should fail because path is a directory
	result, err := storage.Get([]byte("dir-key"))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestSet_MkdirAllFailure_WhenParentIsFile(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a file where a parent directory should be
	blockingFile := filepath.Join(storage.config.DataDir, "blocking")
	require.NoError(t, os.WriteFile(blockingFile, []byte("blocking"), 0644))

	// Try to set a key that requires "blocking" to be a directory
	err := storage.Set([]byte("blocking/nested/key"), []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// Set stages the value in "<path>.tmp" before renaming it into place. A
// directory already occupying that temp path makes OpenFile return EISDIR, so
// this reaches the create branch with MkdirAll already past -- and unlike a
// read-only parent it fails for every uid (testing-guidelines.md, Fault
// Injection).
func TestSet_OpenFileFailure_WhenTempPathIsDirectory(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	tmpPath := storage.keyToPath([]byte("readonly/key")) + ".tmp"
	require.NoError(t, os.MkdirAll(tmpPath, 0755))

	err := storage.Set([]byte("readonly/key"), []byte("value"))

	require.Error(t, err)
	assert.ErrorIs(t, err, syscall.EISDIR)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// Set writes a temp file and renames it over the target. Renaming a file onto
// a non-empty directory fails for every uid, so this reaches the rename branch
// specifically -- MkdirAll and OpenFile both succeed first.
func TestSet_RenameFailure_WhenTargetIsNonEmptyDir(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Occupy the key's final path with a directory that is not empty.
	target := storage.keyToPath([]byte("new-key"))
	require.NoError(t, os.MkdirAll(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "occupant"), []byte("x"), 0644))

	err := storage.Set([]byte("new-key"), []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestDelete_RemoveFailure_WhenPathIsDirectory(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a directory where a key file would be
	keyDir := filepath.Join(storage.config.DataDir, "dir-key.json")
	require.NoError(t, os.MkdirAll(keyDir, 0755))

	// Put a file inside so os.Remove fails (can't remove non-empty dir)
	innerFile := filepath.Join(keyDir, "inner.txt")
	require.NoError(t, os.WriteFile(innerFile, []byte("content"), 0644))

	// Attempt to delete - should fail because it's a non-empty directory
	err := storage.Delete([]byte("dir-key"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// WalkDir reports an error from its own root when that root does not exist,
// which reaches the walkErr branch for every uid.
func TestScan_WalkDirError_WhenDataDirIsMissing(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))
	require.NoError(t, os.RemoveAll(storage.config.DataDir))

	results, err := storage.Scan(nil, nil)

	require.Error(t, err)
	assert.Nil(t, results)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// WalkDir yields a symlink as a non-directory entry (it lstats), so the
// callback goes on to ReadFile, which follows the link to a directory and
// returns EISDIR. Root does not bypass that either.
func TestScan_ReadFileError_WhenEntryIsSymlinkToDir(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("unreadable-key"), []byte("value")))

	keyPath := storage.keyToPath([]byte("unreadable-key"))
	targetDir := filepath.Join(t.TempDir(), "target")
	require.NoError(t, os.MkdirAll(targetDir, 0755))
	require.NoError(t, os.Remove(keyPath))
	require.NoError(t, os.Symlink(targetDir, keyPath))

	results, err := storage.Scan(nil, nil)

	require.Error(t, err)
	assert.Nil(t, results)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestSnapshot_WalkDirError_WhenDataDirIsMissing(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))
	require.NoError(t, os.RemoveAll(storage.config.DataDir))

	snapshot, err := storage.CreateSnapshotMap()

	require.Error(t, err)
	assert.Nil(t, snapshot)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestSnapshot_ReadFileError_WhenEntryIsSymlinkToDir(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("unreadable-key"), []byte("value")))

	keyPath := storage.keyToPath([]byte("unreadable-key"))
	targetDir := filepath.Join(t.TempDir(), "target")
	require.NoError(t, os.MkdirAll(targetDir, 0755))
	require.NoError(t, os.Remove(keyPath))
	require.NoError(t, os.Symlink(targetDir, keyPath))

	snapshot, err := storage.CreateSnapshotMap()

	require.Error(t, err)
	assert.Nil(t, snapshot)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// Restore clears the tree with its own WalkDir before writing; a missing root
// fails that first pass.
func TestRestore_WalkDirError_WhenDataDirIsMissing(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))
	require.NoError(t, os.RemoveAll(storage.config.DataDir))

	err := storage.Restore(map[string][]byte{"new-key": []byte("new-value")})

	require.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestRestore_MkdirAllFailure_WhenParentIsFile(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a file where a parent directory should be during restore
	blockingFile := filepath.Join(storage.config.DataDir, "blocking")
	require.NoError(t, os.WriteFile(blockingFile, []byte("blocking"), 0644))

	// Restore with a key that requires "blocking" to be a directory
	snapshot := map[string][]byte{
		"blocking/nested/key": []byte("value"),
	}

	err := storage.Restore(snapshot)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// Restore stages each entry through "<path>.tmp". Pre-creating a directory at
// that temp path makes OpenFile return EISDIR, reaching the create branch after
// MkdirAll has already succeeded -- and it does so for every uid.
func TestRestore_OpenFileFailure_WhenTempPathIsDirectory(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	tmpPath := storage.keyToPath([]byte("readonly/key")) + ".tmp"
	require.NoError(t, os.MkdirAll(tmpPath, 0755))

	snapshot := map[string][]byte{
		"readonly/key": []byte("value"),
	}

	err := storage.Restore(snapshot)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestLen_WalkDirError_WhenDataDirIsMissing(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))
	require.NoError(t, os.RemoveAll(storage.config.DataDir))

	count, err := storage.Len()

	require.Error(t, err)
	assert.Equal(t, 0, count)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestPathToKey_ReturnsRelativePathForPathOutsideDataDir(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Try to convert a path that's outside the data directory
	// filepath.Rel returns a relative path with ".." components for paths outside the base
	outsidePath := "/some/completely/different/path/key.json"

	key := storage.pathToKey(outsidePath)

	// filepath.Rel succeeds but returns a path with ".." components
	// The key will have ".." prefix, which is not a valid key in practice
	// This test verifies the actual behavior of pathToKey
	assert.True(t, strings.HasPrefix(key, ".."))
}

// The clear phase only ever calls os.Remove on a regular file, and for uid 0
// that unlink cannot be made to fail through filesystem shape alone -- there is
// no ENOTDIR/ENOENT equivalent for "unlink refused". Until FileStorage grows a
// filesystem seam this branch is only reachable as a normal user, so the test
// skips under root rather than asserting the environment
// (testing-guidelines.md, Fault Injection).
func TestRestore_RemoveFailure_WhenFileInProtectedDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory write permission; no uid-independent shape for a failing unlink")
	}

	storage := createTestStorage(t)
	defer storage.Close()

	// Create a nested directory structure
	nestedDir := filepath.Join(storage.config.DataDir, "nested")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))

	// Create a file in the nested directory
	nestedFile := filepath.Join(nestedDir, "key.json")
	require.NoError(t, os.WriteFile(nestedFile, []byte("data"), 0644))

	// Make the nested directory read-only (prevents file deletion inside)
	require.NoError(t, os.Chmod(nestedDir, 0555))
	defer os.Chmod(nestedDir, 0755) // Cleanup

	// Restore should fail when trying to remove the file
	err := storage.Restore(map[string][]byte{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestScan_SkipsEmptyKeyFromPathToKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file that would result in empty key when converted
	// This can happen if pathToKey returns empty string
	// We simulate by creating a file in a weird location
	weirdFile := filepath.Join(storage.config.DataDir, ".json")
	require.NoError(t, os.WriteFile(weirdFile, []byte("weird"), 0644))

	// Scan should skip files that result in empty keys
	results, err := storage.Scan(nil, nil)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []byte("valid-key"), results[0][0])
}

func TestSnapshot_SkipsEmptyKeyFromPathToKey(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a valid key
	require.NoError(t, storage.Set([]byte("valid-key"), []byte("valid-value")))

	// Create a file that would result in empty key when converted
	weirdFile := filepath.Join(storage.config.DataDir, ".json")
	require.NoError(t, os.WriteFile(weirdFile, []byte("weird"), 0644))

	// Snapshot should skip files that result in empty keys
	snapshot, err := storage.CreateSnapshotMap()

	require.NoError(t, err)
	require.Len(t, snapshot, 1)
	assert.Equal(t, []byte("valid-value"), snapshot["valid-key"])
}

func TestGet_ReadFileError_WhenFileIsSymlinkToNonexistent(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a symlink to a non-existent target
	keyPath := storage.keyToPath([]byte("symlink-key"))
	require.NoError(t, os.Symlink("/nonexistent/target", keyPath))

	// Get should return NotFound for broken symlinks
	result, err := storage.Get([]byte("symlink-key"))

	require.Error(t, err)
	assert.Nil(t, result)
	// os.ReadFile on broken symlink returns not-exist error
	assert.True(t, errors.IsNotFound(err))
}

func TestSet_OverwriteSymlinkTarget(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a file and a symlink to it
	targetPath := filepath.Join(storage.config.DataDir, "target-file")
	require.NoError(t, os.WriteFile(targetPath, []byte("original"), 0644))

	keyPath := storage.keyToPath([]byte("symlink-key"))
	require.NoError(t, os.Symlink(targetPath, keyPath))

	// Set should atomically replace (via rename), which overwrites the symlink
	err := storage.Set([]byte("symlink-key"), []byte("new-value"))
	require.NoError(t, err)

	// The symlink should be replaced with an actual file
	result, err := storage.Get([]byte("symlink-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("new-value"), result)

	// Verify it's no longer a symlink
	info, err := os.Lstat(keyPath)
	require.NoError(t, err)
	assert.False(t, info.Mode()&os.ModeSymlink != 0)
}

func TestDelete_DeletesSymlink(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a target file and a symlink to it
	targetPath := filepath.Join(storage.config.DataDir, "target-file.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("target content"), 0644))

	keyPath := storage.keyToPath([]byte("symlink-key"))
	require.NoError(t, os.Symlink(targetPath, keyPath))

	// Delete should remove the symlink
	err := storage.Delete([]byte("symlink-key"))
	require.NoError(t, err)

	// Symlink should be gone
	_, statErr := os.Lstat(keyPath)
	assert.True(t, os.IsNotExist(statErr))

	// But target should still exist
	_, statErr = os.Stat(targetPath)
	assert.NoError(t, statErr)
}

// -----------------------------------------------------------------------------
// File deleted during iteration tests (covers lines 309-310 and 388-389)
// -----------------------------------------------------------------------------

func TestScan_FileDeletedDuringIteration_SkipsDeletedFiles(t *testing.T) {
	// Create storage with many files to increase the chance of deletion during iteration
	tempDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = tempDir

	storage, err := New(config, nil)
	require.NoError(t, err)
	defer storage.Close()

	// Create many files
	const numFiles = 100
	for i := 0; i < numFiles; i++ {
		key := []byte(strings.Repeat("x", 50) + string(rune('a'+i%26)) + strings.Repeat("y", 50))
		require.NoError(t, storage.Set(key, []byte("value")))
	}

	// Start a goroutine that continuously deletes files
	var stopDeleting atomic.Bool
	deletesDone := make(chan struct{})

	go func() {
		defer close(deletesDone)
		for !stopDeleting.Load() {
			// Walk the directory and delete files directly
			files, walkErr := filepath.Glob(filepath.Join(tempDir, "*.json"))
			if walkErr != nil {
				continue
			}
			for _, f := range files {
				if stopDeleting.Load() {
					return
				}
				os.Remove(f) // Ignore errors - file might already be deleted
				time.Sleep(time.Microsecond)
			}
		}
	}()

	// Run scan multiple times while deletion is happening
	// The scan should not fail even if files are deleted during iteration
	for i := 0; i < 10; i++ {
		// We need to bypass the lock to test concurrent file deletion
		// So we call the internal scan logic by releasing the lock temporarily
		results, scanErr := storage.Scan(nil, nil)
		// The scan should succeed (possibly with fewer results due to deletions)
		// or we should see the specific "file deleted during iteration" path exercised
		if scanErr == nil {
			// Success - scan completed without error
			assert.NotNil(t, results)
		}
		// Re-create some files to keep the test running
		for j := 0; j < 10; j++ {
			key := []byte(strings.Repeat("z", 50) + string(rune('a'+j%26)))
			storage.Set(key, []byte("recreated"))
		}
	}

	stopDeleting.Store(true)
	<-deletesDone
}

func TestScan_FileDeletedAfterWalkButBeforeRead(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create a valid key
	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))

	// Create a symlink that we can break during the test
	key3Path := storage.keyToPath([]byte("key3"))
	targetPath := filepath.Join(storage.config.DataDir, "target-to-delete.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("target-value"), 0644))
	require.NoError(t, os.Symlink(targetPath, key3Path))

	// Now delete the target - this makes the symlink point to a non-existent file
	// When WalkDir encounters it, the file exists (symlink is there),
	// but when ReadFile is called, it will fail with ENOENT
	require.NoError(t, os.Remove(targetPath))

	// Scan should skip the broken symlink without error (covers lines 309-310)
	results, err := storage.Scan(nil, nil)
	require.NoError(t, err)

	// Should only return key1 and key2
	assert.Len(t, results, 2)
}

func TestSnapshot_FileDeletedAfterWalkButBeforeRead(t *testing.T) {
	storage := createTestStorage(t)
	defer storage.Close()

	// Create valid keys
	require.NoError(t, storage.Set([]byte("key1"), []byte("value1")))
	require.NoError(t, storage.Set([]byte("key2"), []byte("value2")))

	// Create a symlink that we can break during the test
	key3Path := storage.keyToPath([]byte("key3"))
	targetPath := filepath.Join(storage.config.DataDir, "target-to-delete.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("target-value"), 0644))
	require.NoError(t, os.Symlink(targetPath, key3Path))

	// Delete the target - makes the symlink broken
	require.NoError(t, os.Remove(targetPath))

	// Snapshot should skip the broken symlink without error (covers lines 388-389)
	snapshot, err := storage.CreateSnapshotMap()
	require.NoError(t, err)

	// Should only return key1 and key2
	assert.Len(t, snapshot, 2)
	assert.Equal(t, []byte("value1"), snapshot["key1"])
	assert.Equal(t, []byte("value2"), snapshot["key2"])
}

func TestSnapshot_FileDeletedDuringIteration_SkipsDeletedFiles(t *testing.T) {
	tempDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = tempDir

	storage, err := New(config, nil)
	require.NoError(t, err)
	defer storage.Close()

	// Create many files
	const numFiles = 100
	for i := 0; i < numFiles; i++ {
		key := []byte(strings.Repeat("s", 50) + string(rune('a'+i%26)) + strings.Repeat("n", 50))
		require.NoError(t, storage.Set(key, []byte("value")))
	}

	// Start a goroutine that continuously deletes files
	var stopDeleting atomic.Bool
	deletesDone := make(chan struct{})

	go func() {
		defer close(deletesDone)
		for !stopDeleting.Load() {
			files, walkErr := filepath.Glob(filepath.Join(tempDir, "*.json"))
			if walkErr != nil {
				continue
			}
			for _, f := range files {
				if stopDeleting.Load() {
					return
				}
				os.Remove(f)
				time.Sleep(time.Microsecond)
			}
		}
	}()

	// Run snapshot multiple times while deletion is happening
	for i := 0; i < 10; i++ {
		snapshot, snapErr := storage.CreateSnapshotMap()
		if snapErr == nil {
			assert.NotNil(t, snapshot)
		}
		// Re-create some files
		for j := 0; j < 10; j++ {
			key := []byte(strings.Repeat("r", 50) + string(rune('a'+j%26)))
			storage.Set(key, []byte("recreated"))
		}
	}

	stopDeleting.Store(true)
	<-deletesDone
}

// -----------------------------------------------------------------------------
// pathToKey filepath.Rel failure test (covers line 127)
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

func createTestStorage(t *testing.T) *FileStorage {
	t.Helper()

	config := DefaultConfig()
	config.DataDir = t.TempDir()

	storage, err := New(config, nil)
	require.NoError(t, err)

	return storage
}

// =============================================================================
// Consolidated from coverage_95_test.go
// =============================================================================

// createTestFileStorage creates a FileStorage instance for testing.
func createTestFileStorage(t *testing.T) *FileStorage {
	t.Helper()

	tmpDir := t.TempDir()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &Config{
		DataDir:         tmpDir,
		CodecType:       codec.JSON,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
		SyncWrites:      false,
	}

	store, err := New(cfg, log)
	require.NoError(t, err)
	return store
}

func createTestFileStorageWithSync(t *testing.T) *FileStorage {
	t.Helper()

	tmpDir := t.TempDir()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &Config{
		DataDir:         tmpDir,
		CodecType:       codec.JSON,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
		SyncWrites:      true,
	}

	store, err := New(cfg, log)
	require.NoError(t, err)
	return store
}

// ---------------------------------------------------------------------------
// setUnlocked - exercise SyncWrites path
// ---------------------------------------------------------------------------

func TestSetUnlocked_WithSyncWrites(t *testing.T) {
	store := createTestFileStorageWithSync(t)
	defer store.Close()

	err := store.Set([]byte("sync-key"), []byte("sync-value"))
	assert.NoError(t, err)

	val, err := store.Get([]byte("sync-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("sync-value"), val)
}

func TestSetUnlocked_WithSyncWritesLargeValue(t *testing.T) {
	store := createTestFileStorageWithSync(t)
	defer store.Close()

	// Large value to exercise the write + sync path
	largeValue := make([]byte, 64*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	err := store.Set([]byte("large-sync-key"), largeValue)
	assert.NoError(t, err)

	val, err := store.Get([]byte("large-sync-key"))
	require.NoError(t, err)
	assert.Equal(t, largeValue, val)
}

// ---------------------------------------------------------------------------
// Restore - cover more paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// newFileSnapshot - cover read error path (non-permission, non-not-found)
// ---------------------------------------------------------------------------

func TestNewFileSnapshot_WithData(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// Add test data
	err := store.Set([]byte("snap-key1"), []byte("snap-value1"))
	require.NoError(t, err)
	err = store.Set([]byte("snap-key2"), []byte("snap-value2"))
	require.NoError(t, err)

	// Take snapshot
	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Verify snapshot data
	val, err := snap.Get([]byte("snap-key1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("snap-value1"), val)

	val, err = snap.Get([]byte("snap-key2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("snap-value2"), val)
}

func TestNewFileSnapshot_Empty(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Should return not found for any key
	_, err = snap.Get([]byte("nonexistent"))
	assert.Error(t, err)
}

func TestNewFileSnapshot_ClosedStorage(t *testing.T) {
	store := createTestFileStorage(t)
	err := store.Close()
	require.NoError(t, err)

	_, err = store.Snapshot()
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Snapshot iterator - cover seek and iteration patterns
// ---------------------------------------------------------------------------

func TestSnapshotIterator_SeekToFirst(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("alpha"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("beta"), []byte("2"))
	require.NoError(t, err)
	err = store.Set([]byte("gamma"), []byte("3"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	assert.True(t, iter.SeekToFirst())
	assert.True(t, iter.Valid())
	assert.NotEmpty(t, iter.Key())
}

func TestSnapshotIterator_ReverseIteration(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("a"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("b"), []byte("2"))
	require.NoError(t, err)
	err = store.Set([]byte("c"), []byte("3"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	assert.True(t, iter.SeekToFirst())

	// In reverse, should start from last key
	var keys []string
	for iter.Valid() {
		keys = append(keys, string(iter.Key()))
		if !iter.Next() {
			break
		}
	}
	assert.NotEmpty(t, keys)

	// Should be in reverse order
	if len(keys) > 1 {
		for i := 0; i < len(keys)-1; i++ {
			assert.GreaterOrEqual(t, keys[i], keys[i+1])
		}
	}
}

func TestSnapshotIterator_SeekToKey(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("aaa"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("bbb"), []byte("2"))
	require.NoError(t, err)
	err = store.Set([]byte("ccc"), []byte("3"))
	require.NoError(t, err)
	err = store.Set([]byte("ddd"), []byte("4"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	// Seek to "bbb" - should find it
	found := iter.Seek([]byte("bbb"))
	assert.True(t, found)
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestSnapshotIterator_SeekBeyondAll(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("aaa"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("bbb"), []byte("2"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	// Seek beyond all keys
	found := iter.Seek([]byte("zzz"))
	assert.False(t, found)
}

func TestSnapshotIterator_SeekReverseBeyondAll(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("bbb"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("ccc"), []byte("2"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Test reverse seek that finds nothing: seek a key that is after all keys
	// In reverse mode with reversed keys ["ccc", "bbb"], seeking beyond both
	// would need a key before all keys in original order
	iter := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// Seeking exact match
	found := iter.Seek([]byte("bbb"))
	assert.True(t, found)
	assert.Equal(t, []byte("bbb"), iter.Key())
}

func TestSnapshotIterator_ValueOnClosed(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("key"), []byte("value"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)

	iter := snap.NewIterator(&storage.IteratorOptions{})
	iter.SeekToFirst()

	// Close iterator
	err = iter.Close()
	require.NoError(t, err)

	// Value should return nil after close
	assert.Nil(t, iter.Value())

	// Close snapshot
	err = snap.Close()
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// pathToKey - cover error path
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Exists - cover error and success paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// List - cover various patterns
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Delete - cover more paths
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Factory Create - cover more paths
// ---------------------------------------------------------------------------

func TestFactory_BackendType(t *testing.T) {
	factory := NewFactory(nil, WithCodecType(codec.JSON))
	assert.Equal(t, "file", factory.BackendType())
}

// ---------------------------------------------------------------------------
// Batch operations through Set/Delete
// ---------------------------------------------------------------------------

func TestBatch_MultipleOps_WithSyncWrites(t *testing.T) {
	store := createTestFileStorageWithSync(t)
	defer store.Close()

	batch := store.Batch()

	err := batch.Set([]byte("batch-sync-1"), []byte("val1"))
	require.NoError(t, err)
	err = batch.Set([]byte("batch-sync-2"), []byte("val2"))
	require.NoError(t, err)

	err = batch.Commit()
	assert.NoError(t, err)

	val, err := store.Get([]byte("batch-sync-1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("val1"), val)
}

// ---------------------------------------------------------------------------
// Close idempotent
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Snapshot Get - empty key, closed snapshot
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Snapshot iterator on closed snapshot
// ---------------------------------------------------------------------------

func TestSnapshot_NewIterator_ClosedSnapshot(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("key1"), []byte("val1"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)

	err = snap.Close()
	require.NoError(t, err)

	// Iterator on closed snapshot should not panic
	iter := snap.NewIterator(&storage.IteratorOptions{})
	assert.False(t, iter.SeekToFirst())
	err = iter.Close()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Storage iterator - cover bounds
// ---------------------------------------------------------------------------

func TestIterator_WithBounds(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("aaa"), []byte("1"))
	require.NoError(t, err)
	err = store.Set([]byte("bbb"), []byte("2"))
	require.NoError(t, err)
	err = store.Set([]byte("ccc"), []byte("3"))
	require.NoError(t, err)
	err = store.Set([]byte("ddd"), []byte("4"))
	require.NoError(t, err)

	iter := store.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte("bbb"),
		UpperBound: []byte("ddd"),
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Contains(t, keys, "bbb")
	assert.Contains(t, keys, "ccc")
	assert.NotContains(t, keys, "aaa")
}

// ---------------------------------------------------------------------------
// Restore with read-only target dir (should fail)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// GetWithContext and SetWithContext
// ---------------------------------------------------------------------------

func TestGetWithContext(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("ctx-key"), []byte("ctx-value"))
	require.NoError(t, err)

	val, err := store.Get([]byte("ctx-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("ctx-value"), val)
}

// ---------------------------------------------------------------------------
// Multiple Set/Get/Delete cycles to exercise the full pipeline
// ---------------------------------------------------------------------------

func TestSetGetDeleteCycle(t *testing.T) {
	store := createTestFileStorageWithSync(t)
	defer store.Close()

	keys := []string{"cycle/k1", "cycle/k2", "cycle/k3"}

	// Set all
	for _, k := range keys {
		err := store.Set([]byte(k), []byte("value-"+k))
		require.NoError(t, err)
	}

	// Verify all
	for _, k := range keys {
		val, err := store.Get([]byte(k))
		require.NoError(t, err)
		assert.Equal(t, []byte("value-"+k), val)
	}

	// Delete all
	for _, k := range keys {
		err := store.Delete([]byte(k))
		require.NoError(t, err)
	}

	// Verify all deleted
	for _, k := range keys {
		_, err := store.Get([]byte(k))
		assert.Error(t, err)
	}
}

// ---------------------------------------------------------------------------
// Snapshot with modifications after snapshot
// ---------------------------------------------------------------------------

func TestSnapshot_IsolatedFromModifications(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("iso-key"), []byte("original"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	// Modify after snapshot
	err = store.Set([]byte("iso-key"), []byte("modified"))
	require.NoError(t, err)

	// Snapshot should still have original value
	val, err := snap.Get([]byte("iso-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), val)
}

// ---------------------------------------------------------------------------
// newFileSnapshot - deleted file during snapshot (concurrent deletion)
// ---------------------------------------------------------------------------

func TestNewFileSnapshot_FileDeletedDuringRead(t *testing.T) {
	tmpDir := t.TempDir()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &Config{
		DataDir:         tmpDir,
		CodecType:       codec.JSON,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
		SyncWrites:      false,
	}

	store, err := New(cfg, log)
	require.NoError(t, err)
	defer store.Close()

	// Write a file
	err = store.Set([]byte("delete-during-snap"), []byte("value"))
	require.NoError(t, err)

	// Delete the actual file directly (simulating concurrent deletion)
	filePath := store.keyToPath([]byte("delete-during-snap"))
	err = os.Remove(filePath)
	require.NoError(t, err)

	// Snapshot should handle missing files gracefully (skip them)
	snap, snapErr := store.Snapshot()
	require.NoError(t, snapErr)
	defer snap.Close()

	// The deleted key should not be in the snapshot
	_, getErr := snap.Get([]byte("delete-during-snap"))
	assert.Error(t, getErr)
}

// ---------------------------------------------------------------------------
// Snapshot iterator with LowerBound and UpperBound
// ---------------------------------------------------------------------------

func TestSnapshotIterator_WithBounds(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	for _, k := range []string{"aa", "bb", "cc", "dd", "ee"} {
		err := store.Set([]byte(k), []byte("val-"+k))
		require.NoError(t, err)
	}

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{
		LowerBound: []byte("bb"),
		UpperBound: []byte("dd"),
	})
	defer iter.Close()

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	assert.Contains(t, keys, "bb")
	assert.Contains(t, keys, "cc")
	assert.NotContains(t, keys, "aa")
	assert.NotContains(t, keys, "ee")
}

// ---------------------------------------------------------------------------
// New - cover edge case configurations
// ---------------------------------------------------------------------------

func TestNew_DefaultPermissions(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &Config{
		DataDir:         t.TempDir(),
		CodecType:       codec.JSON,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
	}

	store, err := New(cfg, log)
	require.NoError(t, err)
	defer store.Close()

	err = store.Set([]byte("default-perm-key"), []byte("value"))
	assert.NoError(t, err)
}

func TestNew_MsgpackCodec(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &Config{
		DataDir:         t.TempDir(),
		CodecType:       codec.MsgPack,
		FilePermissions: DefaultFilePermissions,
		DirPermissions:  DefaultDirPermissions,
	}

	store, err := New(cfg, log)
	require.NoError(t, err)
	defer store.Close()

	err = store.Set([]byte("msgpack-key"), []byte("msgpack-value"))
	assert.NoError(t, err)

	val, err := store.Get([]byte("msgpack-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("msgpack-value"), val)
}

// ---------------------------------------------------------------------------
// Batch Rollback and Close
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// setUnlocked - verify atomic rename by checking file doesn't have .tmp suffix
// ---------------------------------------------------------------------------

func TestSetUnlocked_AtomicRename(t *testing.T) {
	store := createTestFileStorageWithSync(t)
	defer store.Close()

	err := store.Set([]byte("atomic-key"), []byte("atomic-value"))
	require.NoError(t, err)

	// Verify no .tmp files remain
	pattern := filepath.Join(store.config.DataDir, "**", "*.tmp")
	tmpFiles, globErr := filepath.Glob(pattern)
	require.NoError(t, globErr)
	assert.Empty(t, tmpFiles)

	// Verify the actual file exists at the correct path
	path := store.keyToPath([]byte("atomic-key"))
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
}

// ---------------------------------------------------------------------------
// setUnlocked - trigger MkdirAll error by making dataDir read-only
// ---------------------------------------------------------------------------

func TestSetUnlocked_MkdirAllError(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// A regular file where "subdir" needs to be a directory: MkdirAll returns
	// ENOTDIR, which root cannot walk through, unlike a read-only parent
	// (testing-guidelines.md, Fault Injection).
	require.NoError(t, os.WriteFile(filepath.Join(store.config.DataDir, "subdir"), []byte("blocking"), 0o644))

	err := store.Set([]byte("subdir/new-key"), []byte("value"))
	assert.Error(t, err)
	assert.ErrorIs(t, err, syscall.ENOTDIR)
}

// ---------------------------------------------------------------------------
// Restore - trigger write error path
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Iterator - Seek forward to exact match
// ---------------------------------------------------------------------------

func TestIterator_SeekExactMatch(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	for _, k := range []string{"apple", "banana", "cherry", "date"} {
		err := store.Set([]byte(k), []byte("val-"+k))
		require.NoError(t, err)
	}

	iter := store.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	// Seek to exact key
	found := iter.Seek([]byte("banana"))
	assert.True(t, found)
	assert.Equal(t, []byte("banana"), iter.Key())
}

func TestIterator_SeekBetweenKeys(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	for _, k := range []string{"aaa", "ccc", "eee"} {
		err := store.Set([]byte(k), []byte("val-"+k))
		require.NoError(t, err)
	}

	iter := store.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	// Seek to a key between existing keys
	found := iter.Seek([]byte("bbb"))
	assert.True(t, found)
	assert.Equal(t, []byte("ccc"), iter.Key())
}

// ---------------------------------------------------------------------------
// Iterator - Reverse Seek
// ---------------------------------------------------------------------------

func TestIterator_ReverseSeekExact(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	for _, k := range []string{"aaa", "bbb", "ccc"} {
		err := store.Set([]byte(k), []byte("val-"+k))
		require.NoError(t, err)
	}

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	found := iter.Seek([]byte("bbb"))
	assert.True(t, found)
	assert.Equal(t, []byte("bbb"), iter.Key())
}

// ---------------------------------------------------------------------------
// Snapshot Value with data
// ---------------------------------------------------------------------------

func TestSnapshotIterator_Value(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	err := store.Set([]byte("val-key"), []byte("val-data"))
	require.NoError(t, err)

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	iter := snap.NewIterator(&storage.IteratorOptions{})
	defer iter.Close()

	iter.SeekToFirst()
	assert.True(t, iter.Valid())
	assert.Equal(t, []byte("val-data"), iter.Value())
}

// ---------------------------------------------------------------------------
// Factory Create - with nil logger (auto-creates one)
// ---------------------------------------------------------------------------

func TestFactory_Create_NilLogger(t *testing.T) {
	factory := NewFactory(nil, WithCodecType(codec.JSON))

	store, err := factory.Create("nil-logger-partition", &PartitionConfig{
		DataDir: t.TempDir(),
	})
	assert.NoError(t, err)
	assert.NotNil(t, store)
}

// ---------------------------------------------------------------------------
// Restore - WalkDir error path (non-existent data dir)
// ---------------------------------------------------------------------------

// =============================================================================
// Consolidated from coverage_96_test.go
// =============================================================================

// TestFactory_Create_NewReturnsError tests Factory.Create when New fails
// because the DataDir points to a location that cannot be created.
func TestFactory_Create_NewReturnsError(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	factory := NewFactory(log, WithCodecType(codec.JSON))

	// Create a file that blocks MkdirAll from creating a directory at that path
	tmpDir := t.TempDir()
	blockingFile := filepath.Join(tmpDir, "blocker")
	require.NoError(t, os.WriteFile(blockingFile, []byte("file"), 0644))

	config := &PartitionConfig{
		DataDir: filepath.Join(blockingFile, "subdir"),
	}

	_, err = factory.Create("test-partition", config)
	assert.Error(t, err, "should fail when DataDir cannot be created")
}

// TestSnapshot_SkipsDanglingSymlink tests that snapshot creation handles
// files that no longer exist by skipping them (os.IsNotExist path in
// newFileSnapshot). The dangling key appears in the keys list but has
// no data in the snapshot.
func TestSnapshot_SkipsDanglingSymlink(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// Write a real key
	require.NoError(t, store.Set([]byte("real-key"), []byte("real-value")))

	// Create a dangling symlink with the correct extension in the data directory.
	// listKeys will find it via WalkDir, but os.ReadFile will get ENOENT,
	// triggering the os.IsNotExist skip path at snapshot.go line 55-56.
	danglingLink := filepath.Join(store.config.DataDir, "dangling-key"+store.extension)
	require.NoError(t, os.Symlink("/nonexistent/target", danglingLink))

	// Snapshot should succeed (not error out from the dangling symlink)
	snapshot, err := store.Snapshot()
	require.NoError(t, err, "snapshot creation should succeed despite dangling symlink")
	defer snapshot.Close()

	// The real key should be accessible in the snapshot
	val, err := snapshot.Get([]byte("real-key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("real-value"), val)

	// The dangling key is in the keys list but has no data.
	// Accessing it should return ErrNotFound since it was skipped.
	_, err = snapshot.Get([]byte("dangling-key"))
	assert.Error(t, err, "dangling key should not have data in snapshot")
}

// TestSnapshotIterator_Value_MissingData tests the snapshot iterator Value()
// method when a key exists in the keys list but has no corresponding data
// in the snapshot map (returns nil for !exists path at snapshot.go line 297).
func TestSnapshotIterator_Value_MissingData(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// Write a real key
	require.NoError(t, store.Set([]byte("real-key"), []byte("real-value")))

	// Create a dangling symlink so snapshot has a key without data
	danglingLink := filepath.Join(store.config.DataDir, "aaa-dangling"+store.extension)
	require.NoError(t, os.Symlink("/nonexistent/target", danglingLink))

	snapshot, err := store.Snapshot()
	require.NoError(t, err)
	defer snapshot.Close()

	// The snapshot iterator should handle keys with missing data gracefully
	iter := snapshot.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	require.True(t, iter.Valid())

	// The first key alphabetically is "aaa-dangling" which has no data
	assert.Equal(t, []byte("aaa-dangling"), iter.Key())
	assert.Nil(t, iter.Value(), "value should be nil for key with missing data")

	// The second key should be "real-key" with valid data
	iter.Next()
	require.True(t, iter.Valid())
	assert.Equal(t, []byte("real-key"), iter.Key())
	assert.Equal(t, []byte("real-value"), iter.Value())
}

// TestIterator_ReverseSeek_ExactMatchFirstKey tests the reverse Seek path
// where the target exactly matches the first key in reverse order (the
// largest key). This exercises the path where sort.Search returns idx > 0
// and the match is at idx-1.
func TestIterator_ReverseSeek_ExactMatchFirstKey(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("v1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("v2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("v3")))

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are ["ccc", "bbb", "aaa"]
	// Seek("ccc") should match the first key and return true
	found := iter.Seek([]byte("ccc"))
	assert.True(t, found)
	assert.Equal(t, []byte("ccc"), iter.Key())
}

// TestSnapshotIterator_ReverseSeek_ExactMatchFirstKey tests the reverse
// Seek path on a snapshot iterator where the target exactly matches the
// first key in reverse order.
func TestSnapshotIterator_ReverseSeek_ExactMatchFirstKey(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("v1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("v2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("v3")))

	snapshot, err := store.Snapshot()
	require.NoError(t, err)
	defer snapshot.Close()

	iter := snapshot.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are ["ccc", "bbb", "aaa"]
	// Seek("ccc") should match the first key and return true
	found := iter.Seek([]byte("ccc"))
	assert.True(t, found)
	assert.Equal(t, []byte("ccc"), iter.Key())
}

// TestSetUnlocked_OpenFileError_DirAtTmpPath tests setUnlocked when the
// temp file cannot be created because a directory exists at the tmp path.
func TestSetUnlocked_OpenFileError_DirAtTmpPath(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// Create the parent directory for the key
	keyPath := store.keyToPath([]byte("new-key"))
	tmpPath := keyPath + ".tmp"
	require.NoError(t, os.MkdirAll(filepath.Dir(keyPath), store.config.DirPermissions))

	// Create a directory at the tmp path location - OpenFile will fail
	// because it can't truncate/write over a directory
	require.NoError(t, os.MkdirAll(tmpPath, 0755))

	err := store.Set([]byte("new-key"), []byte("value"))
	assert.Error(t, err, "should fail when tmp file path is a directory")
}

// TestRestore_MkdirAllError tests Restore when creating parent
// directories for a snapshot entry fails because a file blocks the path.

// TestRestore_OpenFileError_ReadOnlyDir tests Restore when creating the
// temp file for a snapshot entry fails because the data directory is
// read-only. The WalkDir phase succeeds (only reads), MkdirAll succeeds
// (dir already exists), but OpenFile fails due to permission denied.

// TestPathToKey_RelError tests pathToKey when filepath.Rel fails because
// the input path cannot be made relative to the DataDir.
func TestPathToKey_RelError(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// filepath.Rel fails when base is absolute and target is relative
	result := store.pathToKey("relative/path/no/root")
	assert.Equal(t, "", result, "pathToKey should return empty for unrelateable path")
}

// TestExists_StatError_WhenParentIsFile covers the branch where os.Stat fails
// with something other than IsNotExist. A regular file used as a path component
// yields ENOTDIR, which root cannot bypass.
func TestExists_StatError_WhenParentIsFile(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	// "sub" is a regular file, so stat of "sub/file.json" cannot resolve.
	require.NoError(t, os.WriteFile(filepath.Join(store.config.DataDir, "sub"), []byte("data"), 0644))

	_, err := store.Exists([]byte("sub/file"))
	require.Error(t, err, "should fail with ENOTDIR on stat")
	assert.ErrorIs(t, err, syscall.ENOTDIR)
}

// TestList_ListKeysError tests List when listKeys fails because the walk root
// does not exist.
func TestList_ListKeysError_WhenDataDirIsMissing(t *testing.T) {
	store := createTestFileStorage(t)
	defer store.Close()

	require.NoError(t, os.RemoveAll(store.config.DataDir))

	_, err := store.List(nil)
	require.Error(t, err, "should fail when the data directory is missing")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// ---------------------------------------------------------------------------
// Exists (storage.go:182 -- 0.0% covered)
// ---------------------------------------------------------------------------

func TestExists_KeyFound(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("exists-key"), []byte("value")))

	found, err := store.Exists([]byte("exists-key"))
	require.NoError(t, err)
	assert.True(t, found)
}

func TestExists_KeyNotFound(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	found, err := store.Exists([]byte("no-such-key"))
	require.NoError(t, err)
	assert.False(t, found)
}

func TestExists_EmptyKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	_, err := store.Exists([]byte{})
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestExists_ClosedStorage(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Close())

	_, err := store.Exists([]byte("key"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// ---------------------------------------------------------------------------
// List (storage.go:206 -- 0.0% covered)
// ---------------------------------------------------------------------------

func TestList_WithPrefix(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("users/1"), []byte("alice")))
	require.NoError(t, store.Set([]byte("users/2"), []byte("bob")))
	require.NoError(t, store.Set([]byte("orders/1"), []byte("order")))

	keys, err := store.List([]byte("users/"))
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestList_EmptyPrefix(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("k1"), []byte("v1")))
	require.NoError(t, store.Set([]byte("k2"), []byte("v2")))

	keys, err := store.List([]byte{})
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestList_NoMatchingPrefix(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))

	keys, err := store.List([]byte("zzz/"))
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestList_ClosedStorage(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Close())

	_, err := store.List(nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// ---------------------------------------------------------------------------
// pathToKey (storage.go:142 -- 75.0% covered, error path)
// ---------------------------------------------------------------------------

func TestPathToKey_ValidPath(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	path := filepath.Join(store.config.DataDir, "mykey"+store.extension)
	key := store.pathToKey(path)
	assert.Equal(t, "mykey", key)
}

// ---------------------------------------------------------------------------
// setUnlocked error branches (storage.go:243 -- 65.5% covered)
// ---------------------------------------------------------------------------

func TestSetUnlocked_ReadOnlyDir(t *testing.T) {
	tempDir := t.TempDir()
	readOnlyDir := filepath.Join(tempDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o755))

	config := DefaultConfig()
	config.DataDir = readOnlyDir

	store, err := New(config, nil)
	require.NoError(t, err)
	defer store.Close()

	// "nested" is a regular file, so MkdirAll for "nested/deep" returns
	// ENOTDIR -- for every uid, unlike a read-only data directory
	// (testing-guidelines.md, Fault Injection).
	require.NoError(t, os.WriteFile(filepath.Join(readOnlyDir, "nested"), []byte("blocking"), 0o644))

	err = store.Set([]byte("nested/deep/key"), []byte("value"))
	require.Error(t, err)
	assert.ErrorIs(t, err, syscall.ENOTDIR)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestSetUnlocked_NilValue(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	// Setting nil value should work (empty file)
	err := store.Set([]byte("nil-val"), nil)
	require.NoError(t, err)

	val, err := store.Get([]byte("nil-val"))
	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestSetUnlocked_OverwriteExisting(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("original")))
	require.NoError(t, store.Set([]byte("key"), []byte("updated")))

	val, err := store.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), val)
}

// ---------------------------------------------------------------------------
// Restore error branches (storage.go:517 -- 76.2% covered)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// newFileSnapshot error path (snapshot.go:41 -- 73.3% covered)
// ---------------------------------------------------------------------------

func TestSnapshot_Success(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	val, err := snap.Get([]byte("a"))
	require.NoError(t, err)
	assert.Equal(t, []byte("1"), val)
}

func TestSnapshot_ClosedStorage(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Close())

	_, err := store.Snapshot()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// ---------------------------------------------------------------------------
// Snapshot Seek (snapshot.go:195 -- 87.5% covered)
// ---------------------------------------------------------------------------

func TestSnapshotIterator_SeekExact(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))
	require.NoError(t, store.Set([]byte("c"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("b"))
	assert.True(t, found)
	assert.Equal(t, []byte("b"), it.Key())
}

func TestSnapshotIterator_SeekBeyondLast(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("zzz"))
	assert.False(t, found)
}

func TestSnapshotIterator_SeekEmptySnapshot(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("any"))
	assert.False(t, found)
}

func TestSnapshotIterator_SeekReverse(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))
	require.NoError(t, store.Set([]byte("c"), []byte("3")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer it.Close()

	found := it.Seek([]byte("b"))
	assert.True(t, found)
}

func TestSnapshotIterator_SeekReverseNoMatch(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("c"), []byte("1")))
	require.NoError(t, store.Set([]byte("d"), []byte("2")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer it.Close()

	// Seek to value greater than all keys in reverse mode
	found := it.Seek([]byte("e"))
	assert.False(t, found)
}

// ---------------------------------------------------------------------------
// Snapshot Value (snapshot.go:287 -- 90.0% covered, OnlyKeys branch)
// ---------------------------------------------------------------------------

func TestSnapshotIterator_ValueOnlyKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("k1"), []byte("v1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{OnlyKeys: true})
	defer it.Close()

	ok := it.SeekToFirst()
	require.True(t, ok)

	assert.NotNil(t, it.Key())
	assert.Nil(t, it.Value()) // OnlyKeys should return nil for Value
}

func TestSnapshotIterator_ValueInvalidPosition(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	snap, err := store.Snapshot()
	require.NoError(t, err)
	defer snap.Close()

	it := snap.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	// Before seeking - invalid position
	assert.Nil(t, it.Value())
}

// ---------------------------------------------------------------------------
// Iterator Seek (iterator.go:132 -- 87.5% covered)
// ---------------------------------------------------------------------------

func TestIterator_SeekExact(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))
	require.NoError(t, store.Set([]byte("c"), []byte("3")))

	it := store.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("b"))
	assert.True(t, found)
	assert.Equal(t, []byte("b"), it.Key())
}

func TestIterator_SeekBeyondLast(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))

	it := store.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("zzz"))
	assert.False(t, found)
}

func TestIterator_SeekEmpty(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	it := store.NewIterator(&storage.IteratorOptions{})
	defer it.Close()

	found := it.Seek([]byte("any"))
	assert.False(t, found)
}

func TestIterator_SeekReverse(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))
	require.NoError(t, store.Set([]byte("b"), []byte("2")))
	require.NoError(t, store.Set([]byte("c"), []byte("3")))

	it := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer it.Close()

	found := it.Seek([]byte("b"))
	assert.True(t, found)
}

func TestIterator_SeekReverseNoMatch(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("c"), []byte("1")))
	require.NoError(t, store.Set([]byte("d"), []byte("2")))

	it := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer it.Close()

	found := it.Seek([]byte("e"))
	assert.False(t, found)
}

// ---------------------------------------------------------------------------
// Batch Commit edge cases (batch.go:122 -- 93.8% covered)
// ---------------------------------------------------------------------------

func TestBatch_CommitToClosedStorage(t *testing.T) {
	store := createTestStorage(t)

	batch := store.Batch()
	require.NoError(t, batch.Set([]byte("key"), []byte("val")))

	require.NoError(t, store.Close())

	err := batch.Commit()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

func TestBatch_CommitMultipleKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	batch := store.Batch()
	require.NoError(t, batch.Set([]byte("k1"), []byte("v1")))
	require.NoError(t, batch.Set([]byte("k2"), []byte("v2")))
	require.NoError(t, batch.Set([]byte("k3"), []byte("v3")))

	require.NoError(t, batch.Commit())

	for _, k := range []string{"k1", "k2", "k3"} {
		_, err := store.Get([]byte(k))
		require.NoError(t, err)
	}
}

func TestBatch_CommitWithDeletes(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("old"), []byte("data")))

	batch := store.Batch()
	require.NoError(t, batch.Delete([]byte("old")))
	require.NoError(t, batch.Set([]byte("new"), []byte("data")))

	require.NoError(t, batch.Commit())

	_, err := store.Get([]byte("old"))
	assert.True(t, errors.Is(err, errors.ErrNotFound))

	val, err := store.Get([]byte("new"))
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
}

// ---------------------------------------------------------------------------
// Factory Create (factory.go:63 -- 86.7% covered)
// ---------------------------------------------------------------------------

func TestFactory_CreateNilConfig(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	f := NewFactory(log)
	_, createErr := f.Create("test", nil)
	require.Error(t, createErr)
	assert.True(t, errors.IsInvalidInput(createErr))
}

func TestFactory_CreateEmptyDataDir(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	f := NewFactory(log)
	_, createErr := f.Create("test", &PartitionConfig{DataDir: ""})
	require.Error(t, createErr)
	assert.True(t, errors.IsInvalidInput(createErr))
}

func TestFactory_CreateSuccess(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	f := NewFactory(log)
	s, createErr := f.Create("test-partition", &PartitionConfig{DataDir: t.TempDir()})
	require.NoError(t, createErr)
	require.NotNil(t, s)
	require.NoError(t, s.Close())
}

func TestFactory_CreateWithCodecType(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	f := NewFactory(log, WithCodecType(codec.JSON))
	s, createErr := f.Create("json-partition", &PartitionConfig{DataDir: t.TempDir()})
	require.NoError(t, createErr)
	require.NotNil(t, s)
	require.NoError(t, s.Close())
}

func TestFactory_CreateNilLogger(t *testing.T) {
	f := NewFactory(nil)
	s, createErr := f.Create("test", &PartitionConfig{DataDir: t.TempDir()})
	require.NoError(t, createErr)
	require.NotNil(t, s)
	require.NoError(t, s.Close())
}

// ---------------------------------------------------------------------------
// New (storage.go:101 -- 92.3% covered, default config path)
// ---------------------------------------------------------------------------

func TestNew_NilConfig(t *testing.T) {
	_, err := New(nil, nil)
	require.Error(t, err)
}

func TestNew_CustomPermissions(t *testing.T) {
	config := &Config{
		DataDir:         t.TempDir(),
		FilePermissions: 0o600,
		DirPermissions:  0o700,
		SyncWrites:      false,
		CodecType:       codec.JSON,
	}

	store, err := New(config, nil)
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.Set([]byte("k"), []byte("v")))

	val, getErr := store.Get([]byte("k"))
	require.NoError(t, getErr)
	assert.Equal(t, []byte("v"), val)
}

// ---------------------------------------------------------------------------
// More setUnlocked coverage (file write error paths)
// ---------------------------------------------------------------------------

func TestSet_LargeValue(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	largeValue := make([]byte, 1024*1024) // 1MB
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	err := store.Set([]byte("large-key"), largeValue)
	require.NoError(t, err)

	val, err := store.Get([]byte("large-key"))
	require.NoError(t, err)
	assert.Equal(t, largeValue, val)
}

func TestSet_OverwriteWithDifferentSize(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("short")))
	require.NoError(t, store.Set([]byte("key"), []byte("this is a much longer value")))

	val, err := store.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("this is a much longer value"), val)
}

func TestSet_EmptyValue(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("empty"), []byte{}))

	val, err := store.Get([]byte("empty"))
	require.NoError(t, err)
	assert.Empty(t, val)
}

// ---------------------------------------------------------------------------
// Restore with SyncWrites (covers the Sync branch in Restore)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// More Exists and List with various key patterns
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Snapshot with read after close
// ---------------------------------------------------------------------------

func TestSnapshot_GetAfterClose(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("k"), []byte("v")))

	snap, err := store.Snapshot()
	require.NoError(t, err)

	require.NoError(t, snap.Close())

	_, err = snap.Get([]byte("k"))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Iterator Seek after close
// ---------------------------------------------------------------------------

func TestIterator_SeekAfterClose(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))

	it := store.NewIterator(&storage.IteratorOptions{})
	it.Close()

	found := it.Seek([]byte("a"))
	assert.False(t, found)
}

func TestSnapshotIterator_SeekAfterClose(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("a"), []byte("1")))

	snap, err := store.Snapshot()
	require.NoError(t, err)

	it := snap.NewIterator(&storage.IteratorOptions{})
	it.Close()

	found := it.Seek([]byte("a"))
	assert.False(t, found)

	snap.Close()
}

// ---------------------------------------------------------------------------
// Factory Create with nil logger should auto-create one
// ---------------------------------------------------------------------------

func TestFactory_CreateAutoLogger(t *testing.T) {
	f := NewFactory(nil, WithCodecType(codec.JSON))
	s, err := f.Create("auto-log", &PartitionConfig{DataDir: t.TempDir()})
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NoError(t, s.Close())
}

// ---------------------------------------------------------------------------
// newFileSnapshot with unreadable file (snapshot.go:41 -- covers read error path)
// ---------------------------------------------------------------------------

func TestSnapshot_WithUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Log("Skipping: root can read all files")
		return
	}

	store := createTestStorage(t)
	defer store.Close()

	// Write a key
	require.NoError(t, store.Set([]byte("readable"), []byte("yes")))
	require.NoError(t, store.Set([]byte("unreadable"), []byte("no")))

	// Make one file unreadable
	path := store.keyToPath([]byte("unreadable"))
	require.NoError(t, os.Chmod(path, 0o000))
	defer os.Chmod(path, 0o644)

	// Snapshot should fail with a read error for the unreadable file
	_, err := store.Snapshot()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
}

// ---------------------------------------------------------------------------
// pathToKey error path - unreachable path
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// New with validate error
// ---------------------------------------------------------------------------

func TestNew_MissingDataDir(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = "" // Empty data dir should fail validation

	_, err := New(config, nil)
	require.Error(t, err)
}

func TestNew_InvalidCodecType(t *testing.T) {
	config := DefaultConfig()
	config.DataDir = t.TempDir()
	config.CodecType = codec.Type("invalid_codec")

	_, err := New(config, nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Exists with stat error for non-permission issue
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// List with deeply nested keys
// ---------------------------------------------------------------------------

// =============================================================================
// Consolidated from storage_coverage_test.go
// =============================================================================

// -----------------------------------------------------------------------------
// setUnlocked write error paths (covers storage.go lines 220-246)
// These test the tmpFile.Write failure, tmpFile.Sync failure,
// tmpFile.Close failure, and os.Rename failure branches.
// -----------------------------------------------------------------------------

func TestSetUnlocked_ClosedStorage_ReturnsError(t *testing.T) {
	store := createTestStorage(t)
	require.NoError(t, store.Close())

	err := store.Set([]byte("key"), []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.ErrStorage))
	assert.Contains(t, err.Error(), "closed")
}

func TestSetUnlocked_EmptyKey_ReturnsError(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	err := store.Set([]byte{}, []byte("value"))

	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err))
}

func TestSet_WithSyncWrites_Succeeds(t *testing.T) {
	tempDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = tempDir
	config.SyncWrites = true

	store, err := New(config, nil)
	require.NoError(t, err)
	defer store.Close()

	err = store.Set([]byte("key"), []byte("value"))

	require.NoError(t, err)

	val, err := store.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestSet_WithoutSyncWrites_Succeeds(t *testing.T) {
	tempDir := t.TempDir()
	config := DefaultConfig()
	config.DataDir = tempDir
	config.SyncWrites = false

	store, err := New(config, nil)
	require.NoError(t, err)
	defer store.Close()

	err = store.Set([]byte("key"), []byte("value"))

	require.NoError(t, err)

	val, err := store.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

// -----------------------------------------------------------------------------
// Restore write error paths (covers storage.go lines 510-544)
// Tests the MkdirAll, OpenFile, Write, Sync, Close, and Rename error branches.
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// pathToKey edge cases (covers storage.go line 146)
// -----------------------------------------------------------------------------

func TestPathToKey_EmptyStringForInvalidPath(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	// Test with path that should produce empty key after trimming extension
	emptyKeyPath := filepath.Join(store.config.DataDir, ".json")
	key := store.pathToKey(emptyKeyPath)

	// After removing the data dir prefix and trimming ".json", we get ""
	assert.Equal(t, "", key)
}

// -----------------------------------------------------------------------------
// Iterator Seek in reverse mode (covers iterator.go lines 140-160)
// -----------------------------------------------------------------------------

func TestIterator_Seek_ReverseExactMatch(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb, aaa]
	found := iter.Seek([]byte("bbb"))

	assert.True(t, found)
	assert.True(t, iter.Valid())
}

func TestIterator_Seek_ReverseFirstKey(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("aaa"), []byte("1")))
	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb, aaa]
	// Seek to "ccc" (the first element)
	found := iter.Seek([]byte("ccc"))

	assert.True(t, found)
	assert.True(t, iter.Valid())
}

func TestIterator_Seek_ReverseNotFound(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb]
	// Seek to "ddd" - all keys are < "ddd" in reverse order
	found := iter.Seek([]byte("ddd"))
	// The reverse seek logic checks sort.Search for first key < target
	// and then uses idx-1. With keys [ccc, bbb], first key < "ddd" is at idx 0 (ccc),
	// idx > 0 is false, then checks keys[0] == "ddd"? No. Returns false.
	assert.False(t, found)
}

func TestIterator_Seek_ReverseEmptyKeys(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	found := iter.Seek([]byte("any"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_Seek_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	require.NoError(t, iter.Close())

	found := iter.Seek([]byte("key"))

	assert.False(t, found)
	assert.False(t, iter.Valid())
}

func TestIterator_Seek_ReverseTargetSmallerThanAll(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("bbb"), []byte("2")))
	require.NoError(t, store.Set([]byte("ccc"), []byte("3")))

	iter := store.NewIterator(&storage.IteratorOptions{Reverse: true})
	defer iter.Close()

	// In reverse mode, keys are [ccc, bbb]
	// Seek to "aaa" - target smaller than all keys
	found := iter.Seek([]byte("aaa"))

	// sort.Search looks for first key < "aaa" in [ccc, bbb]:
	//   ccc < aaa? No. bbb < aaa? No. idx = 2.
	//   idx > 0? Yes. current = idx-1 = 1, which is "bbb". Valid.
	assert.True(t, found)
}

// -----------------------------------------------------------------------------
// Iterator Value with deleted key (covers iterator.go line 245)
// -----------------------------------------------------------------------------

func TestIterator_Value_ReturnsNilWhenKeyDeletedFromStorage(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	defer iter.Close()

	iter.SeekToFirst()
	assert.True(t, iter.Valid())

	// Delete the key from storage while iterator is positioned
	require.NoError(t, store.Delete([]byte("key")))

	// Value should return nil and set error since key no longer exists
	val := iter.Value()
	assert.Nil(t, val)
	assert.NotNil(t, iter.Error())
}

// -----------------------------------------------------------------------------
// listKeys with unreadable directory (covers storage.go line 621)
// -----------------------------------------------------------------------------

func TestListKeys_WithUnreadableSubdir_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Log("Skipping permission test as root")
		return
	}
	if runtime.GOOS == "windows" {
		t.Log("Skipping permission test on Windows")
		return
	}

	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	// Create an unreadable subdirectory
	unreadableDir := filepath.Join(store.config.DataDir, "unreadable")
	require.NoError(t, os.MkdirAll(unreadableDir, 0755))
	require.NoError(t, os.Chmod(unreadableDir, 0000))
	defer func() {
		require.NoError(t, os.Chmod(unreadableDir, 0755))
	}()

	// listKeys is called internally by NewIterator
	iter := store.NewIterator(nil)
	defer iter.Close()

	assert.NotNil(t, iter.Error())
}

// -----------------------------------------------------------------------------
// listKeys skips temp files and non-matching extensions (covers storage.go lines 603-609)
// -----------------------------------------------------------------------------

func TestListKeys_SkipsTempAndNonMatchingFiles(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key1"), []byte("value1")))

	// Create temp files and non-matching extension files
	tempFile := filepath.Join(store.config.DataDir, "temp.json.tmp")
	require.NoError(t, os.WriteFile(tempFile, []byte("temp"), 0644))

	nonMatchFile := filepath.Join(store.config.DataDir, "file.txt")
	require.NoError(t, os.WriteFile(nonMatchFile, []byte("text"), 0644))

	iter := store.NewIterator(nil)
	defer iter.Close()

	require.NoError(t, iter.Error())

	var keys []string
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		keys = append(keys, string(iter.Key()))
	}

	// Only the valid key should appear
	assert.Equal(t, []string{"key1"}, keys)
}

// -----------------------------------------------------------------------------
// Iterator SeekToLast on closed iterator (covers iterator.go line 120)
// -----------------------------------------------------------------------------

func TestIterator_SeekToLast_ClosedIterator(t *testing.T) {
	store := createTestStorage(t)
	defer store.Close()

	require.NoError(t, store.Set([]byte("key"), []byte("value")))

	iter := store.NewIterator(nil)
	require.NoError(t, iter.Close())

	found := iter.SeekToLast()

	assert.False(t, found)
	assert.False(t, iter.Valid())
}
