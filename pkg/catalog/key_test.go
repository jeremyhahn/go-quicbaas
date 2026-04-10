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

package catalog

import (
	"errors"
	"strings"
	"testing"
)

func TestNewObjectKey(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		path      string
		wantErr   bool
		errCode   ErrorCode
	}{
		{
			name:      "valid simple key",
			namespace: "backups",
			path:      "daily/2024-01-15.tar.gz",
			wantErr:   false,
		},
		{
			name:      "valid tenant key",
			namespace: "tenant-acme",
			path:      "documents/report.pdf",
			wantErr:   false,
		},
		{
			name:      "valid with underscores",
			namespace: "my_namespace",
			path:      "file_name.txt",
			wantErr:   false,
		},
		{
			name:      "valid numeric namespace",
			namespace: "123tenant",
			path:      "data.json",
			wantErr:   false,
		},
		{
			name:      "empty namespace",
			namespace: "",
			path:      "test.txt",
			wantErr:   true,
			errCode:   ErrCodeNamespaceEmpty,
		},
		{
			name:      "empty path",
			namespace: "backups",
			path:      "",
			wantErr:   true,
			errCode:   ErrCodePathEmpty,
		},
		{
			name:      "invalid namespace starting with hyphen",
			namespace: "-invalid",
			path:      "test.txt",
			wantErr:   true,
			errCode:   ErrCodeNamespaceInvalid,
		},
		{
			name:      "invalid namespace with space",
			namespace: "my namespace",
			path:      "test.txt",
			wantErr:   true,
			errCode:   ErrCodeNamespaceInvalid,
		},
		{
			name:      "invalid path starting with slash",
			namespace: "backups",
			path:      "/invalid/path",
			wantErr:   true,
			errCode:   ErrCodePathInvalid,
		},
		{
			name:      "invalid path ending with slash",
			namespace: "backups",
			path:      "invalid/path/",
			wantErr:   true,
			errCode:   ErrCodePathInvalid,
		},
		{
			name:      "invalid path with consecutive slashes",
			namespace: "backups",
			path:      "invalid//path",
			wantErr:   true,
			errCode:   ErrCodePathInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := NewObjectKey(tt.namespace, tt.path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewObjectKey() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("NewObjectKey() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("NewObjectKey() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("NewObjectKey() error = %v, wantErr = false", err)
				return
			}

			if key.Namespace != tt.namespace {
				t.Errorf("NewObjectKey() namespace = %v, want %v", key.Namespace, tt.namespace)
			}
			if key.Path != tt.path {
				t.Errorf("NewObjectKey() path = %v, want %v", key.Path, tt.path)
			}
		})
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		wantNamespace string
		wantPath      string
		wantErr       bool
		errCode       ErrorCode
	}{
		{
			name:          "valid simple key",
			key:           "backups:daily/2024-01-15.tar.gz",
			wantNamespace: "backups",
			wantPath:      "daily/2024-01-15.tar.gz",
			wantErr:       false,
		},
		{
			name:          "valid tenant key",
			key:           "tenant-acme:documents/report.pdf",
			wantNamespace: "tenant-acme",
			wantPath:      "documents/report.pdf",
			wantErr:       false,
		},
		{
			name:          "valid blobs key",
			key:           "blobs:user/123/avatar.png",
			wantNamespace: "blobs",
			wantPath:      "user/123/avatar.png",
			wantErr:       false,
		},
		{
			name:          "valid system key",
			key:           "system:config/cluster.yaml",
			wantNamespace: "system",
			wantPath:      "config/cluster.yaml",
			wantErr:       false,
		},
		{
			name:    "empty key",
			key:     "",
			wantErr: true,
			errCode: ErrCodeKeyEmpty,
		},
		{
			name:    "missing separator",
			key:     "backupsdaily2024.tar.gz",
			wantErr: true,
			errCode: ErrCodeKeyInvalid,
		},
		{
			name:    "empty namespace",
			key:     ":path/to/file",
			wantErr: true,
			errCode: ErrCodeNamespaceEmpty,
		},
		{
			name:    "empty path",
			key:     "namespace:",
			wantErr: true,
			errCode: ErrCodePathEmpty,
		},
		{
			name:    "key too long",
			key:     strings.Repeat("a", MaxKeyLength+1),
			wantErr: true,
			errCode: ErrCodeKeyTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParseKey(tt.key)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseKey() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("ParseKey() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("ParseKey() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseKey() error = %v, wantErr = false", err)
				return
			}

			if key.Namespace != tt.wantNamespace {
				t.Errorf("ParseKey() namespace = %v, want %v", key.Namespace, tt.wantNamespace)
			}
			if key.Path != tt.wantPath {
				t.Errorf("ParseKey() path = %v, want %v", key.Path, tt.wantPath)
			}
		})
	}
}

