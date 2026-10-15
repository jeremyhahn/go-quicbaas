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

// parameterstore_handler.go adapts the ParameterStore client to the eight
// quicbaas_parameter_* MCP tools.
//
// The tools are registered under quicbaas_parameter_* and
// quicbaas_parameter_batch_*, not the quicbaas_parameterstore_* names the
// generic derivation produces. Their schema is entirely string-typed:
// booleans and integers travel as their string forms, and tags, path lists
// and batch parameter lists travel as JSON-encoded strings. Values travel as
// text, so a value that is not valid UTF-8 is refused rather than corrupted.
// The results are maps that are decoded back into the client/parameterstore
// types the SDK client type-asserts on.

package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	psclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// parameterStoreServiceName is the service name the ParameterStore client
// sends.
const parameterStoreServiceName = "quicbaas.ParameterStore"

// toolVerb binds one SDK method to the MCP tool that serves it: the tool
// name, the conversion of the SDK request into tool arguments, and the
// decoding of the tool's JSON object result into the SDK response type.
type toolVerb struct {
	tool   string
	args   func(req interface{}) (map[string]interface{}, error)
	decode func(m map[string]interface{}) interface{}
}

// requestToolVerb calls the verb's tool and decodes its result.
func (t *Transport) requestToolVerb(ctx context.Context, op string, verb toolVerb, req interface{}) (interface{}, error) {
	arguments, err := verb.args(req)
	if err != nil {
		return nil, err
	}
	result, err := t.callTool(ctx, verb.tool, arguments)
	if err != nil {
		return nil, err
	}
	m, err := asResultMap(result, op)
	if err != nil {
		return nil, err
	}
	return verb.decode(m), nil
}

// isMCPParameterStoreService reports whether the service name targets the
// parameter store.
func isMCPParameterStoreService(service string) bool {
	return strings.HasPrefix(service, parameterStoreServiceName) || service == "ParameterStore"
}

// parameterStoreVerbs is the closed set of ParameterStore methods served
// over MCP. Keep in sync with pkg/api/mcp/parameters_tools.go.
var parameterStoreVerbs = map[string]toolVerb{
	"GetParameter":     {tool: "quicbaas_parameter_get", args: getParameterArgs, decode: decodeGetParameter},
	"SetParameter":     {tool: "quicbaas_parameter_set", args: setParameterArgs, decode: decodeSetParameter},
	"DeleteParameter":  {tool: "quicbaas_parameter_delete", args: deleteParameterArgs, decode: decodeDeleteParameter},
	"ListParameters":   {tool: "quicbaas_parameter_list", args: listParametersArgs, decode: decodeListParameters},
	"SearchByTags":     {tool: "quicbaas_parameter_search", args: searchByTagsArgs, decode: decodeSearchByTags},
	"GetParameters":    {tool: "quicbaas_parameter_batch_get", args: getParametersArgs, decode: decodeGetParameters},
	"SetParameters":    {tool: "quicbaas_parameter_batch_set", args: setParametersArgs, decode: decodeSetParameters},
	"DeleteParameters": {tool: "quicbaas_parameter_batch_delete", args: deleteParametersArgs, decode: decodeDeleteParameters},
}

// newParameterStoreUnsupportedError reports a ParameterStore method the MCP
// tool set does not serve.
func newParameterStoreUnsupportedError(method string) error {
	return errors.NewUnimplemented("mcp.parameterstore."+method,
		"method not exposed by the ParameterStore MCP tool set")
}

// parameterStoreRequest asserts the SDK request type for method.
func parameterStoreRequest[T any](method string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.parameterstore."+method,
			"invalid request type for "+method)
	}
	return r, nil
}

// parameterText converts a parameter value to the text the tools carry.
func parameterText(method string, value []byte) (string, error) {
	if !utf8.Valid(value) {
		return "", errors.NewInvalidInput("mcp.parameterstore."+method,
			"value is not valid UTF-8; the MCP parameter tools carry values as text")
	}
	return string(value), nil
}

// putJSONStringArg sets key to the JSON encoding of v, as the string-typed
// tool schema requires.
func putJSONStringArg(args map[string]interface{}, op, key string, v interface{}) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return errors.NewInternal(op,
			"failed to encode "+key+": "+err.Error())
	}
	args[key] = string(encoded)
	return nil
}

// putLimitArg sets the page limit, leaving it unset at zero so the server
// applies its default.
func putLimitArg(args map[string]interface{}, limit int) {
	if limit > 0 {
		args["limit"] = strconv.Itoa(limit)
	}
}

func getParameterArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.GetParameterRequest]("GetParameter", req)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"path":            r.Path,
		"with_decryption": strconv.FormatBool(r.WithDecryption),
	}, nil
}

func setParameterArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.SetParameterRequest]("SetParameter", req)
	if err != nil {
		return nil, err
	}
	value, err := parameterText("SetParameter", r.Value)
	if err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"path":          r.Path,
		"value":         value,
		"audit_enabled": strconv.FormatBool(r.AuditEnabled),
		"overwrite":     strconv.FormatBool(r.Overwrite),
	}
	putStringArg(args, "type", r.Type)
	putStringArg(args, "description", r.Description)
	putStringArg(args, "kms_key_id", r.KMSKeyID)
	if r.TTLSeconds != 0 {
		args["ttl_seconds"] = strconv.FormatInt(r.TTLSeconds, 10)
	}
	if len(r.Tags) > 0 {
		if err := putJSONStringArg(args, "mcp.parameterstore.SetParameter", "tags", r.Tags); err != nil {
			return nil, err
		}
	}
	return args, nil
}

func deleteParameterArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.DeleteParameterRequest]("DeleteParameter", req)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"path": r.Path}, nil
}

func listParametersArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.ListParametersRequest]("ListParameters", req)
	if err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"prefix":          r.Prefix,
		"recursive":       strconv.FormatBool(r.Recursive),
		"with_decryption": strconv.FormatBool(r.WithDecryption),
		"with_values":     strconv.FormatBool(r.WithValues),
	}
	putLimitArg(args, r.Limit)
	putStringArg(args, "cursor", r.Cursor)
	return args, nil
}

func searchByTagsArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.SearchByTagsRequest]("SearchByTags", req)
	if err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"with_values":     strconv.FormatBool(r.WithValues),
		"with_decryption": strconv.FormatBool(r.WithDecryption),
	}
	if err := putJSONStringArg(args, "mcp.parameterstore.SearchByTags", "tags", r.Tags); err != nil {
		return nil, err
	}
	putStringArg(args, "prefix", r.Prefix)
	putLimitArg(args, r.Limit)
	putStringArg(args, "cursor", r.Cursor)
	return args, nil
}

func getParametersArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.GetParametersRequest]("GetParameters", req)
	if err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"with_decryption": strconv.FormatBool(r.WithDecryption),
	}
	if err := putJSONArrayArg(args, "paths", r.Paths); err != nil {
		return nil, err
	}
	return args, nil
}

