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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestCLIErrors checks every CLI error constructor: the QBError code and op,
// the typed inner error reachable through errors.As with its field set, and
// the inner message carried into QBError.Error().
func TestCLIErrors(t *testing.T) {
	cause := errors.New("unexpected token")
	tests := []struct {
		name string
		err  *qberrors.QBError
		code qberrors.ErrorCode
		msg  string
		as   func(t *testing.T, err error)
	}{
		{
			name: "UnsupportedAlgorithm",
			err:  NewUnsupportedAlgorithmError("op", "CHACHA20"),
			code: qberrors.ErrInvalidInput,
			msg:  "unsupported algorithm: CHACHA20",
			as: func(t *testing.T, err error) {
				var inner *UnsupportedAlgorithmError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "CHACHA20", inner.Algorithm)
			},
		},
		{
			name: "InvalidDayFormat",
			err:  NewInvalidDayFormatError("op", "abc"),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid day format: abc",
			as: func(t *testing.T, err error) {
				var inner *InvalidDayFormatError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "abc", inner.Value)
			},
		},
		{
			name: "NegativeDuration",
			err:  NewNegativeDurationError("op", "-5h"),
			code: qberrors.ErrInvalidInput,
			msg:  "grace period cannot be negative: -5h",
			as: func(t *testing.T, err error) {
				var inner *NegativeDurationError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "-5h", inner.Value)
			},
		},
		{
			name: "InvalidDurationFormat",
			err:  NewInvalidDurationFormatError("op", "not-a-duration"),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid duration format: not-a-duration",
			as: func(t *testing.T, err error) {
				var inner *InvalidDurationFormatError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "not-a-duration", inner.Value)
			},
		},
		{
			name: "UnsupportedOutputFormat",
			err:  NewUnsupportedOutputFormatError("op", "xml"),
			code: qberrors.ErrInvalidInput,
			msg:  "unsupported output format: xml",
			as: func(t *testing.T, err error) {
				var inner *UnsupportedOutputFormatError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "xml", inner.Format)
			},
		},
		{
			name: "NoActiveMigration",
			err:  NewNoActiveMigrationError("op"),
			code: qberrors.ErrNotFound,
			msg:  "no active migration found",
			as: func(t *testing.T, err error) {
				var inner *NoActiveMigrationError
				require.True(t, errors.As(err, &inner))
			},
		},
		{
			name: "InvalidStorageURL",
			err:  NewInvalidStorageURLError("op", "bad-url"),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid storage URL format: bad-url (expected scheme://path)",
			as: func(t *testing.T, err error) {
				var inner *InvalidStorageURLError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "bad-url", inner.URL)
			},
		},
		{
			name: "UnsupportedStorageScheme",
			err:  NewUnsupportedStorageSchemeError("op", "ftp"),
			code: qberrors.ErrInvalidInput,
			msg:  "unsupported storage scheme: ftp (supported: pebble, memory, file)",
			as: func(t *testing.T, err error) {
				var inner *UnsupportedStorageSchemeError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "ftp", inner.Scheme)
			},
		},
		{
			name: "MigrationPartialFailure",
			err:  NewMigrationPartialFailureError("op", 42),
			code: qberrors.ErrInternal,
			msg:  "migration completed with 42 failed keys",
			as: func(t *testing.T, err error) {
				var inner *MigrationPartialFailureError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, int64(42), inner.FailedKeys)
			},
		},
		{
			name: "KeyNotFound",
			err:  NewKeyNotFoundError("op"),
			code: qberrors.ErrNotFound,
			msg:  "key not found",
			as: func(t *testing.T, err error) {
				var inner *KeyNotFoundError
				require.True(t, errors.As(err, &inner))
			},
		},
		{
			name: "InvalidFilterFormat",
			err:  NewInvalidFilterFormatError("op", "badfilter"),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid filter format: badfilter (expected key=value)",
			as: func(t *testing.T, err error) {
				var inner *InvalidFilterFormatError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, "badfilter", inner.Filter)
			},
		},
		{
			name: "NoMetadataProvided",
			err:  NewNoMetadataProvidedError("op"),
			code: qberrors.ErrInvalidInput,
			msg:  "no metadata provided; use --metadata or --metadata-json",
			as: func(t *testing.T, err error) {
				var inner *NoMetadataProvidedError
				require.True(t, errors.As(err, &inner))
			},
		},
		{
			name: "FilterRequired",
			err:  NewFilterRequiredError("op"),
			code: qberrors.ErrInvalidInput,
			msg:  "at least one --filter is required",
			as: func(t *testing.T, err error) {
				var inner *FilterRequiredError
				require.True(t, errors.As(err, &inner))
			},
		},
		{
			name: "InvalidMetadataJSON",
			err:  NewInvalidMetadataJSONError("op", cause),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid metadata JSON: unexpected token",
			as: func(t *testing.T, err error) {
				var inner *InvalidMetadataJSONError
				require.True(t, errors.As(err, &inner))
				assert.Equal(t, cause, inner.Unwrap())
			},
		},
		{
			name: "InvalidMetadataJSONNilCause",
			err:  NewInvalidMetadataJSONError("op", nil),
			code: qberrors.ErrInvalidInput,
			msg:  "invalid metadata JSON: <nil>",
			as: func(t *testing.T, err error) {
				var inner *InvalidMetadataJSONError
				require.True(t, errors.As(err, &inner))
				assert.Nil(t, inner.Unwrap())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.err)
			assert.Equal(t, tt.code, tt.err.Code)
			assert.Equal(t, "op", tt.err.Op)
			assert.Equal(t, tt.msg, tt.err.Err.Error())
			assert.Contains(t, tt.err.Error(), tt.msg)
			tt.as(t, tt.err)
		})
	}
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
