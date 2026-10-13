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

package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// captureServerLog points srv's logger at a JSON buffer the test can read.
func captureServerLog(t *testing.T, srv *Server) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	log, err := qrdbsdk.NewLoggerWithHandler(h, logger.LogConfig{Level: "debug"})
	require.NoError(t, err)
	srv.logger = log
	return &buf
}

// selfRegWarnings returns the captured log lines that carry the
// self-service registration warning.
func selfRegWarnings(buf *bytes.Buffer) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, selfServiceRegistrationWarning) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestWarnSelfServiceRegistrationPosture(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		approval bool
		want     []string
	}{
		{
			name:     "enabled with approval",
			enabled:  true,
			approval: true,
			want:     []string{`"level":"WARN"`, `"flag":"iam.self_service_registration.enabled"`, `"require_admin_approval":true`},
		},
		{
			name:     "enabled without approval",
			enabled:  true,
			approval: false,
			want:     []string{`"level":"WARN"`, `"require_admin_approval":false`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newModuleHostTestServer(t)
			buf := captureServerLog(t, srv)
			srv.config.IAM.SelfServiceRegistration.Enabled = tt.enabled
			srv.config.IAM.SelfServiceRegistration.RequireAdminApproval = tt.approval

			srv.warnSelfServiceRegistrationPosture()

			warnings := selfRegWarnings(buf)
			require.Len(t, warnings, 1)
			for _, field := range tt.want {
				assert.Contains(t, warnings[0], field)
			}
		})
	}
}

func TestWarnSelfServiceRegistrationPosture_SilentWhenDisabled(t *testing.T) {
	srv := newModuleHostTestServer(t)
	buf := captureServerLog(t, srv)

	srv.warnSelfServiceRegistrationPosture()

	assert.Empty(t, selfRegWarnings(buf))
}

// TestUnifiedModuleServerOptions_WarnsWhenSelfRegistrationOpen pins the
// wiring: the warning is emitted where the flag reaches the xkms module, and
// only when the flag is on.
func TestUnifiedModuleServerOptions_WarnsWhenSelfRegistrationOpen(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		srv := newModuleHostTestServer(t)
		buf := captureServerLog(t, srv)
		srv.config.IAM.SelfServiceRegistration.Enabled = enabled

		_, err := srv.unifiedModuleServerOptions(nil)
		require.NoError(t, err)

		if enabled {
			assert.Len(t, selfRegWarnings(buf), 1)
		} else {
			assert.Empty(t, selfRegWarnings(buf))
		}
	}
}
