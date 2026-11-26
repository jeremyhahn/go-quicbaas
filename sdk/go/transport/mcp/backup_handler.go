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

// transformBackupResponse decodes the create, list and restore tools' results
// into the CreateBackupResponse, ListBackupsResponse and RestoreBackupResponse
// every other transport returns, the types the backup client asserts. The
// server marshals those service structs as is, so their keys are the Go field
// names. Any other verb's result is returned unchanged.
func transformBackupResponse(method string, result interface{}) (interface{}, error) {
	var target interface{}
	switch method {
	case "Create":
		target = &service.CreateBackupResponse{}
	case "List":
		target = &service.ListBackupsResponse{}
	case "Restore":
		target = &service.RestoreBackupResponse{}
	default:
		return result, nil
	}
	if err := remarshal(result, target); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.backup."+method, "failed to decode tool result")
	}
	return target, nil
}
