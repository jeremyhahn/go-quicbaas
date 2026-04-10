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

// Example: Model Context Protocol (MCP) Client
//
// This example demonstrates how to interact with QuicBaaS's MCP server, enabling
// AI/LLM systems to access database operations through a standardized protocol.
//
// Features demonstrated:
//   - Connecting to QuicBaaS MCP server via HTTP transport
//   - MCP protocol initialization and capability negotiation
//   - Tool discovery (listing available database operations)
//   - Tool invocation (executing database operations)
//   - Resource access (reading cluster status, metrics, etc.)
//   - Authentication with Bearer tokens
//   - Error handling for MCP responses
//
// The MCP server implements the Model Context Protocol specification:
// https://spec.modelcontextprotocol.io/specification/
//
// Prerequisites:
//   - A running QuicBaaS server with MCP transport enabled
//   - Default MCP endpoint: http://localhost:8888/mcp
//   - Server must be initialized and unsealed
//
// Usage:
//
//	# Run with default settings
//	go run main.go
//
//	# Run with custom MCP endpoint
//	go run main.go -address http://localhost:8888
//
//	# Run with authentication token
//	go run main.go -token "your-access-token"
//
//	# Run with verbose output
//	go run main.go -verbose
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// MCPVersion is the protocol version supported by this client.
const MCPVersion = "2024-11-05"

// JSON-RPC error codes as defined in the MCP specification.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

	// Custom QuicBaaS MCP error codes.
	AuthenticationRequired = -32001
	InvalidToken           = -32002
	PermissionDenied       = -32003
)

// JSONRPCRequest represents a JSON-RPC 2.0 request message.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response message.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPClient provides methods to interact with a QuicBaaS MCP server.
type MCPClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
	requestID  atomic.Uint64
	verbose    bool
}

// NewMCPClient creates a new MCP client instance.
func NewMCPClient(endpoint string, token string, verbose bool) *MCPClient {
	return &MCPClient{
		endpoint: endpoint + "/mcp",
		token:    token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		verbose: verbose,
	}
}

// sendRequest sends a JSON-RPC request to the MCP server.
func (c *MCPClient) sendRequest(method string, params interface{}) (*JSONRPCResponse, error) {
	id := c.requestID.Add(1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, &ClientError{Op: "sendRequest", Message: "failed to marshal request", Err: err}
	}

	if c.verbose {
		fmt.Printf("  [REQUEST] %s\n", string(body))
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Op: "sendRequest", Message: "failed to create HTTP request", Err: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("MCP-Protocol-Version", MCPVersion)

	// Add authentication token if provided.
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &ClientError{Op: "sendRequest", Message: "HTTP request failed", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Op: "sendRequest", Message: "failed to read response body", Err: err}
	}

	if c.verbose {
		fmt.Printf("  [RESPONSE] %s\n", string(respBody))
	}

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, &ClientError{Op: "sendRequest", Message: "failed to unmarshal response", Err: err}
	}

	return &rpcResp, nil
}

