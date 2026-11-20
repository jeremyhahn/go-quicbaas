// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package backup

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewStorageBackend_RefusesSessionTokenWithStaticKeys proves a temporary
// credential config is refused rather than sent without its session token:
// the storage library builds static credentials with an empty token.
func TestNewStorageBackend_RefusesSessionTokenWithStaticKeys(t *testing.T) {
	for _, backend := range []string{"s3", "minio"} {
		for _, tokenKey := range []string{"session_token", "aws_session_token", "sessionToken"} {
			t.Run(backend+"/"+tokenKey, func(t *testing.T) {
				storage, err := NewStorageBackend(&Config{
					StorageBackend: backend,
					StorageConfig: map[string]string{
						"endpoint":   "127.0.0.1:1",
						"bucket":     "backups",
						"region":     "us-east-1",
						"access_key": "AKIDEXAMPLE",
						"secret_key": "secret",
						tokenKey:     "token",
					},
				})
				require.Error(t, err)
				assert.Nil(t, storage)
				assert.Contains(t, err.Error(), "session_token")
				if backend == "minio" {
					assert.NotContains(t, err.Error(), "environment credential chain")
				} else {
					assert.Contains(t, err.Error(), "environment credential chain")
				}
			})
		}
	}
}

// TestObjstoreSettings_EndpointSchemeFollowsUseSSL proves a bare endpoint
// takes its scheme from use_ssl, an endpoint whose scheme contradicts an
// explicit use_ssl is refused, and the caller's map is never modified.
func TestObjstoreSettings_EndpointSchemeFollowsUseSSL(t *testing.T) {
	cases := []struct {
		name     string
		backend  string
		endpoint string
		useSSL   string // "" means unset
		want     string // expected endpoint; "" with wantErr
		wantErr  bool
	}{
		{"minio bare unset", "minio", "localhost:9000", "", "http://localhost:9000", false},
		{"minio bare false", "minio", "localhost:9000", "false", "http://localhost:9000", false},
		{"minio bare true", "minio", "localhost:9000", "true", "https://localhost:9000", false},
		{"minio http unset", "minio", "http://localhost:9000", "", "http://localhost:9000", false},
		{"minio https unset", "minio", "https://localhost:9000", "", "https://localhost:9000", false},
		{"minio https true", "minio", "https://localhost:9000", "true", "https://localhost:9000", false},
		{"minio http true", "minio", "http://localhost:9000", "true", "", true},
		{"minio https false", "minio", "https://localhost:9000", "false", "", true},
		{"s3 bare unset", "s3", "s3.example.com", "", "s3.example.com", false},
		{"s3 bare true", "s3", "s3.example.com", "true", "s3.example.com", false},
		{"s3 bare false", "s3", "s3.example.com", "false", "http://s3.example.com", false},
		{"s3 http false", "s3", "http://s3.example.com", "false", "http://s3.example.com", false},
		{"s3 http true", "s3", "http://s3.example.com", "true", "", true},
		{"s3 https false", "s3", "https://s3.example.com", "false", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := map[string]string{"endpoint": tc.endpoint, "bucket": "backups"}
			if tc.useSSL != "" {
				in["use_ssl"] = tc.useSSL
			}
			before := maps.Clone(in)

			got, err := objstoreBackendSettings(tc.backend, in)
			assert.Equal(t, before, in, "the caller's map must not be modified")
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "use_ssl")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got["endpoint"])
		})
	}
}
