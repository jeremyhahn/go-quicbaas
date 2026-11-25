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

// Package notify delivers operator notifications over several channels.
//
// Signal is the preferred, end-to-end encrypted channel and is always tried
// first. Slack is supported for teams that use it. A notification marked
// Sensitive never carries its body over a channel that is not end-to-end
// encrypted: such a channel receives the subject, severity and link, and a
// pointer to where the details went.
//
// The package imports only the standard library, so any go-quicbaas package
// can depend on it without an import cycle.
package notify