// Initialize performs the MCP protocol handshake with the server.
func (c *MCPClient) Initialize() (map[string]interface{}, error) {
	params := map[string]interface{}{
		"protocolVersion": MCPVersion,
		"clientInfo": map[string]string{
			"name":    "QuicBaaS MCP Example Client",
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{},
	}

	resp, err := c.sendRequest("initialize", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, &ClientError{Op: "Initialize", Message: "unexpected result type"}
	}

	return result, nil
}

// Ping sends a ping request to verify connectivity.
func (c *MCPClient) Ping() (bool, error) {
	resp, err := c.sendRequest("ping", nil)
	if err != nil {
		return false, err
	}

	if resp.Error != nil {
		return false, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return false, &ClientError{Op: "Ping", Message: "unexpected result type"}
	}

	pong, _ := result["pong"].(bool)
	return pong, nil
}

// ListTools retrieves the list of available tools from the server.
func (c *MCPClient) ListTools() ([]map[string]interface{}, error) {
	resp, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, &ClientError{Op: "ListTools", Message: "unexpected result type"}
	}

	toolsRaw, ok := result["tools"].([]interface{})
	if !ok {
		return nil, &ClientError{Op: "ListTools", Message: "tools field not found or invalid type"}
	}

	tools := make([]map[string]interface{}, 0, len(toolsRaw))
	for _, t := range toolsRaw {
		if tool, ok := t.(map[string]interface{}); ok {
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// CallTool invokes a specific tool with the given arguments.
func (c *MCPClient) CallTool(name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	resp, err := c.sendRequest("tools/call", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, &ClientError{Op: "CallTool", Message: "unexpected result type"}
	}

	return result, nil
}

// ListResources retrieves the list of available resources from the server.
func (c *MCPClient) ListResources() ([]map[string]interface{}, error) {
	resp, err := c.sendRequest("resources/list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, &ClientError{Op: "ListResources", Message: "unexpected result type"}
	}

	resourcesRaw, ok := result["resources"].([]interface{})
	if !ok {
		return nil, &ClientError{Op: "ListResources", Message: "resources field not found or invalid type"}
	}

	resources := make([]map[string]interface{}, 0, len(resourcesRaw))
	for _, r := range resourcesRaw {
		if resource, ok := r.(map[string]interface{}); ok {
			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// ReadResource reads a specific resource by URI.
func (c *MCPClient) ReadResource(uri string) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"uri": uri,
	}

	resp, err := c.sendRequest("resources/read", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, &RPCError{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return nil, &ClientError{Op: "ReadResource", Message: "unexpected result type"}
	}

	return result, nil
}

// ClientError represents a client-side error.
type ClientError struct {
	Op      string
	Message string
	Err     error
}

// Error returns the error message.
func (e *ClientError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *ClientError) Unwrap() error {
	return e.Err
}

// RPCError represents a JSON-RPC error returned by the server.
type RPCError struct {
	Code    int
	Message string
}

// Error returns the error message with code.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

func main() {
	// Parse command-line flags.
	address := flag.String("address", "http://localhost:8888", "MCP server address")
	token := flag.String("token", "", "Authentication token (optional)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	fmt.Println("=== QuicBaaS MCP Client Example ===")
	fmt.Printf("MCP Endpoint: %s/mcp\n", *address)
	fmt.Printf("Protocol:     %s\n", MCPVersion)
	if *token != "" {
		fmt.Println("Auth:         Token provided")
	} else {
		fmt.Println("Auth:         None (using public tools only)")
	}
	fmt.Println()

	// Create MCP client.
	client := NewMCPClient(*address, *token, *verbose)

	// Step 1: Initialize MCP connection.
	fmt.Println("Step 1: Initializing MCP connection...")
	initResult, err := client.Initialize()
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	serverInfo, _ := initResult["serverInfo"].(map[string]interface{})
	serverName, _ := serverInfo["name"].(string)
	serverVersion, _ := serverInfo["version"].(string)
	protocolVersion, _ := initResult["protocolVersion"].(string)

	fmt.Printf("  Server:          %s v%s\n", serverName, serverVersion)
	fmt.Printf("  Protocol:        %s\n", protocolVersion)

	capabilities, _ := initResult["capabilities"].(map[string]interface{})
	fmt.Printf("  Capabilities:\n")
	if tools, ok := capabilities["tools"]; ok && tools != nil {
		fmt.Println("    - Tools: Supported")
	}
	if resources, ok := capabilities["resources"]; ok && resources != nil {
		fmt.Println("    - Resources: Supported")
	}
	fmt.Println()

	// Step 2: Ping the server.
	fmt.Println("Step 2: Testing connectivity with ping...")
	pong, err := client.Ping()
	if err != nil {
		log.Printf("  Warning: Ping failed: %v", err)
	} else {
		fmt.Printf("  Pong: %v\n", pong)
	}
	fmt.Println()

	// Step 3: Discover available tools.
	fmt.Println("Step 3: Discovering available tools...")
	tools, err := client.ListTools()
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	fmt.Printf("  Found %d tools:\n", len(tools))

	// Group tools by category for display.
	toolCategories := make(map[string][]string)
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if len(name) > 0 {
			category := extractCategory(name)
			toolCategories[category] = append(toolCategories[category], name)
		}
	}

	for category, toolNames := range toolCategories {
		fmt.Printf("    [%s] %d tools\n", category, len(toolNames))
		// Show first 3 tools in each category.
		displayCount := minInt(3, len(toolNames))
		for i := 0; i < displayCount; i++ {
			fmt.Printf("      - %s\n", toolNames[i])
		}
		if len(toolNames) > 3 {
			fmt.Printf("      ... and %d more\n", len(toolNames)-3)
		}
	}
	fmt.Println()

	// Step 4: List available resources.
	fmt.Println("Step 4: Listing available resources...")
	resources, err := client.ListResources()
	if err != nil {
		log.Printf("  Warning: Failed to list resources: %v", err)
	} else {
		fmt.Printf("  Found %d resources:\n", len(resources))
		for _, res := range resources {
			uri, _ := res["uri"].(string)
			name, _ := res["name"].(string)
			fmt.Printf("    - %s (%s)\n", name, uri)
		}
	}
	fmt.Println()

	// Step 5: Invoke health check tool (public, no auth required).
	fmt.Println("Step 5: Invoking health check tool...")
	healthResult, err := client.CallTool("quicbaas_health", map[string]interface{}{
		"includeDetails": true,
	})
	if err != nil {
		log.Printf("  Warning: Health check failed: %v", err)
	} else {
		displayToolResult("Health Status", healthResult)
	}
	fmt.Println()

	// Step 6: Invoke liveness probe tool.
	fmt.Println("Step 6: Invoking liveness probe...")
	livenessResult, err := client.CallTool("quicbaas_liveness", map[string]interface{}{})
	if err != nil {
		log.Printf("  Warning: Liveness probe failed: %v", err)
	} else {
		displayToolResult("Liveness", livenessResult)
	}
	fmt.Println()

	// Step 7: Invoke cluster status tool.
	fmt.Println("Step 7: Invoking cluster status tool...")
	clusterResult, err := client.CallTool("quicbaas_cluster_status", map[string]interface{}{
		"includeDetails": true,
	})
	if err != nil {
		log.Printf("  Warning: Cluster status failed: %v", err)
	} else {
		displayToolResult("Cluster Status", clusterResult)
	}
	fmt.Println()

	// Step 8: Read health resource.
	fmt.Println("Step 8: Reading health resource...")
	healthResource, err := client.ReadResource("quicbaas://health")
	if err != nil {
		log.Printf("  Warning: Failed to read health resource: %v", err)
	} else {
		displayResourceResult("Health Resource", healthResource)
	}
	fmt.Println()

	// Step 9: Demonstrate KV operations (if authenticated).
	if *token != "" {
		fmt.Println("Step 9: Demonstrating KV operations (authenticated)...")
		demonstrateKVOperations(client)
	} else {
		fmt.Println("Step 9: Skipping KV operations (no auth token provided)")
		fmt.Println("  Tip: Use -token flag to provide authentication for write operations")
	}
	fmt.Println()

	// Step 10: Demonstrate cache operations.
	fmt.Println("Step 10: Checking cache statistics...")
	cacheResult, err := client.CallTool("quicbaas_cache_stats", map[string]interface{}{})
	if err != nil {
		log.Printf("  Warning: Cache stats failed: %v", err)
	} else {
		displayToolResult("Cache Stats", cacheResult)
	}
	fmt.Println()

	fmt.Println("=== MCP Client Example Complete ===")
	fmt.Println()
	fmt.Println("Key Takeaways:")
	fmt.Println("  1. MCP provides a standardized protocol for AI/LLM database access")
	fmt.Println("  2. Initialize establishes the connection and negotiates capabilities")
	fmt.Println("  3. Tools expose database operations with typed schemas")
	fmt.Println("  4. Resources provide read-only access to cluster state")
	fmt.Println("  5. Bearer tokens in Authorization header enable authenticated operations")
	fmt.Println("  6. All operations follow JSON-RPC 2.0 message format")
	fmt.Println()
	fmt.Println("Integration Tips:")
	fmt.Println("  - Use this client pattern to integrate QuicBaaS with LLM systems")
	fmt.Println("  - Tools can be registered as callable functions in AI agents")
	fmt.Println("  - Resources provide context for AI decision-making")
	fmt.Println("  - The MCP server also supports stdio transport for subprocess use")
}

// demonstrateKVOperations shows basic key-value operations via MCP tools.
func demonstrateKVOperations(client *MCPClient) {
	// Put a value.
	fmt.Println("  Storing test value...")
	putResult, err := client.CallTool("quicbaas_put", map[string]interface{}{
		"entityType": "mcp_example",
		"id":         12345,
		"data":       "Hello from MCP client!",
	})
	if err != nil {
		log.Printf("    Warning: Put failed: %v", err)
		return
	}
	fmt.Println("    Put succeeded")
	displayToolResultContent(putResult)

	// Get the value back.
	fmt.Println("  Retrieving test value...")
	getResult, err := client.CallTool("quicbaas_get", map[string]interface{}{
		"entityType": "mcp_example",
		"id":         12345,
	})
	if err != nil {
		log.Printf("    Warning: Get failed: %v", err)
		return
	}
	fmt.Println("    Get succeeded")
	displayToolResultContent(getResult)

	// List entities.
	fmt.Println("  Listing entities...")
	listResult, err := client.CallTool("quicbaas_list", map[string]interface{}{
		"entityType": "mcp_example",
		"pageSize":   10,
	})
	if err != nil {
		log.Printf("    Warning: List failed: %v", err)
		return
	}
	fmt.Println("    List succeeded")
	displayToolResultContent(listResult)

	// Delete the value.
	fmt.Println("  Deleting test value...")
	deleteResult, err := client.CallTool("quicbaas_delete", map[string]interface{}{
		"entityType": "mcp_example",
		"id":         12345,
	})
	if err != nil {
		log.Printf("    Warning: Delete failed: %v", err)
		return
	}
	fmt.Println("    Delete succeeded")
	displayToolResultContent(deleteResult)
}

// displayToolResult displays a tool execution result with formatting.
func displayToolResult(label string, result map[string]interface{}) {
	fmt.Printf("  %s:\n", label)
	displayToolResultContent(result)
}

// displayToolResultContent extracts and displays content from a tool result.
func displayToolResultContent(result map[string]interface{}) {
	// MCP tool results are wrapped in a content array.
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		fmt.Println("    (no content)")
		return
	}

	for _, item := range content {
		contentItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		contentType, _ := contentItem["type"].(string)
		if contentType == "text" {
			text, _ := contentItem["text"].(string)

			// Try to parse as JSON for pretty printing.
			var jsonData interface{}
			if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
				prettyJSON, err := json.MarshalIndent(jsonData, "    ", "  ")
				if err == nil {
					fmt.Printf("    %s\n", string(prettyJSON))
					continue
				}
			}

			// Fallback to raw text.
			fmt.Printf("    %s\n", text)
		}
	}
}

// displayResourceResult displays a resource read result.
func displayResourceResult(label string, result map[string]interface{}) {
	fmt.Printf("  %s:\n", label)

	// Resources are wrapped in a contents array.
	contents, ok := result["contents"].([]interface{})
	if !ok || len(contents) == 0 {
		fmt.Println("    (no content)")
		return
	}

	for _, item := range contents {
		contentItem, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		mimeType, _ := contentItem["mimeType"].(string)
		text, _ := contentItem["text"].(string)

		if mimeType == "application/json" {
			var jsonData interface{}
			if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
				prettyJSON, err := json.MarshalIndent(jsonData, "    ", "  ")
				if err == nil {
					fmt.Printf("    %s\n", string(prettyJSON))
					continue
				}
			}
		}

		// Fallback to raw text (truncated).
		if len(text) > 200 {
			fmt.Printf("    %s... (truncated)\n", text[:200])
		} else {
			fmt.Printf("    %s\n", text)
		}
	}
}

// extractCategory extracts the category from a tool name (e.g., "quicbaas_kms" -> "KMS").
func extractCategory(name string) string {
	// Tool names follow pattern: quicbaas_category_operation.
	if len(name) < 10 {
		return "Other"
	}

	// Skip "quicbaas_" prefix.
	rest := name[9:]

	// Find the next underscore to get category.
	for i, ch := range rest {
		if ch == '_' {
			category := rest[:i]
			return normalizeCategory(category)
		}
	}

	return normalizeCategory(rest)
}

// normalizeCategory normalizes a category name for display.
func normalizeCategory(category string) string {
	switch category {
	case "put", "get", "delete", "list", "batch":
		return "KV Operations"
	case "cluster", "add", "remove", "create", "rebalance":
		return "Cluster"
	case "backup":
		return "Backup"
	case "kms":
		return "KMS"
	case "secrets":
		return "Secrets"
	case "crypto":
		return "Crypto"
	case "seal", "unseal":
		return "Seal"
	case "user", "role", "login", "logout":
		return "Auth/RBAC"
	case "cache":
		return "Cache"
	case "health", "liveness", "readiness":
		return "Health"
	case "migrate", "migration", "replace", "upgrade":
		return "Operations"
	case "aggregate":
		return "Analytics"
	case "dek":
		return "DEK"
	case "object":
		return "Object Storage"
	case "timeseries":
		return "Time Series"
	case "vector":
		return "Vector"
	case "ollama":
		return "Ollama"
	case "rag":
		return "RAG"
	default:
		return "Other"
	}
}

// minInt returns the minimum of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
