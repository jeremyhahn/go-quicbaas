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

import (
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The ServiceModule alias must satisfy the qrdb ServiceModule contract so a
// downstream host can pass it straight to qrdbsdk.WithModule.
var _ qrdbsdk.ServiceModule = (*ServiceModule)(nil)

// TestNewServiceModuleReExport_ConstructsNamedModule proves the SDK re-export
// builds the broker module without importing go-quicbaas/pkg/apimodule at the
// consumer call site, registering under the stack-wide module name.
func TestNewServiceModuleReExport_ConstructsNamedModule(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}

	var resolver ServiceModuleResolver = func() service.QuicBaaS { return nil }
	module, err := NewServiceModule(resolver, log, WithServiceModuleNodeInfo(1, "test"))
	if err != nil {
		t.Fatalf("NewServiceModule: %v", err)
	}
	if module.Name() != ServiceModuleName {
		t.Fatalf("module name = %q, want %q", module.Name(), ServiceModuleName)
	}
	if ServiceModuleName != "quicbaas" {
		t.Fatalf("ServiceModuleName = %q, want %q", ServiceModuleName, "quicbaas")
	}
}

// TestNewServiceModuleReExport_NilResolverFailsClosed proves the construction
// error path is reachable through the re-export: a nil resolver must refuse
// to build a dead module surface.
func TestNewServiceModuleReExport_NilResolverFailsClosed(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	if _, err := NewServiceModule(nil, log); err == nil {
		t.Fatal("NewServiceModule with a nil resolver must fail")
	}
}
