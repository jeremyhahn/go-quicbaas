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

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
)

// parameterStoreServiceName is the wire service name the SDK parameter
// store client sends.
const parameterStoreServiceName = "quicbaas.ParameterStore"

// parameterStoreService returns the configured parameter store service, or a
// ServiceNotConfiguredError when the embedded server has none.
func parameterStoreService(svc service.QuicBaaS) (service.ParameterStoreService, error) {
	paramSvc := svc.ParameterStoreService()
	if paramSvc == nil {
		return nil, NewServiceNotConfiguredError("ParameterStore")
	}
	return paramSvc, nil
}

// handleParameterStoreGetParameter routes ParameterStore/GetParameter.
func handleParameterStoreGetParameter(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.GetParameterRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "GetParameter", "*parameterstore.GetParameterRequest", typeNameOf(req))
	}
	resp, err := paramSvc.GetParameter(ctx, &interfaces.GetParameterRequest{
		Path:           sdkReq.Path,
		WithDecryption: sdkReq.WithDecryption,
	})
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.GetParameterResponse{
		Parameter: parameterValueToSDK(resp.Parameter),
		Timestamp: resp.Timestamp,
	}, nil
}

// handleParameterStoreSetParameter routes ParameterStore/SetParameter.
func handleParameterStoreSetParameter(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.SetParameterRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "SetParameter", "*parameterstore.SetParameterRequest", typeNameOf(req))
	}
	resp, err := paramSvc.SetParameter(ctx, setParameterRequestFromSDK(sdkReq))
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.SetParameterResponse{
		Path:      resp.Path,
		Created:   resp.Created,
		Timestamp: resp.Timestamp,
	}, nil
}

// handleParameterStoreDeleteParameter routes ParameterStore/DeleteParameter.
func handleParameterStoreDeleteParameter(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.DeleteParameterRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "DeleteParameter", "*parameterstore.DeleteParameterRequest", typeNameOf(req))
	}
	resp, err := paramSvc.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: sdkReq.Path})
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.DeleteParameterResponse{
		Path:      resp.Path,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp,
	}, nil
}

// handleParameterStoreGetParameters routes ParameterStore/GetParameters.
func handleParameterStoreGetParameters(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.GetParametersRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "GetParameters", "*parameterstore.GetParametersRequest", typeNameOf(req))
	}
	resp, err := paramSvc.GetParameters(ctx, &interfaces.GetParametersRequest{
		Paths:          sdkReq.Paths,
		WithDecryption: sdkReq.WithDecryption,
	})
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.GetParametersResponse{
		Parameters: parameterValuesToSDK(resp.Parameters),
		Errors:     resp.Errors,
		Timestamp:  resp.Timestamp,
	}, nil
}

// handleParameterStoreSetParameters routes ParameterStore/SetParameters.
func handleParameterStoreSetParameters(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.SetParametersRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "SetParameters", "*parameterstore.SetParametersRequest", typeNameOf(req))
	}
	params := make([]*interfaces.SetParameterRequest, len(sdkReq.Parameters))
	for i, param := range sdkReq.Parameters {
		params[i] = setParameterRequestFromSDK(param)
	}
	resp, err := paramSvc.SetParameters(ctx, &interfaces.SetParametersRequest{Parameters: params})
	if err != nil || resp == nil {
		return nil, err
	}
	results := make([]*parameterstore.SetParameterResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &parameterstore.SetParameterResult{
			Path:    result.Path,
			Created: result.Created,
			Success: result.Success,
			Error:   result.Error,
		}
	}
	return &parameterstore.SetParametersResponse{
		Results:   results,
		Timestamp: resp.Timestamp,
	}, nil
}

// handleParameterStoreDeleteParameters routes ParameterStore/DeleteParameters.
func handleParameterStoreDeleteParameters(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.DeleteParametersRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "DeleteParameters", "*parameterstore.DeleteParametersRequest", typeNameOf(req))
	}
	resp, err := paramSvc.DeleteParameters(ctx, &interfaces.DeleteParametersRequest{Paths: sdkReq.Paths})
	if err != nil || resp == nil {
		return nil, err
	}
	results := make([]*parameterstore.DeleteParameterResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &parameterstore.DeleteParameterResult{
			Path:    result.Path,
			Deleted: result.Deleted,
			Error:   result.Error,
		}
	}
	return &parameterstore.DeleteParametersResponse{
		Results:      results,
		DeletedCount: resp.DeletedCount,
		Timestamp:    resp.Timestamp,
	}, nil
}

