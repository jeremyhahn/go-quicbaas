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

package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestNetworkListenerLogLines(t *testing.T) {
	const (
		httpLine = "HTTP server listening on 0.0.0.0:8443"
		grpcLine = "gRPC server listening on 0.0.0.0:19443"
	)
	tests := []struct {
		name        string
		restEnabled bool
		grpcEnabled bool
		want        []string
	}{
		{name: "grpc disabled, rest enabled", restEnabled: true, want: []string{httpLine}},
		{name: "grpc enabled, rest disabled", grpcEnabled: true, want: []string{grpcLine}},
		{name: "both enabled", restEnabled: true, grpcEnabled: true, want: []string{httpLine, grpcLine}},
		{name: "neither enabled", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.Host = "0.0.0.0"
			cfg.Server.REST.Enabled = tt.restEnabled
			cfg.Server.REST.Port = 8443
			cfg.Server.GRPC.Enabled = tt.grpcEnabled
			cfg.Server.GRPC.Port = 19443

			got := networkListenerLogLines(cfg)
			if len(tt.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("networkListenerLogLines() = %q, want empty", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("networkListenerLogLines() = %q, want %q", got, tt.want)
			}
			if !tt.grpcEnabled {
				for _, line := range got {
					if strings.Contains(line, "gRPC") {
						t.Fatalf("line %q mentions gRPC while server.grpc.enabled is false", line)
					}
				}
			}
		})
	}
}
