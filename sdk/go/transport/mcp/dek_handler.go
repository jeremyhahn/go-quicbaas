// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// dekResultDecoder decodes a DEK tool result into the service response type
// the DEK client accepts.
type dekResultDecoder func(result interface{}) (interface{}, error)

// dekResultDecoders holds the DEK verbs whose tool result is decoded into a
// typed response. The server's DEK tools marshal the service responses, so
// the JSON tags are the wire field names.
var dekResultDecoders = map[string]dekResultDecoder{
	"GetDEKStatus": func(result interface{}) (interface{}, error) {
		var resp service.GetDEKStatusResponse
		if err := decodeDEKResult("GetDEKStatus", result, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	},
	"GetReencryptionProgress": func(result interface{}) (interface{}, error) {
		var resp service.GetReencryptionProgressResponse
		if err := decodeDEKResult("GetReencryptionProgress", result, &resp); err != nil {
			return nil, err
		}
		return &resp, nil
	},
}

// isMCPDEKService reports whether service names the DEK service.
func isMCPDEKService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "DEK"
}

// transformDEKResponse decodes the result of a DEK verb listed in
// dekResultDecoders; any other verb's result is returned unchanged.
func transformDEKResponse(method string, result interface{}) (interface{}, error) {
	decode, ok := dekResultDecoders[method]
	if !ok {
		return result, nil
	}
	return decode(result)
}

// decodeDEKResult re-encodes a tool result and decodes it into out.
func decodeDEKResult(method string, result, out interface{}) error {
	return decodeToolResult("mcp.dek."+method, result, out)
}
