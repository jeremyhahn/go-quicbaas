//go:build !integration

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

package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Additional accessor coverage for the composition / IAM / lifecycle getters
// that the base accessor suite did not exercise. Each verifies the documented
// "returns nil / not-configured" contract on a freshly built service, plus the
// Set* writers prove the wired value is surfaced back through its accessor.
// =============================================================================

func TestQuicBaaS_TrustAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.Trust())
}

func TestQuicBaaS_ACMEAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.ACME())
}

func TestQuicBaaS_ProjectAccessor_AutoInitialized(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	// Project is auto-wired with an in-memory store during construction.
	assert.NotNil(t, svc.Project())
}

func TestQuicBaaS_AgentAdminAccessor_AutoInitialized(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	// AgentAdmin is auto-wired with an in-memory registry during construction.
	assert.NotNil(t, svc.AgentAdmin())
}

func TestQuicBaaS_AgentAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.Agent())
}

func TestQuicBaaS_SchedulerEngineAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.SchedulerEngine())
}

func TestQuicBaaS_SSHClientAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.SSHClient())
}

func TestQuicBaaS_DatabaseAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.Database())
}

func TestQuicBaaS_LoginCeremonyAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()
	assert.Nil(t, svc.LoginCeremony())
}

func TestQuicBaaS_SetLoginService_SetsServiceForCeremony(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)
	require.Nil(t, impl.LoginCeremony())

	// The concrete LoginService backed by a nil in-process client is a valid
	// LoginServicer (it fails closed), so it proves the writer surfaces the
	// wired value back through LoginCeremony() without a heavyweight mock.
	login := NewLoginService(nil)
	impl.SetLoginService(login)
	assert.Same(t, login, impl.LoginCeremony())
}

// =============================================================================
// IAM provider accessors — return nil / no error when no IAM provider is wired.
// Additional IAM provider accessors are covered in accessor_methods_iam_test.go.
// =============================================================================
