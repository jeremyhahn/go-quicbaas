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

// Package catalog provides the internal catalog for QuicBaaS's object storage,
// tracking object metadata, locations, replicas, and policies.
package catalog

import (
	"strings"
	"unicode"
)

// Key format constants.
const (
	// KeySeparator separates namespace from path in object keys.
	KeySeparator = ":"

	// MaxKeyLength is the maximum total length of an object key.
	MaxKeyLength = 1024

	// MaxNamespaceLength is the maximum length of a namespace.
	MaxNamespaceLength = 128

	// MaxPathLength is the maximum length of a path (MaxKeyLength - MaxNamespaceLength - separator).
	MaxPathLength = 895
)

// Storage key prefixes for PebbleDB.
const (
	// PrefixCatalog is the prefix for catalog entries.
	PrefixCatalog = "cat:"

	// PrefixIndex is the prefix for secondary indexes.
	PrefixIndex = "idx:"

	// IndexTypeStore is the index type for objects by backend store.
	IndexTypeStore = "store"

	// IndexTypeTenant is the index type for objects by tenant.
	IndexTypeTenant = "tenant"

	// IndexTypeChecksum is the index type for deduplication.
	IndexTypeChecksum = "checksum"

	// IndexTypeStatus is the index type for objects by status.
	IndexTypeStatus = "status"

	// IndexTypeTag is the index type for objects by tag.
	IndexTypeTag = "tag"

	// IndexTypeEncryption is the index type for objects by encryption key.
	IndexTypeEncryption = "encryption"
)

// ObjectKey represents a parsed object storage key.
// Format: {namespace}:{path}
// Example: "tenant-acme:documents/report.pdf".
type ObjectKey struct {
	// Namespace identifies the logical grouping (e.g., "backups", "tenant-acme").
	Namespace string

	// Path is the object path within the namespace (e.g., "daily/2024-01-15.tar.gz").
	Path string
}

// NewObjectKey creates a new ObjectKey with validation.
func NewObjectKey(namespace, path string) (*ObjectKey, error) {
	key := &ObjectKey{
		Namespace: namespace,
		Path:      path,
	}
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return key, nil
}

// ParseKey parses a key string into namespace and path components.
func ParseKey(key string) (*ObjectKey, error) {
	if key == "" {
		return nil, NewKeyEmptyError()
	}

	if len(key) > MaxKeyLength {
		return nil, NewKeyTooLongError(len(key), MaxKeyLength)
	}

	idx := strings.Index(key, KeySeparator)
	if idx == -1 {
		return nil, NewKeyInvalidError(key, "missing separator ':'")
	}

	if idx == 0 {
		return nil, NewNamespaceEmptyError()
	}

	namespace := key[:idx]
	path := key[idx+1:]

	if path == "" {
		return nil, NewPathEmptyError()
	}

	objKey := &ObjectKey{
		Namespace: namespace,
		Path:      path,
	}

	if err := objKey.Validate(); err != nil {
		return nil, err
	}

	return objKey, nil
}

// String returns the full key string in format "namespace:path".
func (k *ObjectKey) String() string {
	return k.Namespace + KeySeparator + k.Path
}

// Validate validates the key components.
func (k *ObjectKey) Validate() error {
	if k.Namespace == "" {
		return NewNamespaceEmptyError()
	}

	if k.Path == "" {
		return NewPathEmptyError()
	}

	if len(k.Namespace) > MaxNamespaceLength {
		return NewNamespaceInvalidError(k.Namespace, "exceeds maximum length of 128 characters")
	}

	if len(k.Path) > MaxPathLength {
		return NewPathInvalidError(k.Path, "exceeds maximum length of 895 characters")
	}

	totalLen := len(k.Namespace) + 1 + len(k.Path) // +1 for separator
	if totalLen > MaxKeyLength {
		return NewKeyTooLongError(totalLen, MaxKeyLength)
	}

	if !IsValidNamespace(k.Namespace) {
		return NewNamespaceInvalidError(k.Namespace, "must be alphanumeric with hyphens/underscores, starting with letter or digit")
	}

	if !IsValidPath(k.Path) {
		return NewPathInvalidError(k.Path, "must not start/end with slash, no consecutive slashes, no control characters")
	}

	return nil
}