// parameterSetArg is one element of the batch-set tool's JSON parameter
// list; its tags match the server's parameterSetInput.
type parameterSetArg struct {
	Path         string            `json:"path"`
	Value        string            `json:"value"`
	Type         string            `json:"type,omitempty"`
	Description  string            `json:"description,omitempty"`
	KMSKeyID     string            `json:"kms_key_id,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	TTLSeconds   int64             `json:"ttl_seconds,omitempty"`
	AuditEnabled bool              `json:"audit_enabled,omitempty"`
	Overwrite    bool              `json:"overwrite,omitempty"`
}

func setParametersArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.SetParametersRequest]("SetParameters", req)
	if err != nil {
		return nil, err
	}
	params := make([]parameterSetArg, 0, len(r.Parameters))
	for _, p := range r.Parameters {
		if p == nil {
			return nil, errors.NewInvalidInput("mcp.parameterstore.SetParameters",
				"parameters must not contain a nil entry")
		}
		value, err := parameterText("SetParameters", p.Value)
		if err != nil {
			return nil, err
		}
		params = append(params, parameterSetArg{
			Path:         p.Path,
			Value:        value,
			Type:         p.Type,
			Description:  p.Description,
			KMSKeyID:     p.KMSKeyID,
			Tags:         p.Tags,
			TTLSeconds:   p.TTLSeconds,
			AuditEnabled: p.AuditEnabled,
			Overwrite:    p.Overwrite,
		})
	}
	args := map[string]interface{}{}
	if err := putJSONStringArg(args, "mcp.parameterstore.SetParameters", "parameters", params); err != nil {
		return nil, err
	}
	return args, nil
}

func deleteParametersArgs(req interface{}) (map[string]interface{}, error) {
	r, err := parameterStoreRequest[psclient.DeleteParametersRequest]("DeleteParameters", req)
	if err != nil {
		return nil, err
	}
	args := map[string]interface{}{}
	if err := putJSONArrayArg(args, "paths", r.Paths); err != nil {
		return nil, err
	}
	return args, nil
}

// parameterValueFromResult decodes one formatted parameter.
func parameterValueFromResult(m map[string]interface{}) *psclient.ParameterValue {
	if m == nil {
		return nil
	}
	p := &psclient.ParameterValue{
		Path:         getResultString(m, "path"),
		Type:         getResultString(m, "type"),
		Description:  getResultString(m, "description"),
		Tags:         getResultStringMap(m, "tags"),
		IsEncrypted:  getResultBool(m, "is_encrypted"),
		KMSKeyID:     getResultString(m, "kms_key_id"),
		AuditEnabled: getResultBool(m, "audit_enabled"),
		CreatedAt:    getResultTime(m, "created_at"),
		UpdatedAt:    getResultTime(m, "updated_at"),
		CreatedBy:    getResultString(m, "created_by"),
		UpdatedBy:    getResultString(m, "updated_by"),
	}
	if value, ok := m["value"].(string); ok {
		p.Value = []byte(value)
	}
	if _, ok := m["expires_at"]; ok {
		expiresAt := getResultTime(m, "expires_at")
		p.ExpiresAt = &expiresAt
	}
	return p
}

// parameterValuesFromResult decodes the parameter list under key.
func parameterValuesFromResult(m map[string]interface{}, key string) []*psclient.ParameterValue {
	entries := resultMapSlice(m, key)
	params := make([]*psclient.ParameterValue, 0, len(entries))
	for _, entry := range entries {
		params = append(params, parameterValueFromResult(entry))
	}
	return params
}

func decodeGetParameter(m map[string]interface{}) interface{} {
	return &psclient.GetParameterResponse{
		Parameter: parameterValueFromResult(nestedResultMap(m, "parameter")),
		Timestamp: getResultTime(m, "response_timestamp"),
	}
}

func decodeSetParameter(m map[string]interface{}) interface{} {
	return &psclient.SetParameterResponse{
		Path:      getResultString(m, "path"),
		Created:   getResultBool(m, "created"),
		Timestamp: getResultTime(m, "timestamp"),
	}
}

func decodeDeleteParameter(m map[string]interface{}) interface{} {
	return &psclient.DeleteParameterResponse{
		Path:      getResultString(m, "path"),
		Deleted:   getResultBool(m, "deleted"),
		Timestamp: getResultTime(m, "timestamp"),
	}
}

func decodeListParameters(m map[string]interface{}) interface{} {
	return &psclient.ListParametersResponse{
		Parameters: parameterValuesFromResult(m, "parameters"),
		NextCursor: getResultString(m, "next_cursor"),
		HasMore:    getResultBool(m, "has_more"),
		TotalCount: getResultInt64(m, "total_count"),
		Timestamp:  getResultTime(m, "timestamp"),
	}
}

func decodeSearchByTags(m map[string]interface{}) interface{} {
	return &psclient.SearchByTagsResponse{
		Parameters: parameterValuesFromResult(m, "parameters"),
		NextCursor: getResultString(m, "next_cursor"),
		HasMore:    getResultBool(m, "has_more"),
		Timestamp:  getResultTime(m, "timestamp"),
	}
}

func decodeGetParameters(m map[string]interface{}) interface{} {
	return &psclient.GetParametersResponse{
		Parameters: parameterValuesFromResult(m, "parameters"),
		Errors:     getResultStringMap(m, "errors"),
		Timestamp:  getResultTime(m, "timestamp"),
	}
}

func decodeSetParameters(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "results")
	results := make([]*psclient.SetParameterResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, &psclient.SetParameterResult{
			Path:    getResultString(entry, "path"),
			Created: getResultBool(entry, "created"),
			Success: getResultBool(entry, "success"),
			Error:   getResultString(entry, "error"),
		})
	}
	return &psclient.SetParametersResponse{
		Results:   results,
		Timestamp: getResultTime(m, "timestamp"),
	}
}

func decodeDeleteParameters(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "results")
	results := make([]*psclient.DeleteParameterResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, &psclient.DeleteParameterResult{
			Path:    getResultString(entry, "path"),
			Deleted: getResultBool(entry, "deleted"),
			Error:   getResultString(entry, "error"),
		})
	}
	return &psclient.DeleteParametersResponse{
		Results:      results,
		DeletedCount: int(getResultInt64(m, "deleted_count")),
		Timestamp:    getResultTime(m, "timestamp"),
	}
}
