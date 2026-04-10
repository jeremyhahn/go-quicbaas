//go:build rag

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

package watcher

import (
	"time"
)

// EventType represents the type of file system event.
type EventType int

const (
	// EventCreate indicates a new file was created.
	EventCreate EventType = iota
	// EventModify indicates a file was modified.
	EventModify
	// EventDelete indicates a file was deleted.
	EventDelete
	// EventRename indicates a file was renamed.
	EventRename
)

// String returns a string representation of the event type.
func (et EventType) String() string {
	switch et {
	case EventCreate:
		return "create"
	case EventModify:
		return "modify"
	case EventDelete:
		return "delete"
	case EventRename:
		return "rename"
	default:
		return "unknown"
	}
}

// Event represents a file system event.
type Event struct {
	// Type is the type of event (create, modify, delete).
	Type EventType

	// Path is the file path that triggered the event.
	Path string

	// Timestamp is when the event occurred.
	Timestamp time.Time

	// Size is the file size in bytes (0 for delete events).
	Size int64

	// OldPath is the original path for rename events.
	OldPath string
}

// IsModification returns true if the event represents a file modification
// (create, modify, or rename).
func (e Event) IsModification() bool {
	return e.Type == EventCreate || e.Type == EventModify || e.Type == EventRename
}

// IsDeletion returns true if the event represents a file deletion.
func (e Event) IsDeletion() bool {
	return e.Type == EventDelete
}