// IsValidNamespace checks if namespace is valid.
// Namespaces must be alphanumeric with hyphens and underscores allowed.
// Must start with a letter or number, cannot be empty.
func IsValidNamespace(ns string) bool {
	if ns == "" {
		return false
	}

	for i, r := range ns {
		if i == 0 {
			// First character must be alphanumeric
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		} else {
			// Subsequent characters can be alphanumeric, hyphen, or underscore
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
				return false
			}
		}
	}

	return true
}

// IsValidPath checks if path is valid.
// Paths can contain alphanumeric characters, hyphens, underscores, dots, and forward slashes.
// Paths cannot start or end with a slash, cannot contain consecutive slashes,
// and cannot contain control characters.
func IsValidPath(path string) bool {
	if path == "" {
		return false
	}

	// Cannot start or end with slash
	if strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}

	// Cannot contain consecutive slashes
	if strings.Contains(path, "//") {
		return false
	}

	for _, r := range path {
		// Check for control characters
		if unicode.IsControl(r) {
			return false
		}

		// Allow alphanumeric, hyphen, underscore, dot, and forward slash
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '-' && r != '_' && r != '.' && r != '/' {
			return false
		}
	}

	return true
}

// CatalogKey returns the PebbleDB key for catalog storage.
// Format: "cat:{namespace}:{path}".
func (k *ObjectKey) CatalogKey() []byte {
	return []byte(PrefixCatalog + k.String())
}

// IndexKey returns an index key for a given index type and value.
// Format: "idx:{indexType}:{indexValue}:{namespace}:{path}".
func (k *ObjectKey) IndexKey(indexType, indexValue string) []byte {
	return []byte(PrefixIndex + indexType + KeySeparator + indexValue + KeySeparator + k.String())
}

// StoreIndexKey returns the index key for the store index.
// Format: "idx:store:{store}:{namespace}:{path}".
func (k *ObjectKey) StoreIndexKey(store string) []byte {
	return k.IndexKey(IndexTypeStore, store)
}

// TenantIndexKey returns the index key for the tenant index.
// Format: "idx:tenant:{tenant}:{namespace}:{path}".
func (k *ObjectKey) TenantIndexKey(tenant string) []byte {
	return k.IndexKey(IndexTypeTenant, tenant)
}

// ChecksumIndexKey returns the index key for the checksum index.
// Format: "idx:checksum:{hex}:{namespace}:{path}".
func (k *ObjectKey) ChecksumIndexKey(checksumHex string) []byte {
	return k.IndexKey(IndexTypeChecksum, checksumHex)
}

// StatusIndexKey returns the index key for the status index.
// Format: "idx:status:{status}:{namespace}:{path}".
func (k *ObjectKey) StatusIndexKey(status string) []byte {
	return k.IndexKey(IndexTypeStatus, status)
}

// TagIndexKey returns the index key for a tag.
// Format: "idx:tag:{tag}:{value}:{namespace}:{path}".
func (k *ObjectKey) TagIndexKey(tag, value string) []byte {
	return []byte(PrefixIndex + IndexTypeTag + KeySeparator + tag + KeySeparator + value + KeySeparator + k.String())
}

// EncryptionIndexKey returns the index key for the encryption key index.
// Format: "idx:encryption:{keyid}:{namespace}:{path}".
func (k *ObjectKey) EncryptionIndexKey(keyID string) []byte {
	return k.IndexKey(IndexTypeEncryption, keyID)
}

// ParseCatalogKey parses a PebbleDB catalog key back into an ObjectKey.
// Input format: "cat:{namespace}:{path}".
func ParseCatalogKey(catalogKey []byte) (*ObjectKey, error) {
	s := string(catalogKey)

	if !strings.HasPrefix(s, PrefixCatalog) {
		return nil, NewKeyInvalidError(s, "missing catalog prefix 'cat:'")
	}

	// Remove the "cat:" prefix
	objectKeyStr := s[len(PrefixCatalog):]

	return ParseKey(objectKeyStr)
}

// ExtractNamespaceFromKey extracts the namespace from a raw key string.
// Returns empty string if the key format is invalid.
func ExtractNamespaceFromKey(key string) string {
	idx := strings.Index(key, KeySeparator)
	if idx <= 0 {
		return ""
	}
	return key[:idx]
}

