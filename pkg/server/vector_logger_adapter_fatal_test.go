//go:build vector && !integration

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

// vector_logger_adapter_fatal_test.go exercises the Fatal/Fatalf methods of
// the qrdb logger adapter. These delegate to logger.Logger.Fatal{,f}, which
// terminate the process, so they are verified with the standard subprocess
// re-exec pattern: the child runs the fatal path and the parent asserts a
// non-zero exit.

package server

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newAdapterForFatal(t *testing.T) *qrdbLoggerAdapter {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "info", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return &qrdbLoggerAdapter{log: log}
}

func TestQRDBLoggerAdapter_Fatal(t *testing.T) {
	if os.Getenv("BE_ADAPTER_FATAL") == "1" {
		newAdapterForFatal(t).Fatal("adapter fatal")
		return // unreached
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestQRDBLoggerAdapter_Fatal")
	cmd.Env = append(os.Environ(), "BE_ADAPTER_FATAL=1")
	err := cmd.Run()
	var e *exec.ExitError
	if errors.As(err, &e) && !e.Success() {
		return // expected: child exited non-zero
	}
	t.Errorf("Fatal should have exited the process non-zero, got: %v", err)
}

func TestQRDBLoggerAdapter_Fatalf(t *testing.T) {
	if os.Getenv("BE_ADAPTER_FATALF") == "1" {
		newAdapterForFatal(t).Fatalf("adapter fatal %d", 1)
		return // unreached
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestQRDBLoggerAdapter_Fatalf")
	cmd.Env = append(os.Environ(), "BE_ADAPTER_FATALF=1")
	err := cmd.Run()
	var e *exec.ExitError
	if errors.As(err, &e) && !e.Success() {
		return // expected: child exited non-zero
	}
	t.Errorf("Fatalf should have exited the process non-zero, got: %v", err)
}
