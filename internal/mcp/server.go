// Package mcp implements a Model Context Protocol (MCP) server over stdio.
//
// MCP uses JSON-RPC 2.0 for communication. The server reads requests from
// stdin and writes responses to stdout. This allows Claude, Codex, Pi, and
// other MCP-compatible clients to consume engram9 knowledge bundles.
//
// Spec: https://modelcontextprotocol.io/specification/2024-11-05
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/qiffang/engram9/internal/storage"
)

// ProtocolVersion is the MCP protocol version we advertise.
const ProtocolVersion = "2024-11-05"

// Server handles MCP JSON-RPC requests over stdio.
type Server struct {
	store storage.Store
}

// NewServer creates an MCP server backed by the given store.
func NewServer(store storage.Store) *Server {
	return &Server{store: store}
}

// --- JSON-RPC types ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`

	// hasID distinguishes "id" absent from "id": null.
	// JSON-RPC 2.0: requests have "id", notifications do not.
	hasID bool
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP protocol types ---

type initializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Capabilities    capabilities `json:"capabilities"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type capabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type toolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolsCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Server loop ---

// Serve reads JSON-RPC requests from r and writes responses to w.
// It blocks until r is closed or an unrecoverable error occurs.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4<<20), 4<<20) // 4MB line buffer

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		req, err := parseRequest([]byte(line))
		if err != nil {
			log.Printf("[mcp] invalid JSON-RPC: %v", err)
			resp := jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &jsonRPCError{Code: -32700, Message: "parse error"},
			}
			writeResponse(w, resp)
			continue
		}

		// JSON-RPC 2.0: notifications (no "id" field) must not receive responses.
		if !req.hasID {
			s.handleNotification(req)
			continue
		}

		resp := s.handleRequest(req)
		if resp != nil {
			writeResponse(w, *resp)
		}
	}

	return scanner.Err()
}

// parseRequest unmarshals a JSON-RPC message and detects whether "id" is present.
// JSON-RPC 2.0 distinguishes requests (have "id") from notifications (no "id").
func parseRequest(data []byte) (jsonRPCRequest, error) {
	var req jsonRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, err
	}
	// Check whether "id" key exists in the raw JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return req, err
	}
	_, req.hasID = raw["id"]
	return req, nil
}

// handleNotification processes JSON-RPC notifications (no "id" → no response).
func (s *Server) handleNotification(req jsonRPCRequest) {
	// MCP defines several notification methods; log unrecognized ones.
	switch {
	case strings.HasPrefix(req.Method, "notifications/"):
		// Known MCP notification namespace — silently accept.
	default:
		log.Printf("[mcp] unhandled notification: %s", req.Method)
	}
}

func (s *Server) handleRequest(req jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "ping":
		return &jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: ProtocolVersion,
			ServerInfo: serverInfo{
				Name:    "engram9-mcp",
				Version: "0.1.0",
			},
			Capabilities: capabilities{
				Tools: &toolsCapability{ListChanged: false},
			},
		},
	}
}

func (s *Server) handleToolsList(req jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  toolsListResult{Tools: MCPTools},
	}
}

func (s *Server) handleToolsCall(req jsonRPCRequest) *jsonRPCResponse {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &jsonRPCError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	result, err := s.executeTool(params.Name, params.Arguments)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolsCallResult{
				Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Error: %s", err.Error())}},
				IsError: true,
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolsCallResult{
			Content: []toolContent{{Type: "text", Text: result}},
		},
	}
}

func writeResponse(w io.Writer, resp jsonRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[mcp] marshal error: %v", err)
		return
	}
	_, _ = fmt.Fprintf(w, "%s\n", data)
}