// ExtractPathFromKey extracts the path from a raw key string.
// Returns empty string if the key format is invalid.
func ExtractPathFromKey(key string) string {
	idx := strings.Index(key, KeySeparator)
	if idx < 0 || idx >= len(key)-1 {
		return ""
	}
	return key[idx+1:]
}

// BuildIndexPrefix builds a prefix for scanning an index by type and value.
// Format: "idx:{indexType}:{indexValue}:".
func BuildIndexPrefix(indexType, indexValue string) []byte {
	return []byte(PrefixIndex + indexType + KeySeparator + indexValue + KeySeparator)
}

// BuildTagIndexPrefix builds a prefix for scanning a tag index.
// Format: "idx:tag:{tag}:{value}:".
func BuildTagIndexPrefix(tag, value string) []byte {
	return []byte(PrefixIndex + IndexTypeTag + KeySeparator + tag + KeySeparator + value + KeySeparator)
}

// BuildCatalogPrefix builds a prefix for scanning catalog entries by namespace.
// Format: "cat:{namespace}:".
func BuildCatalogPrefix(namespace string) []byte {
	return []byte(PrefixCatalog + namespace + KeySeparator)
}

// BuildAllCatalogPrefix returns the prefix for scanning all catalog entries.
// Format: "cat:".
func BuildAllCatalogPrefix() []byte {
	return []byte(PrefixCatalog)
}

// ParseIndexKey parses an index key to extract the object key.
// Input format: "idx:{indexType}:{indexValue}:{namespace}:{path}"
// Returns the ObjectKey and the index value.
func ParseIndexKey(indexKey []byte) (*ObjectKey, string, error) {
	s := string(indexKey)

	if !strings.HasPrefix(s, PrefixIndex) {
		return nil, "", NewKeyInvalidError(s, "missing index prefix 'idx:'")
	}

	// Remove "idx:" prefix
	remainder := s[len(PrefixIndex):]

	// Find first colon to get index type
	firstColon := strings.Index(remainder, KeySeparator)
	if firstColon == -1 {
		return nil, "", NewKeyInvalidError(s, "missing index type")
	}

	// Find second colon to get index value
	afterType := remainder[firstColon+1:]
	secondColon := strings.Index(afterType, KeySeparator)
	if secondColon == -1 {
		return nil, "", NewKeyInvalidError(s, "missing index value")
	}

	indexValue := afterType[:secondColon]

	// The rest is the object key (namespace:path)
	objectKeyStr := afterType[secondColon+1:]
	if objectKeyStr == "" {
		return nil, "", NewKeyInvalidError(s, "missing object key")
	}

	objKey, err := ParseKey(objectKeyStr)
	if err != nil {
		return nil, "", err
	}

	return objKey, indexValue, nil
}

// ParseTagIndexKey parses a tag index key to extract the object key, tag name, and tag value.
// Input format: "idx:tag:{tag}:{value}:{namespace}:{path}".
func ParseTagIndexKey(indexKey []byte) (*ObjectKey, string, string, error) {
	s := string(indexKey)

	expectedPrefix := PrefixIndex + IndexTypeTag + KeySeparator
	if !strings.HasPrefix(s, expectedPrefix) {
		return nil, "", "", NewKeyInvalidError(s, "missing tag index prefix 'idx:tag:'")
	}

	// Remove "idx:tag:" prefix
	remainder := s[len(expectedPrefix):]

	// Find first colon to get tag name
	firstColon := strings.Index(remainder, KeySeparator)
	if firstColon == -1 {
		return nil, "", "", NewKeyInvalidError(s, "missing tag name")
	}

	tagName := remainder[:firstColon]
	afterTag := remainder[firstColon+1:]

	// Find second colon to get tag value
	secondColon := strings.Index(afterTag, KeySeparator)
	if secondColon == -1 {
		return nil, "", "", NewKeyInvalidError(s, "missing tag value")
	}

	tagValue := afterTag[:secondColon]

	// The rest is the object key (namespace:path)
	objectKeyStr := afterTag[secondColon+1:]
	if objectKeyStr == "" {
		return nil, "", "", NewKeyInvalidError(s, "missing object key")
	}

	objKey, err := ParseKey(objectKeyStr)
	if err != nil {
		return nil, "", "", err
	}

	return objKey, tagName, tagValue, nil
}
