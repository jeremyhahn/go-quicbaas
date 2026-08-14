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

package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestUnsupportedAlgorithmError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &UnsupportedAlgorithmError{Algorithm: "RSA-4096"}
		assert.Equal(t, "unsupported algorithm: RSA-4096", err.Error())
	})

	t.Run("EmptyAlgorithm", func(t *testing.T) {
		err := &UnsupportedAlgorithmError{Algorithm: ""}
		assert.Equal(t, "unsupported algorithm: ", err.Error())
	})
}

func TestNewUnsupportedAlgorithmError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewUnsupportedAlgorithmError("test.op", "CHACHA20")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
		assert.Equal(t, "test.op", de.Op)
		var inner *UnsupportedAlgorithmError
		assert.True(t, errors.As(de.Err, &inner))
		assert.Equal(t, "CHACHA20", inner.Algorithm)
	})

	t.Run("ErrorStringContainsAlgorithm", func(t *testing.T) {
		de := NewUnsupportedAlgorithmError("crypto.encrypt", "BLOWFISH")
		assert.Contains(t, de.Error(), "BLOWFISH")
	})
}

func TestInvalidDayFormatError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &InvalidDayFormatError{Value: "abc"}
		assert.Equal(t, "invalid day format: abc", err.Error())
	})

	t.Run("EmptyValue", func(t *testing.T) {
		err := &InvalidDayFormatError{Value: ""}
		assert.Equal(t, "invalid day format: ", err.Error())
	})
}

func TestNewInvalidDayFormatError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewInvalidDayFormatError("parse.duration", "xyz")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidInput", func(t *testing.T) {
		de := NewInvalidDayFormatError("op", "notADay")
		assert.Contains(t, de.Error(), "notADay")
	})
}

func TestNegativeDurationError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &NegativeDurationError{Value: "-5h"}
		assert.Equal(t, "grace period cannot be negative: -5h", err.Error())
	})

	t.Run("EmptyValue", func(t *testing.T) {
		err := &NegativeDurationError{Value: ""}
		assert.Equal(t, "grace period cannot be negative: ", err.Error())
	})
}

func TestNewNegativeDurationError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewNegativeDurationError("op", "-10s")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidDuration", func(t *testing.T) {
		de := NewNegativeDurationError("test", "-3m")
		assert.Contains(t, de.Error(), "-3m")
	})
}

func TestInvalidDurationFormatError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &InvalidDurationFormatError{Value: "not-a-duration"}
		assert.Equal(t, "invalid duration format: not-a-duration", err.Error())
	})

	t.Run("EmptyValue", func(t *testing.T) {
		err := &InvalidDurationFormatError{Value: ""}
		assert.Equal(t, "invalid duration format: ", err.Error())
	})
}

func TestNewInvalidDurationFormatError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewInvalidDurationFormatError("op", "xyz")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		de := NewInvalidDurationFormatError("test", "abc123")
		assert.Contains(t, de.Error(), "abc123")
	})
}

func TestUnsupportedOutputFormatError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &UnsupportedOutputFormatError{Format: "xml"}
		assert.Equal(t, "unsupported output format: xml", err.Error())
	})

	t.Run("EmptyFormat", func(t *testing.T) {
		err := &UnsupportedOutputFormatError{Format: ""}
		assert.Equal(t, "unsupported output format: ", err.Error())
	})
}

func TestNewUnsupportedOutputFormatError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewUnsupportedOutputFormatError("display", "yaml")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		de := NewUnsupportedOutputFormatError("test", "binary")
		assert.Contains(t, de.Error(), "binary")
	})
}

func TestNoActiveMigrationError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &NoActiveMigrationError{}
		assert.Equal(t, "no active migration found", err.Error())
	})
}

func TestNewNoActiveMigrationError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewNoActiveMigrationError("migrate.status")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrNotFound, de.Code)
		assert.Equal(t, "migrate.status", de.Op)
	})

	t.Run("ErrorStringContainsMessage", func(t *testing.T) {
		de := NewNoActiveMigrationError("test")
		assert.Contains(t, de.Error(), "no active migration found")
	})
}

func TestInvalidStorageURLError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &InvalidStorageURLError{URL: "bad://url"}
		assert.Contains(t, err.Error(), "bad://url")
		assert.Contains(t, err.Error(), "expected scheme://path")
	})

	t.Run("EmptyURL", func(t *testing.T) {
		err := &InvalidStorageURLError{URL: ""}
		assert.Contains(t, err.Error(), "invalid storage URL format")
	})
}

func TestNewInvalidStorageURLError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewInvalidStorageURLError("storage.parse", "invalid-url")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidURL", func(t *testing.T) {
		de := NewInvalidStorageURLError("test", "ftp://bad")
		assert.Contains(t, de.Error(), "ftp://bad")
	})
}

