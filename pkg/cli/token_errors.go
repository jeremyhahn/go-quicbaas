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

package cli

// Stored-session error types. They are distinct so a caller can tell "there is
// no session here" (run `quicbaas auth login`) from "the session is over"
// (log in again) from "renewal failed" (server/network problem).

// StoredSessionUnavailableError reports that no readable session envelope
// exists at the given path.
type StoredSessionUnavailableError struct {
	Path  string
	Cause error
}

func (e *StoredSessionUnavailableError) Error() string {
	return "cli: no usable session token at " + e.Path + ": " + e.Cause.Error()
}

func (e *StoredSessionUnavailableError) Unwrap() error { return e.Cause }

// StoredSessionExpiredError reports that the stored access token has expired
// and cannot be renewed (no refresh credential, or renewal is disabled).
type StoredSessionExpiredError struct {
	Path string
}

func (e *StoredSessionExpiredError) Error() string {
	return "cli: stored session at " + e.Path + " has expired; run `quicbaas auth login`"
}

// StoredSessionRefreshFailedError reports that renewal was attempted and the
// server or the local store rejected it.
type StoredSessionRefreshFailedError struct {
	Path  string
	Cause error
}

func (e *StoredSessionRefreshFailedError) Error() string {
	return "cli: failed to renew session at " + e.Path + ": " + e.Cause.Error()
}

func (e *StoredSessionRefreshFailedError) Unwrap() error { return e.Cause }
