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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// deviceAdapterPackages are the device transports QuicBaaS composes into
// quicbaasd as the hardware crypto passed into go-xkms. A consumer of this
// SDK, go-trusted-platform, reaches device-backed keys through the SDK's KMS
// client and never links them (SPEC row hardware-backend-composition), even
// though the SDK links pkg/server as its embedded server.
//
// go-xkms's own SDK re-exports its AVR backend and the trusted-firmware
// protocol package, so those reach this graph through go-xkms, not QuicBaaS;
// only the transports that open a device are QuicBaaS's to keep out.
var deviceAdapterPackages = []string{
	"github.com/jeremyhahn/trusted-firmware/sdk/go/serialport",
	"go.bug.st/serial",
}

// TestSDK_NoDeviceAdapterImport proves no SDK package links a device backend
// adapter: the SDK's non-test dependency graph carries none of them.
func TestSDK_NoDeviceAdapterImport(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list over the SDK module")
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	out, err := exec.Command(goTool, "list", "-deps", "./...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./...: %v\n%s", err, out)
	}
	for _, pkg := range strings.Fields(string(out)) {
		for _, adapter := range deviceAdapterPackages {
			if pkg == adapter || strings.HasPrefix(pkg, adapter) {
				t.Errorf("SDK dependency graph carries device adapter package %s", pkg)
			}
		}
	}
}