func TestObjectKey_String(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		path      string
		want      string
	}{
		{
			name:      "simple key",
			namespace: "backups",
			path:      "daily/2024-01-15.tar.gz",
			want:      "backups:daily/2024-01-15.tar.gz",
		},
		{
			name:      "tenant key",
			namespace: "tenant-acme",
			path:      "documents/report.pdf",
			want:      "tenant-acme:documents/report.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &ObjectKey{
				Namespace: tt.namespace,
				Path:      tt.path,
			}
			if got := key.String(); got != tt.want {
				t.Errorf("ObjectKey.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectKey_Validate(t *testing.T) {
	tests := []struct {
		name    string
		key     *ObjectKey
		wantErr bool
		errCode ErrorCode
	}{
		{
			name: "valid key",
			key: &ObjectKey{
				Namespace: "backups",
				Path:      "daily/2024-01-15.tar.gz",
			},
			wantErr: false,
		},
		{
			name: "empty namespace",
			key: &ObjectKey{
				Namespace: "",
				Path:      "file.txt",
			},
			wantErr: true,
			errCode: ErrCodeNamespaceEmpty,
		},
		{
			name: "empty path",
			key: &ObjectKey{
				Namespace: "backups",
				Path:      "",
			},
			wantErr: true,
			errCode: ErrCodePathEmpty,
		},
		{
			name: "namespace too long",
			key: &ObjectKey{
				Namespace: strings.Repeat("a", MaxNamespaceLength+1),
				Path:      "file.txt",
			},
			wantErr: true,
			errCode: ErrCodeNamespaceInvalid,
		},
		{
			name: "path too long",
			key: &ObjectKey{
				Namespace: "backups",
				Path:      strings.Repeat("a", MaxPathLength+1),
			},
			wantErr: true,
			errCode: ErrCodePathInvalid,
		},
		{
			name: "invalid namespace characters",
			key: &ObjectKey{
				Namespace: "invalid@namespace",
				Path:      "file.txt",
			},
			wantErr: true,
			errCode: ErrCodeNamespaceInvalid,
		},
		{
			name: "namespace starting with underscore",
			key: &ObjectKey{
				Namespace: "_invalid",
				Path:      "file.txt",
			},
			wantErr: true,
			errCode: ErrCodeNamespaceInvalid,
		},
		{
			name: "path with space",
			key: &ObjectKey{
				Namespace: "backups",
				Path:      "file name.txt",
			},
			wantErr: true,
			errCode: ErrCodePathInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.key.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("Validate() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("Validate() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() error = %v, wantErr = false", err)
			}
		})
	}
}

func TestIsValidNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      bool
	}{
		{"simple lowercase", "backups", true},
		{"with hyphens", "tenant-acme", true},
		{"with underscores", "my_namespace", true},
		{"with numbers", "tenant123", true},
		{"starts with number", "123tenant", true},
		{"uppercase", "BACKUPS", true},
		{"mixed case", "MyNamespace", true},
		{"empty", "", false},
		{"starts with hyphen", "-invalid", false},
		{"starts with underscore", "_invalid", false},
		{"contains space", "my namespace", false},
		{"contains dot", "my.namespace", false},
		{"contains slash", "my/namespace", false},
		{"contains colon", "my:namespace", false},
		{"contains at sign", "user@domain", false},
		{"contains special char", "namespace!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidNamespace(tt.namespace); got != tt.want {
				t.Errorf("IsValidNamespace(%q) = %v, want %v", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestIsValidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"simple file", "file.txt", true},
		{"nested path", "dir1/dir2/file.txt", true},
		{"with hyphens", "my-file.txt", true},
		{"with underscores", "my_file.txt", true},
		{"with numbers", "file123.txt", true},
		{"multiple dots", "file.backup.tar.gz", true},
		{"empty", "", false},
		{"starts with slash", "/path/to/file", false},
		{"ends with slash", "path/to/dir/", false},
		{"consecutive slashes", "path//to/file", false},
		{"contains space", "my file.txt", false},
		{"contains colon", "my:file.txt", false},
		{"contains at sign", "file@backup.txt", false},
		{"contains special char", "file!.txt", false},
		{"contains control char", "file\x00.txt", false},
		{"contains newline", "file\n.txt", false},
		{"contains tab", "file\t.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidPath(tt.path); got != tt.want {
				t.Errorf("IsValidPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestObjectKey_CatalogKey(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		path      string
		want      string
	}{
		{
			name:      "simple key",
			namespace: "backups",
			path:      "daily/2024-01-15.tar.gz",
			want:      "cat:backups:daily/2024-01-15.tar.gz",
		},
		{
			name:      "tenant key",
			namespace: "tenant-acme",
			path:      "documents/report.pdf",
			want:      "cat:tenant-acme:documents/report.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &ObjectKey{
				Namespace: tt.namespace,
				Path:      tt.path,
			}
			if got := string(key.CatalogKey()); got != tt.want {
				t.Errorf("ObjectKey.CatalogKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectKey_IndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/report.pdf",
	}

	tests := []struct {
		name       string
		indexType  string
		indexValue string
		want       string
	}{
		{
			name:       "store index",
			indexType:  IndexTypeStore,
			indexValue: "s3-primary",
			want:       "idx:store:s3-primary:tenant-acme:documents/report.pdf",
		},
		{
			name:       "tenant index",
			indexType:  IndexTypeTenant,
			indexValue: "tenant-acme",
			want:       "idx:tenant:tenant-acme:tenant-acme:documents/report.pdf",
		},
		{
			name:       "checksum index",
			indexType:  IndexTypeChecksum,
			indexValue: "abc123def456",
			want:       "idx:checksum:abc123def456:tenant-acme:documents/report.pdf",
		},
		{
			name:       "status index",
			indexType:  IndexTypeStatus,
			indexValue: "active",
			want:       "idx:status:active:tenant-acme:documents/report.pdf",
		},
		{
			name:       "encryption index",
			indexType:  IndexTypeEncryption,
			indexValue: "dek:tenant/acme/objects",
			want:       "idx:encryption:dek:tenant/acme/objects:tenant-acme:documents/report.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(key.IndexKey(tt.indexType, tt.indexValue)); got != tt.want {
				t.Errorf("ObjectKey.IndexKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestObjectKey_StoreIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "backups",
		Path:      "daily/backup.tar.gz",
	}

	got := string(key.StoreIndexKey("s3-primary"))
	want := "idx:store:s3-primary:backups:daily/backup.tar.gz"

	if got != want {
		t.Errorf("ObjectKey.StoreIndexKey() = %v, want %v", got, want)
	}
}

func TestObjectKey_TenantIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "report.pdf",
	}

	got := string(key.TenantIndexKey("acme"))
	want := "idx:tenant:acme:tenant-acme:report.pdf"

	if got != want {
		t.Errorf("ObjectKey.TenantIndexKey() = %v, want %v", got, want)
	}
}

func TestObjectKey_ChecksumIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "blobs",
		Path:      "file.bin",
	}

	got := string(key.ChecksumIndexKey("sha256:abc123"))
	want := "idx:checksum:sha256:abc123:blobs:file.bin"

	if got != want {
		t.Errorf("ObjectKey.ChecksumIndexKey() = %v, want %v", got, want)
	}
}

func TestObjectKey_StatusIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "archive",
		Path:      "old/data.zip",
	}

	got := string(key.StatusIndexKey("archived"))
	want := "idx:status:archived:archive:old/data.zip"

	if got != want {
		t.Errorf("ObjectKey.StatusIndexKey() = %v, want %v", got, want)
	}
}

