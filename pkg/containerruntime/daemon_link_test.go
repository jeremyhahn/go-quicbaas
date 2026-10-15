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

package containerruntime

import (
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const (
	modulePath             = "github.com/jeremyhahn/go-quicbaas"
	daemonImport           = modulePath + "/cmd/quicbaasd"
	containerRuntimeImport = modulePath + "/pkg/containerruntime"
)

// TestDaemonNeverLinksContainerRuntime pins where the runtime runs: in the
// fleet node agent, never in quicbaasd, which stays CGO_ENABLED=0. It lists
// every package the daemon links, with cgo off as build-daemon builds it,
// untagged, under the unit tags and under build-daemon's tags, and refuses
// this package among them.
func TestDaemonNeverLinksContainerRuntime(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	goTool := filepath.Join(build.Default.GOROOT, "bin", "go")

	tagSets := map[string]string{
		"untagged":  "",
		"unit tags": "hashicorpmetrics local canary otel l2_redis rest quic mcp rag codec_json raft vector timeseries",
		"build-daemon tags": "hashicorpmetrics local raft timeseries qrdb_ts vector qrdb_vector rag rest quic mcp " +
			"quicbaas_mqtt quicbaas_amqp quicbaas_stomp codec_json codec_yaml codec_toml codec_msgpack " +
			"codec_bson codec_cbor codec_protobuf gms_pure_go",
	}
	for name, tags := range tagSets {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(goTool, "list", "-deps", "-tags", tags, "./cmd/quicbaasd")
			cmd.Dir = moduleRoot
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("go list -deps ./cmd/quicbaasd: %v", err)
			}
			deps := strings.Fields(string(out))
			if !slices.Contains(deps, daemonImport) {
				t.Fatalf("go list did not list %s itself: %d packages", daemonImport, len(deps))
			}
			if slices.Contains(deps, containerRuntimeImport) {
				t.Fatalf("quicbaasd links %s", containerRuntimeImport)
			}
		})
	}
}
