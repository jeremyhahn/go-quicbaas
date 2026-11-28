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

package embedded

import (
	"context"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
)

const queryServiceName = "quicbaas.Query"

// handleQueryExecute serves query.Client.Execute through the same
// QuicBaaS.Query call the REST and gRPC Query handlers make. The service
// matches one index value by equality and takes no cursor, so any other
// operator, a cursor, or a value that is not a string or bytes is refused.
func handleQueryExecute(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*query.QueryRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(queryServiceName, "Execute", "*query.QueryRequest", typeNameOf(req))
	}
	serviceReq, err := queryRequestFromSDK(r)
	if err != nil {
		return nil, err
	}

	stream := make(chan *service.QueryResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Query(ctx, serviceReq, stream)
	}()
	results := []query.QueryResult{}
	for resp := range stream {
		for _, entity := range resp.Entities {
			results = append(results, query.QueryResult{
				Key:   strconv.FormatUint(entity.ID, 10),
				Value: entity.Data,
			})
		}
	}
	if err := <-errCh; err != nil {
		return nil, err
	}

	hasMore := r.Limit > 0 && len(results) > r.Limit
	if hasMore {
		results = results[:r.Limit]
	}
	return &query.QueryResponse{Results: results, HasMore: hasMore}, nil
}

// queryRequestFromSDK converts an SDK query request to the service request,
// refusing the fields the service cannot honour.
func queryRequestFromSDK(r *query.QueryRequest) (*service.QueryRequest, error) {
	if r.Operator != query.OpEqual {
		return nil, &UnsupportedQueryFieldError{Service: queryServiceName, Method: "Execute", Field: "Operator", Value: r.Operator.String()}
	}
	if r.Cursor != "" {
		return nil, &UnsupportedQueryFieldError{Service: queryServiceName, Method: "Execute", Field: "Cursor", Value: r.Cursor}
	}
	var value []byte
	switch v := r.Value.(type) {
	case string:
		value = []byte(v)
	case []byte:
		value = v
	default:
		return nil, &UnsupportedQueryFieldError{Service: queryServiceName, Method: "Execute", Field: "Value", Value: typeNameOf(r.Value)}
	}
	return &service.QueryRequest{
		EntityType: r.EntityType,
		IndexName:  r.Index,
		Value:      value,
	}, nil
}