func TestObjectKey_TagIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/report.pdf",
	}

	got := string(key.TagIndexKey("department", "finance"))
	want := "idx:tag:department:finance:tenant-acme:documents/report.pdf"

	if got != want {
		t.Errorf("ObjectKey.TagIndexKey() = %v, want %v", got, want)
	}
}

func TestObjectKey_EncryptionIndexKey(t *testing.T) {
	key := &ObjectKey{
		Namespace: "secrets",
		Path:      "credentials.json",
	}

	got := string(key.EncryptionIndexKey("dek:prod/secrets"))
	want := "idx:encryption:dek:prod/secrets:secrets:credentials.json"

	if got != want {
		t.Errorf("ObjectKey.EncryptionIndexKey() = %v, want %v", got, want)
	}
}

func TestParseCatalogKey(t *testing.T) {
	tests := []struct {
		name          string
		catalogKey    []byte
		wantNamespace string
		wantPath      string
		wantErr       bool
		errCode       ErrorCode
	}{
		{
			name:          "valid catalog key",
			catalogKey:    []byte("cat:tenant-acme:documents/report.pdf"),
			wantNamespace: "tenant-acme",
			wantPath:      "documents/report.pdf",
			wantErr:       false,
		},
		{
			name:          "valid with nested path",
			catalogKey:    []byte("cat:backups:daily/2024/01/backup.tar.gz"),
			wantNamespace: "backups",
			wantPath:      "daily/2024/01/backup.tar.gz",
			wantErr:       false,
		},
		{
			name:       "missing prefix",
			catalogKey: []byte("tenant-acme:documents/report.pdf"),
			wantErr:    true,
			errCode:    ErrCodeKeyInvalid,
		},
		{
			name:       "wrong prefix",
			catalogKey: []byte("idx:tenant-acme:documents/report.pdf"),
			wantErr:    true,
			errCode:    ErrCodeKeyInvalid,
		},
		{
			name:       "empty after prefix",
			catalogKey: []byte("cat:"),
			wantErr:    true,
			errCode:    ErrCodeKeyEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParseCatalogKey(tt.catalogKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCatalogKey() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("ParseCatalogKey() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("ParseCatalogKey() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseCatalogKey() error = %v, wantErr = false", err)
				return
			}

			if key.Namespace != tt.wantNamespace {
				t.Errorf("ParseCatalogKey() namespace = %v, want %v", key.Namespace, tt.wantNamespace)
			}
			if key.Path != tt.wantPath {
				t.Errorf("ParseCatalogKey() path = %v, want %v", key.Path, tt.wantPath)
			}
		})
	}
}

func TestExtractNamespaceFromKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"valid key", "tenant-acme:documents/report.pdf", "tenant-acme"},
		{"simple key", "backups:file.tar.gz", "backups"},
		{"missing separator", "noseparator", ""},
		{"empty key", "", ""},
		{"starts with separator", ":path/to/file", ""},
		{"only separator", ":", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractNamespaceFromKey(tt.key); got != tt.want {
				t.Errorf("ExtractNamespaceFromKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractPathFromKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"valid key", "tenant-acme:documents/report.pdf", "documents/report.pdf"},
		{"simple key", "backups:file.tar.gz", "file.tar.gz"},
		{"nested path", "ns:a/b/c/d.txt", "a/b/c/d.txt"},
		{"missing separator", "noseparator", ""},
		{"empty key", "", ""},
		{"ends with separator", "namespace:", ""},
		{"only separator", ":", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractPathFromKey(tt.key); got != tt.want {
				t.Errorf("ExtractPathFromKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildIndexPrefix(t *testing.T) {
	tests := []struct {
		name       string
		indexType  string
		indexValue string
		want       string
	}{
		{"store index", IndexTypeStore, "s3-primary", "idx:store:s3-primary:"},
		{"tenant index", IndexTypeTenant, "tenant-acme", "idx:tenant:tenant-acme:"},
		{"checksum index", IndexTypeChecksum, "abc123", "idx:checksum:abc123:"},
		{"status index", IndexTypeStatus, "active", "idx:status:active:"},
		{"encryption index", IndexTypeEncryption, "key-123", "idx:encryption:key-123:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(BuildIndexPrefix(tt.indexType, tt.indexValue)); got != tt.want {
				t.Errorf("BuildIndexPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildTagIndexPrefix(t *testing.T) {
	tests := []struct {
		name  string
		tag   string
		value string
		want  string
	}{
		{"department tag", "department", "finance", "idx:tag:department:finance:"},
		{"classification tag", "classification", "internal", "idx:tag:classification:internal:"},
		{"custom tag", "custom", "value", "idx:tag:custom:value:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(BuildTagIndexPrefix(tt.tag, tt.value)); got != tt.want {
				t.Errorf("BuildTagIndexPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildCatalogPrefix(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{"backups namespace", "backups", "cat:backups:"},
		{"tenant namespace", "tenant-acme", "cat:tenant-acme:"},
		{"system namespace", "system", "cat:system:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(BuildCatalogPrefix(tt.namespace)); got != tt.want {
				t.Errorf("BuildCatalogPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildAllCatalogPrefix(t *testing.T) {
	want := "cat:"
	if got := string(BuildAllCatalogPrefix()); got != want {
		t.Errorf("BuildAllCatalogPrefix() = %v, want %v", got, want)
	}
}

func TestParseIndexKey(t *testing.T) {
	tests := []struct {
		name           string
		indexKey       []byte
		wantNamespace  string
		wantPath       string
		wantIndexValue string
		wantErr        bool
		errCode        ErrorCode
	}{
		{
			name:           "valid store index",
			indexKey:       []byte("idx:store:s3-primary:tenant-acme:documents/report.pdf"),
			wantNamespace:  "tenant-acme",
			wantPath:       "documents/report.pdf",
			wantIndexValue: "s3-primary",
			wantErr:        false,
		},
		{
			name:           "valid status index",
			indexKey:       []byte("idx:status:active:backups:daily/backup.tar.gz"),
			wantNamespace:  "backups",
			wantPath:       "daily/backup.tar.gz",
			wantIndexValue: "active",
			wantErr:        false,
		},
		{
			name:     "missing prefix",
			indexKey: []byte("store:s3-primary:tenant-acme:report.pdf"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing index type",
			indexKey: []byte("idx:"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing index value",
			indexKey: []byte("idx:store:"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing object key",
			indexKey: []byte("idx:store:s3-primary:"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, indexValue, err := ParseIndexKey(tt.indexKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseIndexKey() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("ParseIndexKey() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("ParseIndexKey() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseIndexKey() error = %v, wantErr = false", err)
				return
			}

			if key.Namespace != tt.wantNamespace {
				t.Errorf("ParseIndexKey() namespace = %v, want %v", key.Namespace, tt.wantNamespace)
			}
			if key.Path != tt.wantPath {
				t.Errorf("ParseIndexKey() path = %v, want %v", key.Path, tt.wantPath)
			}
			if indexValue != tt.wantIndexValue {
				t.Errorf("ParseIndexKey() indexValue = %v, want %v", indexValue, tt.wantIndexValue)
			}
		})
	}
}

func TestParseTagIndexKey(t *testing.T) {
	tests := []struct {
		name          string
		indexKey      []byte
		wantNamespace string
		wantPath      string
		wantTagName   string
		wantTagValue  string
		wantErr       bool
		errCode       ErrorCode
	}{
		{
			name:          "valid tag index with department",
			indexKey:      []byte("idx:tag:department:finance:tenant-acme:documents/report.pdf"),
			wantNamespace: "tenant-acme",
			wantPath:      "documents/report.pdf",
			wantTagName:   "department",
			wantTagValue:  "finance",
			wantErr:       false,
		},
		{
			name:          "valid tag index with classification",
			indexKey:      []byte("idx:tag:classification:internal:backups:daily/backup.tar.gz"),
			wantNamespace: "backups",
			wantPath:      "daily/backup.tar.gz",
			wantTagName:   "classification",
			wantTagValue:  "internal",
			wantErr:       false,
		},
		{
			name:          "valid tag index with empty value",
			indexKey:      []byte("idx:tag:env::system:config.yaml"),
			wantNamespace: "system",
			wantPath:      "config.yaml",
			wantTagName:   "env",
			wantTagValue:  "",
			wantErr:       false,
		},
		{
			name:     "missing tag prefix",
			indexKey: []byte("idx:store:s3-primary:tenant-acme:report.pdf"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "wrong prefix",
			indexKey: []byte("cat:tag:department:finance:tenant:file"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing tag name",
			indexKey: []byte("idx:tag:"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing tag value separator",
			indexKey: []byte("idx:tag:department"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "missing object key",
			indexKey: []byte("idx:tag:department:finance:"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
		{
			name:     "invalid object key format",
			indexKey: []byte("idx:tag:department:finance:invalidkey"),
			wantErr:  true,
			errCode:  ErrCodeKeyInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, tagName, tagValue, err := ParseTagIndexKey(tt.indexKey)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTagIndexKey() error = nil, wantErr = true")
					return
				}
				var ce *CatalogError
				if !errors.As(err, &ce) {
					t.Errorf("ParseTagIndexKey() error is not CatalogError: %T", err)
					return
				}
				if ce.Code != tt.errCode {
					t.Errorf("ParseTagIndexKey() error code = %v, want %v", ce.Code, tt.errCode)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTagIndexKey() error = %v, wantErr = false", err)
				return
			}

			if key.Namespace != tt.wantNamespace {
				t.Errorf("ParseTagIndexKey() namespace = %v, want %v", key.Namespace, tt.wantNamespace)
			}
			if key.Path != tt.wantPath {
				t.Errorf("ParseTagIndexKey() path = %v, want %v", key.Path, tt.wantPath)
			}
			if tagName != tt.wantTagName {
				t.Errorf("ParseTagIndexKey() tagName = %v, want %v", tagName, tt.wantTagName)
			}
			if tagValue != tt.wantTagValue {
				t.Errorf("ParseTagIndexKey() tagValue = %v, want %v", tagValue, tt.wantTagValue)
			}
		})
	}
}

func TestParseTagIndexKeyRoundtrip(t *testing.T) {
	// Test that TagIndexKey and ParseTagIndexKey are inverse operations
	original := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/report.pdf",
	}
	tagName := "department"
	tagValue := "finance"

	indexKey := original.TagIndexKey(tagName, tagValue)
	parsed, parsedTagName, parsedTagValue, err := ParseTagIndexKey(indexKey)
	if err != nil {
		t.Fatalf("ParseTagIndexKey() error = %v", err)
	}

	if parsed.Namespace != original.Namespace {
		t.Errorf("Namespace roundtrip failed: got %v, want %v", parsed.Namespace, original.Namespace)
	}
	if parsed.Path != original.Path {
		t.Errorf("Path roundtrip failed: got %v, want %v", parsed.Path, original.Path)
	}
	if parsedTagName != tagName {
		t.Errorf("TagName roundtrip failed: got %v, want %v", parsedTagName, tagName)
	}
	if parsedTagValue != tagValue {
		t.Errorf("TagValue roundtrip failed: got %v, want %v", parsedTagValue, tagValue)
	}
}

func TestConstants(t *testing.T) {
	// Verify constants are set correctly
	if KeySeparator != ":" {
		t.Errorf("KeySeparator = %v, want :", KeySeparator)
	}
	if MaxKeyLength != 1024 {
		t.Errorf("MaxKeyLength = %v, want 1024", MaxKeyLength)
	}
	if MaxNamespaceLength != 128 {
		t.Errorf("MaxNamespaceLength = %v, want 128", MaxNamespaceLength)
	}
	if MaxPathLength != 895 {
		t.Errorf("MaxPathLength = %v, want 895", MaxPathLength)
	}

	// Verify MaxPathLength = MaxKeyLength - MaxNamespaceLength - 1 (separator)
	expectedMaxPath := MaxKeyLength - MaxNamespaceLength - 1
	if MaxPathLength != expectedMaxPath {
		t.Errorf("MaxPathLength = %v, expected %v (MaxKeyLength - MaxNamespaceLength - 1)", MaxPathLength, expectedMaxPath)
	}

	// Verify prefixes
	if PrefixCatalog != "cat:" {
		t.Errorf("PrefixCatalog = %v, want cat:", PrefixCatalog)
	}
	if PrefixIndex != "idx:" {
		t.Errorf("PrefixIndex = %v, want idx:", PrefixIndex)
	}

	// Verify index types
	if IndexTypeStore != "store" {
		t.Errorf("IndexTypeStore = %v, want store", IndexTypeStore)
	}
	if IndexTypeTenant != "tenant" {
		t.Errorf("IndexTypeTenant = %v, want tenant", IndexTypeTenant)
	}
	if IndexTypeChecksum != "checksum" {
		t.Errorf("IndexTypeChecksum = %v, want checksum", IndexTypeChecksum)
	}
	if IndexTypeStatus != "status" {
		t.Errorf("IndexTypeStatus = %v, want status", IndexTypeStatus)
	}
	if IndexTypeTag != "tag" {
		t.Errorf("IndexTypeTag = %v, want tag", IndexTypeTag)
	}
	if IndexTypeEncryption != "encryption" {
		t.Errorf("IndexTypeEncryption = %v, want encryption", IndexTypeEncryption)
	}
}

func TestParseKeyRoundtrip(t *testing.T) {
	// Test that parsing and converting back to string produces the same result
	tests := []string{
		"backups:daily/2024-01-15.tar.gz",
		"tenant-acme:documents/report.pdf",
		"blobs:user/123/avatar.png",
		"system:config/cluster.yaml",
		"ns123:path/to/file.txt",
		"a:b",
	}

	for _, original := range tests {
		t.Run(original, func(t *testing.T) {
			key, err := ParseKey(original)
			if err != nil {
				t.Fatalf("ParseKey() error = %v", err)
			}

			result := key.String()
			if result != original {
				t.Errorf("Roundtrip failed: got %v, want %v", result, original)
			}
		})
	}
}

func TestCatalogKeyRoundtrip(t *testing.T) {
	// Test that CatalogKey and ParseCatalogKey are inverse operations
	original := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/report.pdf",
	}

	catalogKey := original.CatalogKey()
	parsed, err := ParseCatalogKey(catalogKey)
	if err != nil {
		t.Fatalf("ParseCatalogKey() error = %v", err)
	}

	if parsed.Namespace != original.Namespace {
		t.Errorf("Namespace roundtrip failed: got %v, want %v", parsed.Namespace, original.Namespace)
	}
	if parsed.Path != original.Path {
		t.Errorf("Path roundtrip failed: got %v, want %v", parsed.Path, original.Path)
	}
}

func TestUnicodeNamespace(t *testing.T) {
	// Unicode letters should be valid in namespaces
	tests := []struct {
		namespace string
		valid     bool
	}{
		{"tenant", true},
		{"tenant123", true},
		{"Tenant", true},
		{"tenantAccent", true},
		// Unicode letters are valid
		{"tenant", true},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := IsValidNamespace(tt.namespace)
			if got != tt.valid {
				t.Errorf("IsValidNamespace(%q) = %v, want %v", tt.namespace, got, tt.valid)
			}
		})
	}
}

func TestEdgeCasePaths(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		valid bool
	}{
		{"single char", "a", true},
		{"single dot", ".", true},
		{"double dot", "..", true},
		{"hidden file", ".hidden", true},
		{"extension only", ".txt", true},
		{"deeply nested", "a/b/c/d/e/f/g/h/i/j.txt", true},
		{"multiple extensions", "file.backup.2024.tar.gz", true},
		{"numbers only", "123456789", true},
		{"mixed special", "file-name_2024.backup.tar.gz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidPath(tt.path)
			if got != tt.valid {
				t.Errorf("IsValidPath(%q) = %v, want %v", tt.path, got, tt.valid)
			}
		})
	}
}

func TestKeyTotalLengthValidation(t *testing.T) {
	// Create a namespace and path that together exceed MaxKeyLength
	namespace := strings.Repeat("a", MaxNamespaceLength)
	path := strings.Repeat("b", MaxPathLength)

	// This should be valid (exactly at limit)
	key, err := NewObjectKey(namespace, path)
	if err != nil {
		t.Errorf("NewObjectKey() with max lengths should succeed, got error: %v", err)
	}
	if key == nil {
		t.Fatal("NewObjectKey() returned nil key")
	}

	// Verify the total length is exactly at the limit
	totalLen := len(namespace) + 1 + len(path)
	if totalLen != MaxKeyLength {
		t.Errorf("Total length = %d, expected %d", totalLen, MaxKeyLength)
	}
}

func TestParseKeyWithInvalidObjectKey(t *testing.T) {
	// Test ParseKey with a key that passes initial checks but fails validation
	tests := []struct {
		name    string
		key     string
		wantErr bool
		errCode ErrorCode
	}{
		{
			name:    "path starting with slash after namespace",
			key:     "namespace:/invalid/path",
			wantErr: true,
			errCode: ErrCodePathInvalid,
		},
		{
			name:    "namespace with invalid first char",
			key:     "-namespace:valid/path",
			wantErr: true,
			errCode: ErrCodeNamespaceInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKey(tt.key)
			if !tt.wantErr {
				if err != nil {
					t.Errorf("ParseKey() unexpected error = %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("ParseKey() expected error, got nil")
				return
			}

			var ce *CatalogError
			if !errors.As(err, &ce) {
				t.Errorf("ParseKey() error is not CatalogError: %T", err)
				return
			}
			if ce.Code != tt.errCode {
				t.Errorf("ParseKey() error code = %v, want %v", ce.Code, tt.errCode)
			}
		})
	}
}

func TestIndexKeyRoundtrip(t *testing.T) {
	// Test that IndexKey and ParseIndexKey work together correctly
	original := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/report.pdf",
	}
	indexType := IndexTypeStore
	indexValue := "s3-primary"

	indexKey := original.IndexKey(indexType, indexValue)
	parsed, parsedValue, err := ParseIndexKey(indexKey)
	if err != nil {
		t.Fatalf("ParseIndexKey() error = %v", err)
	}

	if parsed.Namespace != original.Namespace {
		t.Errorf("Namespace roundtrip failed: got %v, want %v", parsed.Namespace, original.Namespace)
	}
	if parsed.Path != original.Path {
		t.Errorf("Path roundtrip failed: got %v, want %v", parsed.Path, original.Path)
	}
	if parsedValue != indexValue {
		t.Errorf("IndexValue roundtrip failed: got %v, want %v", parsedValue, indexValue)
	}
}
