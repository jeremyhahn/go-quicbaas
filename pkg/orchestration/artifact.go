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

package orchestration

import "errors"

// =============================================================================
// Artifact — Typed task output / input for cross-agent fan-out
// =============================================================================

// ArtifactKind classifies how the artifact's content is stored and referenced.
type ArtifactKind string

const (
	// ArtifactKindInline embeds the artifact bytes directly in Content.
	// Use for small blobs (≤ InlineLimit).
	ArtifactKindInline ArtifactKind = "inline"

	// ArtifactKindFile references a file on a specific agent's filesystem.
	// Ref.AgentID and Ref.Path are required.
	ArtifactKindFile ArtifactKind = "file"

	// ArtifactKindCommit references a git commit SHA in a repository.
	// Ref.SHA and Ref.Branch are set; Ref.Path optionally narrows the scope.
	ArtifactKindCommit ArtifactKind = "commit"

	// ArtifactKindKVKey references a key in the QuicBaaS KV store.
	// Large inline artifacts are promoted to this kind automatically when
	// they exceed the configured InlineLimit.
	ArtifactKindKVKey ArtifactKind = "kv_key"

	// ArtifactKindURL references an arbitrary external resource (e.g., S3 object).
	// Ref.Path holds the full URL.
	ArtifactKindURL ArtifactKind = "url"
)

// validArtifactKinds is the O(1) set used by the validator and map-based
// kind dispatch to reject unknown kinds fast.
var validArtifactKinds = map[ArtifactKind]struct{}{
	ArtifactKindInline: {},
	ArtifactKindFile:   {},
	ArtifactKindCommit: {},
	ArtifactKindKVKey:  {},
	ArtifactKindURL:    {},
}

// InlineLimit is the default maximum size (bytes) at which an artifact is
// stored inline in the task record.  Callers may override this at
// service-construction time via ArtifactConfig.InlineLimit.
const InlineLimit int64 = 1 << 20 // 1 MiB

// ArtifactRef locates large or external artifact content.
type ArtifactRef struct {
	// AgentID is required for file-kind artifacts to route fetch requests.
	AgentID string `json:"agent_id,omitempty"`

	// Path is the filesystem path, KV key, or full URL depending on Kind.
	Path string `json:"path,omitempty"`

	// SHA is the content hash (any) for file/kv_key kinds, or the git
	// commit SHA for commit-kind artifacts.
	SHA string `json:"sha,omitempty"`

	// Branch is the git branch for commit-kind artifacts.
	Branch string `json:"branch,omitempty"`
}

// Artifact is a typed, named output or input attached to a task.
// Small blobs are stored inline (Kind=inline, Content set). Blobs larger
// than the configured InlineLimit are promoted to KV storage and the task
// record carries only the Ref (Kind=kv_key, Ref.Path is the KV key).
type Artifact struct {
	// Kind classifies the artifact's content location.
	Kind ArtifactKind `json:"kind"`

	// Name uniquely identifies the artifact within a task (required).
	Name string `json:"name"`

	// MIMEType describes the content type (e.g., "text/plain", "application/json").
	MIMEType string `json:"mime_type,omitempty"`

	// Size is the byte count of the artifact content.
	Size int64 `json:"size,omitempty"`

	// Content holds inline bytes when Kind=inline.
	Content []byte `json:"content,omitempty"`

	// Ref locates the artifact for non-inline kinds.
	Ref ArtifactRef `json:"ref,omitempty"`

	// Metadata holds arbitrary key-value pairs for downstream consumers.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// =============================================================================
// Artifact Sentinel Errors
// =============================================================================

var (
	// ErrArtifactTooLarge indicates the artifact payload exceeded the configured InlineLimit.
	ErrArtifactTooLarge = errors.New("orchestration: artifact too large")

	// ErrArtifactKindInvalid indicates an unknown or unsupported ArtifactKind was supplied.
	ErrArtifactKindInvalid = errors.New("orchestration: artifact kind invalid")

	// ErrArtifactRefInvalid indicates the ArtifactRef is missing required fields for its Kind.
	ErrArtifactRefInvalid = errors.New("orchestration: artifact ref invalid")

	// ErrArtifactNotFound indicates a named artifact was not found on the task.
	ErrArtifactNotFound = errors.New("orchestration: artifact not found")
)

// =============================================================================
// Artifact Typed Errors
// =============================================================================

// ArtifactTooLargeError is returned when an artifact's content exceeds InlineLimit.
type ArtifactTooLargeError struct {
	Name  string
	Size  int64
	Limit int64
}

// Error implements the error interface.
func (e *ArtifactTooLargeError) Error() string {
	return ErrArtifactTooLarge.Error() + ": artifact=" + e.Name +
		" size=" + itoa64(e.Size) + " limit=" + itoa64(e.Limit)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ArtifactTooLargeError) Unwrap() error { return ErrArtifactTooLarge }

// ArtifactKindInvalidError is returned when an unknown ArtifactKind is supplied.
type ArtifactKindInvalidError struct {
	Kind ArtifactKind
}

// Error implements the error interface.
func (e *ArtifactKindInvalidError) Error() string {
	return ErrArtifactKindInvalid.Error() + ": " + string(e.Kind)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ArtifactKindInvalidError) Unwrap() error { return ErrArtifactKindInvalid }

// ArtifactRefInvalidError is returned when ArtifactRef is missing required fields.
type ArtifactRefInvalidError struct {
	Kind    ArtifactKind
	Missing string
}

// Error implements the error interface.
func (e *ArtifactRefInvalidError) Error() string {
	return ErrArtifactRefInvalid.Error() + ": kind=" + string(e.Kind) + " missing=" + e.Missing
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ArtifactRefInvalidError) Unwrap() error { return ErrArtifactRefInvalid }

// ArtifactNotFoundError is returned when a named artifact is not found on a task.
type ArtifactNotFoundError struct {
	TaskID string
	Name   string
}

// Error implements the error interface.
func (e *ArtifactNotFoundError) Error() string {
	return ErrArtifactNotFound.Error() + ": task=" + e.TaskID + " artifact=" + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ArtifactNotFoundError) Unwrap() error { return ErrArtifactNotFound }

// =============================================================================
// Artifact validation
// =============================================================================

// validateArtifact returns an error if the artifact is missing required fields
// or carries an unsupported kind.
func validateArtifact(a *Artifact) error {
	if _, ok := validArtifactKinds[a.Kind]; !ok {
		return &ArtifactKindInvalidError{Kind: a.Kind}
	}

	if a.Name == "" {
		return &ArtifactRefInvalidError{Kind: a.Kind, Missing: "name"}
	}

	type refCheck struct {
		field string
		value string
	}

	// refRequired maps each non-inline kind to the ref fields it requires.
	refRequired := map[ArtifactKind][]refCheck{
		ArtifactKindFile:   {{"agent_id", a.Ref.AgentID}, {"path", a.Ref.Path}},
		ArtifactKindCommit: {{"sha", a.Ref.SHA}},
		ArtifactKindKVKey:  {{"path", a.Ref.Path}},
		ArtifactKindURL:    {{"path", a.Ref.Path}},
	}

	if checks, ok := refRequired[a.Kind]; ok {
		for _, c := range checks {
			if c.value == "" {
				return &ArtifactRefInvalidError{Kind: a.Kind, Missing: c.field}
			}
		}
	}

	return nil
}

// itoa64 converts an int64 to its decimal string representation without importing strconv.
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
