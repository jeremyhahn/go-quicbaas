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

// Package tenantagent appoints and retires a tenant's Tenant Agent: the
// tenant-level agent that coordinates its tenant.
//
// An appointment binds a lead-capable catalog Agent or pool to a go-qrdb
// certificate principal holding a tenant-scoped role, and to a go-xkms
// signing key the Tenant Agent signs with. Every operation names one tenant
// and never reads or writes another.
//
// go-quicbaas keeps no identity, role or credential model of its own for the
// Tenant Agent: the principal and its role live in go-qrdb, the key in
// go-xkms, and the appointment record holds only their names.
package tenantagent
