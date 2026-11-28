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

//go:build integration

package helpers

import (
	"errors"
	"strings"
	"testing"
)

// TestStandaloneVerdict pins the rule IsStandaloneMode answers by: one node
// that answers decides the mode, and a run in which no node answered is an
// error, never "standalone", so a caller that returns early on standalone
// cannot pass without asserting anything.
func TestStandaloneVerdict(t *testing.T) {
	errConnect := errors.New("dial tcp: connection refused")
	errStatus := errors.New("rpc error: unavailable")

	tests := []struct {
		name       string
		probes     []standaloneProbe
		standalone bool
		wantErr    bool
		errHas     []string
	}{
		{
			name:   "one answer with a leader",
			probes: []standaloneProbe{{endpoint: "n1:9000", hasLeader: true}},
		},
		{
			name: "an error then an answer with a leader",
			probes: []standaloneProbe{
				{endpoint: "n1:9000", err: errConnect},
				{endpoint: "n2:9000", hasLeader: true},
			},
		},
		{
			name: "every node answers with no leader",
			probes: []standaloneProbe{
				{endpoint: "n1:9000"},
				{endpoint: "n2:9000"},
				{endpoint: "n3:9000"},
			},
			standalone: true,
		},
		{
			name: "an error then an answer with no leader",
			probes: []standaloneProbe{
				{endpoint: "n1:9000", err: errStatus},
				{endpoint: "n2:9000"},
			},
			standalone: true,
		},
		{
			name: "every probe errored",
			probes: []standaloneProbe{
				{endpoint: "n1:9000", err: errConnect},
				{endpoint: "n2:9000", err: errStatus},
			},
			wantErr: true,
			errHas:  []string{"n1:9000", errConnect.Error(), "n2:9000", errStatus.Error()},
		},
		{
			name:    "no probes",
			probes:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			standalone, err := standaloneVerdict(tt.probes)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("standaloneVerdict() = %v, nil; want an error", standalone)
				}
				if standalone {
					t.Errorf("standaloneVerdict() reported standalone alongside error %v", err)
				}
				for _, s := range tt.errHas {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("error %q does not name %q", err, s)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("standaloneVerdict() error = %v; want nil", err)
			}
			if standalone != tt.standalone {
				t.Errorf("standaloneVerdict() = %v; want %v", standalone, tt.standalone)
			}
		})
	}
}