func TestUnsupportedStorageSchemeError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &UnsupportedStorageSchemeError{Scheme: "ftp"}
		assert.Contains(t, err.Error(), "ftp")
		assert.Contains(t, err.Error(), "supported: pebble, memory, file")
	})

	t.Run("EmptyScheme", func(t *testing.T) {
		err := &UnsupportedStorageSchemeError{Scheme: ""}
		assert.Contains(t, err.Error(), "unsupported storage scheme")
	})
}

func TestNewUnsupportedStorageSchemeError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewUnsupportedStorageSchemeError("storage", "nfs")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidScheme", func(t *testing.T) {
		de := NewUnsupportedStorageSchemeError("test", "s3")
		assert.Contains(t, de.Error(), "s3")
	})
}

func TestMigrationPartialFailureError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &MigrationPartialFailureError{FailedKeys: 42}
		assert.Equal(t, "migration completed with 42 failed keys", err.Error())
	})

	t.Run("ZeroFailedKeys", func(t *testing.T) {
		err := &MigrationPartialFailureError{FailedKeys: 0}
		assert.Equal(t, "migration completed with 0 failed keys", err.Error())
	})
}

func TestNewMigrationPartialFailureError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewMigrationPartialFailureError("migrate.run", 100)
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInternal, de.Code)
	})

	t.Run("FailedKeysInMessage", func(t *testing.T) {
		de := NewMigrationPartialFailureError("test", 55)
		assert.Contains(t, de.Error(), "55")
	})
}

func TestKeyNotFoundError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &KeyNotFoundError{}
		assert.Equal(t, "key not found", err.Error())
	})
}

func TestNewKeyNotFoundError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewKeyNotFoundError("data.get")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrNotFound, de.Code)
		assert.Equal(t, "data.get", de.Op)
	})

	t.Run("ErrorStringContainsMessage", func(t *testing.T) {
		de := NewKeyNotFoundError("test")
		assert.Contains(t, de.Error(), "key not found")
	})
}

func TestInvalidFilterFormatError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &InvalidFilterFormatError{Filter: "badfilter"}
		assert.Contains(t, err.Error(), "badfilter")
		assert.Contains(t, err.Error(), "expected key=value")
	})

	t.Run("EmptyFilter", func(t *testing.T) {
		err := &InvalidFilterFormatError{Filter: ""}
		assert.Contains(t, err.Error(), "invalid filter format")
	})
}

func TestNewInvalidFilterFormatError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewInvalidFilterFormatError("vector.search", "no-equals")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("InvalidFilter", func(t *testing.T) {
		de := NewInvalidFilterFormatError("test", "bad")
		assert.Contains(t, de.Error(), "bad")
	})
}

func TestNoMetadataProvidedError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &NoMetadataProvidedError{}
		assert.Contains(t, err.Error(), "no metadata provided")
		assert.Contains(t, err.Error(), "--metadata")
	})
}

func TestNewNoMetadataProvidedError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewNoMetadataProvidedError("vector.update")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("ErrorStringContainsMessage", func(t *testing.T) {
		de := NewNoMetadataProvidedError("test")
		assert.Contains(t, de.Error(), "no metadata provided")
	})
}

func TestFilterRequiredError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		err := &FilterRequiredError{}
		assert.Equal(t, "at least one --filter is required", err.Error())
	})
}

func TestNewFilterRequiredError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		de := NewFilterRequiredError("vector.metadata-search")
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("ErrorStringContainsMessage", func(t *testing.T) {
		de := NewFilterRequiredError("test")
		assert.Contains(t, de.Error(), "--filter")
	})
}

