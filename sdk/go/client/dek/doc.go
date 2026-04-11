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

// Package dek provides a client for managing Data Encryption Keys (DEKs)
// with hierarchical path-based namespacing and multi-tenant isolation.
//
// DEKs are used for envelope encryption where data is encrypted with a
// DEK and the DEK itself is encrypted with a master key managed by the
// KMS service.
package dek