// handleParameterStoreListParameters routes ParameterStore/ListParameters.
func handleParameterStoreListParameters(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.ListParametersRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "ListParameters", "*parameterstore.ListParametersRequest", typeNameOf(req))
	}
	resp, err := paramSvc.ListParameters(ctx, &interfaces.ListParametersRequest{
		Prefix:         sdkReq.Prefix,
		Recursive:      sdkReq.Recursive,
		WithDecryption: sdkReq.WithDecryption,
		WithValues:     sdkReq.WithValues,
		Limit:          sdkReq.Limit,
		Cursor:         sdkReq.Cursor,
	})
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.ListParametersResponse{
		Parameters: parameterValuesToSDK(resp.Parameters),
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		TotalCount: resp.TotalCount,
		Timestamp:  resp.Timestamp,
	}, nil
}

// handleParameterStoreSearchByTags routes ParameterStore/SearchByTags.
func handleParameterStoreSearchByTags(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	paramSvc, err := parameterStoreService(svc)
	if err != nil {
		return nil, err
	}
	sdkReq, ok := req.(*parameterstore.SearchByTagsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(parameterStoreServiceName, "SearchByTags", "*parameterstore.SearchByTagsRequest", typeNameOf(req))
	}
	resp, err := paramSvc.SearchByTags(ctx, &interfaces.SearchByTagsRequest{
		Tags:           sdkReq.Tags,
		Prefix:         sdkReq.Prefix,
		WithValues:     sdkReq.WithValues,
		WithDecryption: sdkReq.WithDecryption,
		Limit:          sdkReq.Limit,
		Cursor:         sdkReq.Cursor,
	})
	if err != nil || resp == nil {
		return nil, err
	}
	return &parameterstore.SearchByTagsResponse{
		Parameters: parameterValuesToSDK(resp.Parameters),
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Timestamp:  resp.Timestamp,
	}, nil
}

// setParameterRequestFromSDK maps one SDK set request onto the service
// request, field for field as the gRPC server does.
func setParameterRequestFromSDK(req *parameterstore.SetParameterRequest) *interfaces.SetParameterRequest {
	if req == nil {
		return nil
	}
	return &interfaces.SetParameterRequest{
		Path:         req.Path,
		Value:        req.Value,
		Type:         req.Type,
		Description:  req.Description,
		KMSKeyID:     req.KMSKeyID,
		Tags:         req.Tags,
		TTLSeconds:   req.TTLSeconds,
		AuditEnabled: req.AuditEnabled,
		Overwrite:    req.Overwrite,
	}
}

// parameterValueToSDK maps a service parameter onto the type the SDK client
// decodes. A nil parameter stays nil.
func parameterValueToSDK(param *interfaces.ParameterValue) *parameterstore.ParameterValue {
	if param == nil {
		return nil
	}
	return &parameterstore.ParameterValue{
		Path:         param.Path,
		Value:        param.Value,
		Type:         param.Type,
		Description:  param.Description,
		Tags:         param.Tags,
		IsEncrypted:  param.IsEncrypted,
		KMSKeyID:     param.KMSKeyID,
		ExpiresAt:    param.ExpiresAt,
		AuditEnabled: param.AuditEnabled,
		CreatedAt:    param.CreatedAt,
		UpdatedAt:    param.UpdatedAt,
		CreatedBy:    param.CreatedBy,
		UpdatedBy:    param.UpdatedBy,
	}
}

// parameterValuesToSDK maps a list of service parameters onto SDK parameters.
func parameterValuesToSDK(params []*interfaces.ParameterValue) []*parameterstore.ParameterValue {
	out := make([]*parameterstore.ParameterValue, len(params))
	for i, param := range params {
		out[i] = parameterValueToSDK(param)
	}
	return out
}