func TestInvalidMetadataJSONError(t *testing.T) {
	t.Run("ErrorMessage", func(t *testing.T) {
		cause := fmt.Errorf("unexpected token")
		err := &InvalidMetadataJSONError{Cause: cause}
		assert.Contains(t, err.Error(), "invalid metadata JSON")
		assert.Contains(t, err.Error(), "unexpected token")
	})

	t.Run("Unwrap", func(t *testing.T) {
		cause := fmt.Errorf("parse error")
		err := &InvalidMetadataJSONError{Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("NilCause", func(t *testing.T) {
		err := &InvalidMetadataJSONError{Cause: nil}
		assert.Contains(t, err.Error(), "invalid metadata JSON")
		assert.Nil(t, err.Unwrap())
	})
}

func TestNewInvalidMetadataJSONError(t *testing.T) {
	t.Run("CreatesQBError", func(t *testing.T) {
		cause := fmt.Errorf("bad json")
		de := NewInvalidMetadataJSONError("vector.upsert", cause)
		require.NotNil(t, de)
		assert.Equal(t, qberrors.ErrInvalidInput, de.Code)
	})

	t.Run("WrappedError", func(t *testing.T) {
		cause := fmt.Errorf("syntax error")
		de := NewInvalidMetadataJSONError("test", cause)
		assert.Contains(t, de.Error(), "syntax error")
	})
}

func TestParseKMSGenerateKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSGenerateKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSImportKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSImportKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSGetKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSGetKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSListKeysResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSListKeysResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSRotateKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSRotateKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSDeleteKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSDeleteKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSUpdateKeyStatusResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSUpdateKeyStatusResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSListKeyVersionsResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSListKeyVersionsResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSExportKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSExportKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSUnwrapKeyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSUnwrapKeyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSSetKeySafetyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSSetKeySafetyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSClearKeySafetyResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSClearKeySafetyResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSEncryptResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSEncryptResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseKMSDecryptResponse_UnmarshalError(t *testing.T) {
	_, err := parseKMSDecryptResponse("not-a-map")
	assert.Error(t, err)
}

// =============================================================================
// DEK Parse Function Error Paths
// =============================================================================

// =============================================================================
// Backend/Safety Parse Function Error Paths
// =============================================================================

func TestParseBackendStatusResponse_UnmarshalError(t *testing.T) {
	_, err := parseBackendStatusResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseBackendTestResponse_UnmarshalError(t *testing.T) {
	_, err := parseBackendTestResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSafetyCheckResponse_UnmarshalError(t *testing.T) {
	_, err := parseSafetyCheckResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSafetyReportResponse_UnmarshalError(t *testing.T) {
	_, err := parseSafetyReportResponse("not-a-map")
	assert.Error(t, err)
}

// =============================================================================
// toJSONMap Edge Cases
// =============================================================================

func TestToJSONMap_MarshalFailure(t *testing.T) {
	// Channel cannot be marshalled to JSON
	_, err := toJSONMap(make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal")
}

func TestToJSONMap_UnmarshalToMapFailure(t *testing.T) {
	// A JSON array cannot be unmarshalled into a map
	_, err := toJSONMap([]string{"a", "b"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

// =============================================================================
// getBytes Edge Cases
// =============================================================================

func TestGetBytes_ByteSliceValue(t *testing.T) {
	data := map[string]interface{}{
		"binary": []byte{0x01, 0x02, 0x03},
	}
	result := getBytes(data, "binary")
	assert.Equal(t, []byte{0x01, 0x02, 0x03}, result)
}

func TestGetBytes_StringValue(t *testing.T) {
	data := map[string]interface{}{
		"text": "hello",
	}
	result := getBytes(data, "text")
	assert.Equal(t, []byte("hello"), result)
}

func TestGetBytes_NilMap(t *testing.T) {
	result := getBytes(nil, "key")
	assert.Nil(t, result)
}

func TestGetBytes_AbsentKey(t *testing.T) {
	data := map[string]interface{}{}
	result := getBytes(data, "missing")
	assert.Nil(t, result)
}

func TestGetBytes_NonStringNonByte(t *testing.T) {
	data := map[string]interface{}{
		"number": 42,
	}
	result := getBytes(data, "number")
	assert.Nil(t, result)
}

func TestJobFailedError(t *testing.T) {
	t.Run("ErrorMessageWithDetails", func(t *testing.T) {
		err := &JobFailedError{JobID: "job_123", Message: "timeout exceeded"}
		assert.Equal(t, "job job_123 failed: timeout exceeded", err.Error())
	})

	t.Run("ErrorMessageWithoutDetails", func(t *testing.T) {
		err := &JobFailedError{JobID: "job_456", Message: ""}
		assert.Equal(t, "job job_456 failed", err.Error())
	})

	t.Run("NewJobFailedError", func(t *testing.T) {
		qbErr := NewJobFailedError("orchestrator.run", "job_789", "agent crashed")
		require.NotNil(t, qbErr)
		assert.Equal(t, qberrors.ErrInternal, qbErr.Code)
		assert.Equal(t, "orchestrator.run", qbErr.Op)

		var jobErr *JobFailedError
		ok := errors.As(qbErr.Err, &jobErr)
		require.True(t, ok)
		assert.Equal(t, "job_789", jobErr.JobID)
		assert.Equal(t, "agent crashed", jobErr.Message)
	})

	t.Run("NewJobFailedErrorNoMessage", func(t *testing.T) {
		qbErr := NewJobFailedError("orchestrator.run", "job_empty", "")
		require.NotNil(t, qbErr)
		assert.Contains(t, qbErr.Error(), "job job_empty failed")
	})
}
