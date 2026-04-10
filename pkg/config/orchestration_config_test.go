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

package config

import (
	"testing"
	"time"
)

func TestOrchestrationConfig_Defaults(t *testing.T) {
	cfg := Default()

	if !cfg.Orchestration.Enabled {
		t.Error("Default OrchestrationConfig.Enabled = false, want true")
	}
	if cfg.Orchestration.HeartbeatTimeout != 90*time.Second {
		t.Errorf("Default OrchestrationConfig.HeartbeatTimeout = %v, want 90s",
			cfg.Orchestration.HeartbeatTimeout)
	}
	if cfg.Orchestration.ClaudeTeamMode {
		t.Error("Default OrchestrationConfig.ClaudeTeamMode = true, want false")
	}
	if cfg.Orchestration.OllamaChatModel != "" {
		t.Errorf("Default OrchestrationConfig.OllamaChatModel = %q, want empty string",
			cfg.Orchestration.OllamaChatModel)
	}
}

func TestOrchestrationConfig_OllamaChatModel(t *testing.T) {
	cfg := Default()
	cfg.Orchestration.OllamaChatModel = "llama3"

	if cfg.Orchestration.OllamaChatModel != "llama3" {
		t.Errorf("OrchestrationConfig.OllamaChatModel = %q, want %q",
			cfg.Orchestration.OllamaChatModel, "llama3")
	}
}

func TestOrchestrationConfig_CustomValues(t *testing.T) {
	cfg := OrchestrationConfig{
		Enabled:          false,
		HeartbeatTimeout: 120 * time.Second,
		ClaudeTeamMode:   true,
		OllamaChatModel:  "codellama",
	}

	if cfg.Enabled {
		t.Error("OrchestrationConfig.Enabled = true, want false")
	}
	if cfg.HeartbeatTimeout != 120*time.Second {
		t.Errorf("OrchestrationConfig.HeartbeatTimeout = %v, want 120s",
			cfg.HeartbeatTimeout)
	}
	if !cfg.ClaudeTeamMode {
		t.Error("OrchestrationConfig.ClaudeTeamMode = false, want true")
	}
	if cfg.OllamaChatModel != "codellama" {
		t.Errorf("OrchestrationConfig.OllamaChatModel = %q, want %q",
			cfg.OllamaChatModel, "codellama")
	}
}

func TestOrchestrationConfig_ZeroValue(t *testing.T) {
	var cfg OrchestrationConfig

	if cfg.Enabled {
		t.Error("zero-value OrchestrationConfig.Enabled = true, want false")
	}
	if cfg.HeartbeatTimeout != 0 {
		t.Errorf("zero-value OrchestrationConfig.HeartbeatTimeout = %v, want 0",
			cfg.HeartbeatTimeout)
	}
	if cfg.ClaudeTeamMode {
		t.Error("zero-value OrchestrationConfig.ClaudeTeamMode = true, want false")
	}
	if cfg.OllamaChatModel != "" {
		t.Errorf("zero-value OrchestrationConfig.OllamaChatModel = %q, want empty",
			cfg.OllamaChatModel)
	}
}
