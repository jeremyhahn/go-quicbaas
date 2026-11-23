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
	"context"
)

// bindingVoid is what SettleBindingWrite leaves in an empty binding slot. It
// decodes as a ProjectBinding with no ProjectID, so every reader already
// treats the slot as holding no binding for any project, and its bytes
// differ from an empty slot, so a pending PutIfAbsent can never apply over it.
type bindingVoid struct {
	VoidToken string `json:"void_token"`
}

// SettleBindingWrite decides the outcome of a PutBinding that returned an
// error, so that a write still pending in the backend can never apply later.
//
// A binding write is a compare-and-swap on the exact bytes it read. If the
// slot carries writeToken, the write landed and landed is true. Otherwise the
// slot is moved to bytes the attempt cannot have read: an empty slot (or a
// void marker) gets a fresh void marker, and this project's record gets the
// same record with a fresh WriteToken. Once that write applies, the attempt
// can no longer apply, and landed is false. A slot that holds another
// project's record (a key digest collision) is refused with
// *VersionConflictError and left alone.
//
// Re-stamping a held record is safe because the bind lease keeps every other
// bind of this project out while the caller settles, and the bind that wrote
// the held record has finished, so nothing still withdraws by its old token.
func (s *Store) SettleBindingWrite(ctx context.Context, tenantID, projectID, writeToken string) (landed bool, err error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return false, err
	}
	if writeToken == "" {
		return false, &InvalidInputError{Field: "write_token", Message: "settling a write needs the token of that write"}
	}
	store, err := s.guardedKV(ctx, "SettleBindingWrite", tenantID)
	if err != nil {
		return false, err
	}
	key := bindingKey(projectID)
	for attempt := 0; attempt < fenceWriteAttempts; attempt++ {
		held, raw, err := loadRecord[ProjectBinding](ctx, "SettleBindingWrite", store, key)
		if err != nil {
			return false, err
		}
		if held != nil && held.ProjectID == projectID && held.WriteToken == writeToken {
			return true, nil
		}
		nonce, err := newNonce()
		if err != nil {
			return false, backendError("SettleBindingWrite.token", err)
		}
		var next any
		switch {
		case held == nil || held.ProjectID == "":
			next = &bindingVoid{VoidToken: nonce}
		case held.ProjectID == projectID:
			restamped := *held
			restamped.WriteToken = nonce
			restamped.ID = entityIDFor(projectID)
			next = &restamped
		default:
			return false, &VersionConflictError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
		}
		applied, err := storeRecord(ctx, "SettleBindingWrite", store, key, raw, next)
		if err != nil {
			return false, err
		}
		if applied {
			return false, nil
		}
		if ctx.Err() != nil {
			return false, backendError("SettleBindingWrite", ctx.Err())
		}
		// The slot moved, perhaps because the pending write just committed;
		// the next round reads it again and sees the token if so.
	}
	return false, &VersionConflictError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
}
