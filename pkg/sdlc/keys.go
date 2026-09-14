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

package sdlc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"path/filepath"
	"strings"
)

// =============================================================================
// Key layout
// =============================================================================
//
// The module's tenant-owned records go through the qrdb DAO on a store the
// tenant resolver already confined, so no tenant appears in any key here. The
// one system-plane record — the webhook route, projectID → tenantID — lives
// under the platform namespace because a webhook delivery carries no tenant
// and has to be routed to one.

const (
	keySeparator = "/"

	// systemNamespace prefixes platform-owned records.
	systemNamespace = "__sys:"

	// webhookRouteKeyspace holds projectID → tenantID for webhook routing.
	webhookRouteKeyspace = systemNamespace + "sdlc/webhookroute/"
)

// ErrInvalidKeySegment indicates an identifier that cannot form a key segment.
var ErrInvalidKeySegment = errors.New("sdlc store: invalid key segment")

// KeySegmentError reports an identifier that cannot safely form a key segment.
type KeySegmentError struct {
	Field   string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *KeySegmentError) Error() string {
	return ErrInvalidKeySegment.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *KeySegmentError) Unwrap() error { return ErrInvalidKeySegment }

// validateKeySegment rejects identifiers that would break a key layout or a
// filesystem path derived from it.
func validateKeySegment(field, value string) error {
	if value == "" {
		return &KeySegmentError{Field: field, Value: value, Message: "must not be empty"}
	}
	if strings.Contains(value, keySeparator) {
		return &KeySegmentError{Field: field, Value: value, Message: "must not contain " + keySeparator}
	}
	if value == "." || value == ".." {
		return &KeySegmentError{Field: field, Value: value, Message: "path-element names cannot be identifiers"}
	}
	if strings.ContainsAny(value, "/\\\x00") {
		return &KeySegmentError{Field: field, Value: value, Message: "must not contain path separators or NUL"}
	}
	if cleaned := filepath.Clean(value); cleaned != value {
		return &KeySegmentError{Field: field, Value: value, Message: "must survive path cleaning unchanged"}
	}
	return nil
}

// SegmentHash returns a fixed-width, path-safe digest of value for use where
// a caller-supplied string must appear in a key or a path.
func SegmentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// entityIDFor derives the DAO entity id of a record from the orchestration id
// it references, so a binding is addressed by its Project and a delivery by
// its Task with no secondary index. The record also carries the referenced id
// in full, and every read compares the two, so a digest collision is refused
// rather than answered with someone else's record.
func entityIDFor(ref string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(ref))
	id := h.Sum64()
	if id == 0 {
		// The DAO treats zero as "unset" and would assign a fresh id.
		return 1
	}
	return id
}

// WebhookRouteKey addresses the system-plane record that routes a webhook
// delivery for projectID to the tenant that bound it.
func WebhookRouteKey(projectID string) (string, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return "", err
	}
	return webhookRouteKeyspace + projectID, nil
}
