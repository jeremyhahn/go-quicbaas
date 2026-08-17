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

package quicbaas

import "github.com/jeremyhahn/go-quicbaas/pkg/apimodule"

// go-quicbaas's qrdb ServiceModule surface, re-exported so downstream hosts
// (go-trusted-platform) that embed a qrdb node can register the quicbaas
// broker capability plane via qrdbsdk.WithModule without importing
// go-quicbaas/pkg. The stack-wide convention is one module per provider
// project, named after the project; quicbaas's registers as
// ServiceModuleName ("quicbaas").

// ServiceModuleName is the unique name quicbaas's module registers under.
const ServiceModuleName = apimodule.ModuleName

// ServiceModule is go-quicbaas's broker capability plane (Scheduler,
// Orchestrator, Project, Account, SSH, Messaging, Embedding) as a qrdb
// ServiceModule.
type ServiceModule = apimodule.Module

// ServiceModuleOption configures optional ServiceModule behavior at
// construction time.
type ServiceModuleOption = apimodule.Option

// ServiceModuleResolver returns the composed service.QuicBaaS facade, or nil
// while the host is still composing it; the module binds lazily on the first
// RPC.
type ServiceModuleResolver = apimodule.ServiceResolver

// NewServiceModule builds go-quicbaas's ServiceModule around the QuicBaaS
// facade resolver. A nil resolver or logger is a composition error and fails
// construction.
var NewServiceModule = apimodule.NewModule

// WithServiceModuleNodeInfo supplies the node identity (nodeID, version) the
// module's Embedding facade constructor requires.
var WithServiceModuleNodeInfo = apimodule.WithNodeInfo
