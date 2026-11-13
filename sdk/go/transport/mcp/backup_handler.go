// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// isMCPBackupService reports whether service names the Backup service, with
// or without its "quicbaas." prefix.
func isMCPBackupService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Backup"
}

// transformBackupResponse decodes the restore tool's result into the
// RestoreBackupResponse every other transport returns. The server marshals
// that service struct as is, so its keys are the Go field names. Any other
// verb's result is returned unchanged.
func transformBackupResponse(method string, result interface{}) (interface{}, error) {
	if method != "Restore" {
		return result, nil
	}
	var resp service.RestoreBackupResponse
	if err := remarshal(result, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.backup.Restore", "failed to decode tool result")
	}
	return &resp, nil
}
