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

//go:build !iam

package service

// IAMService is a stub interface when IAM is not enabled.
// This allows the service package to compile without the iam build tag.
// The actual interface is defined in iam_service.go when iam is set.
type IAMService interface{}

// IAMComponents is a stub interface when IAM is not enabled.
type IAMComponents interface{}

// IAMServiceProvider is a stub interface when IAM is not enabled.
// The actual interface is defined in iam_interfaces.go when iam is set.
type IAMServiceProvider interface{}
