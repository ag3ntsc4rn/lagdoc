// Package mcp serves lagdoc's findings to AI agents over the Model Context
// Protocol's stdio transport: newline-delimited JSON-RPC 2.0 on stdin and
// stdout.
//
// It is dual-era. A request carrying the protocol version in
// _meta["io.modelcontextprotocol/protocolVersion"] is served statelessly
// under the current revision (2026-07-28), which has no handshake. A client
// that opens with `initialize` gets the session-based revisions instead
// (2025-11-25 and earlier), for the rest of the process.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Protocol revisions. Modern ones put the version on every request;
// legacy ones negotiate it once with `initialize`.
const (
	ModernVersion = "2026-07-28"
	LatestLegacy  = "2025-11-25"
)

var (
	modernVersions = []string{ModernVersion}
	legacyVersions = []string{LatestLegacy, "2025-06-18", "2025-03-26", "2024-11-05"}
)

// Reserved _meta keys.
const (
	metaVersion      = "io.modelcontextprotocol/protocolVersion"
	metaCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaServerInfo   = "io.modelcontextprotocol/serverInfo"
)

// JSON-RPC and MCP error codes.
const (
	codeParse              = -32700
	codeInvalidRequest     = -32600
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
	codeInternal           = -32603
	codeUnsupportedVersion = -32022
)

// maxMessage caps one incoming line. Requests to lagdoc are tiny; this
// only stops a runaway client from filling memory.
const maxMessage = 1 << 20

// Server answers MCP requests with its tools. Requests are handled one at
// a time, in order.
type Server struct {
	Name, Version string
	Instructions  string
	Tools         []Tool

	// legacy is the revision negotiated by `initialize`, or "" if the
	// client hasn't sent one.
	legacy string
}

// Tool is one callable tool. Call returns the structured result, or an
// error that becomes a tool execution error (isError) the model can read.
type Tool struct {
	Name         string
	Title        string
	Description  string
	InputSchema  map[string]any
	OutputSchema map[string]any
	Annotations  Annotations
	Call         func(ctx context.Context, args map[string]json.RawMessage) (any, error)
}

// Annotations are hints about a tool's behaviour. Pointers so that false
// is written out rather than left to the defaults (destructive and
// open-world default to true).
type Annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in and writes responses to out until in ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 64<<10)
	w := bufio.NewWriter(out)
	for {
		line, tooLong, err := readLine(r)
		if len(bytes.TrimSpace(line)) > 0 || tooLong {
			var resp *response
			if tooLong {
				resp = &response{ID: json.RawMessage("null"), Error: &rpcError{Code: codeInvalidRequest, Message: "message too large"}}
			} else {
				resp = s.handle(ctx, line)
			}
			if resp != nil {
				resp.JSONRPC = "2.0"
				b, merr := marshal(resp)
				if merr != nil {
					b, _ = marshal(&response{JSONRPC: "2.0", ID: resp.ID, Error: &rpcError{Code: codeInternal, Message: "couldn't encode the result"}})
				}
				if _, werr := w.Write(append(b, '\n')); werr != nil {
					return werr
				}
				if werr := w.Flush(); werr != nil {
					return werr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil // the client closed stdin: time to exit
		} else if err != nil {
			return err
		}
	}
}

// readLine reads one newline-terminated message. A line over maxMessage
// is read to its end and discarded, and reported as too long.
func readLine(r *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) <= maxMessage {
			line = append(line, chunk...)
		} else {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = r.ReadSlice('\n')
			}
			return nil, true, err
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, false, err
		}
	}
}

// marshal writes compact JSON without HTML escaping. Compact JSON never
// contains a raw newline, as stdio framing requires.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func (s *Server) handle(ctx context.Context, line []byte) *response {
	null := json.RawMessage("null")
	trimmed := bytes.TrimSpace(line)
	if trimmed[0] == '[' {
		return &response{ID: null, Error: &rpcError{Code: codeInvalidRequest, Message: "batched requests aren't supported"}}
	}
	var m message
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return &response{ID: null, Error: &rpcError{Code: codeParse, Message: "parse error: " + err.Error()}}
	}
	if m.Method == "" {
		return nil // a response to us (we never send requests), or junk without an ID
	}
	if m.ID == nil {
		return nil // a notification (initialized, cancelled, …): nothing to answer
	}
	if m.JSONRPC != "2.0" || string(m.ID) == "null" {
		return &response{ID: null, Error: &rpcError{Code: codeInvalidRequest, Message: "not a JSON-RPC 2.0 request with an id"}}
	}
	result, err := s.dispatch(ctx, m)
	if err != nil {
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: codeInternal, Message: err.Error()}
		}
		return &response{ID: m.ID, Error: re}
	}
	return &response{ID: m.ID, Result: result}
}

type params struct {
	Meta map[string]json.RawMessage `json:"_meta"`
	// initialize
	ProtocolVersion string `json:"protocolVersion"`
	// tools/call
	Name      string                     `json:"name"`
	Arguments map[string]json.RawMessage `json:"arguments"`
}

// era is how one request is served: the revision, and whether it's modern.
type era struct {
	version string
	modern  bool
}

func (s *Server) dispatch(ctx context.Context, m message) (any, error) {
	var p params
	if len(m.Params) > 0 && string(m.Params) != "null" {
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
		}
	}

	if m.Method == "initialize" {
		return s.initialize(p), nil
	}

	var e era
	if raw, ok := p.Meta[metaVersion]; ok {
		var v string
		if json.Unmarshal(raw, &v) != nil || v == "" {
			return nil, &rpcError{Code: codeInvalidParams, Message: "_meta " + metaVersion + " must be a version string"}
		}
		if !slices.Contains(modernVersions, v) {
			return nil, &rpcError{Code: codeUnsupportedVersion, Message: "Unsupported protocol version",
				Data: map[string]any{"supported": modernVersions, "requested": v}}
		}
		if _, ok := p.Meta[metaCapabilities]; !ok {
			return nil, &rpcError{Code: codeInvalidParams, Message: "missing _meta " + metaCapabilities}
		}
		e = era{v, true}
	} else if s.legacy != "" {
		e = era{s.legacy, false}
	} else if m.Method == "ping" {
		e = era{LatestLegacy, false} // legacy clients may ping at any time
	} else {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf(
			"missing _meta %s: lagdoc speaks MCP %s (version on every request), or %s and earlier after initialize",
			metaVersion, ModernVersion, LatestLegacy)}
	}

	var result map[string]any
	switch m.Method {
	case "ping":
		result = map[string]any{}
	case "server/discover":
		if !e.modern {
			return nil, &rpcError{Code: codeMethodNotFound, Message: "server/discover is part of MCP " + ModernVersion}
		}
		result = map[string]any{
			"supportedVersions": modernVersions,
			"capabilities":      s.capabilities(),
			"instructions":      s.Instructions,
		}
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, t.describe(e))
		}
		result = map[string]any{"tools": tools}
	case "tools/call":
		var err error
		if result, err = s.call(ctx, e, p); err != nil {
			return nil, err
		}
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + m.Method}
	}
	if e.modern {
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{metaServerInfo: s.info()}
	}
	return result, nil
}

// initialize negotiates a legacy revision: the client's, if supported,
// otherwise the newest legacy one (the client then decides whether to
// carry on).
func (s *Server) initialize(p params) map[string]any {
	s.legacy = LatestLegacy
	if slices.Contains(legacyVersions, p.ProtocolVersion) {
		s.legacy = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": s.legacy,
		"capabilities":    s.capabilities(),
		"serverInfo":      s.info(),
		"instructions":    s.Instructions,
	}
}

func (s *Server) capabilities() map[string]any {
	return map[string]any{"tools": map[string]any{}}
}

func (s *Server) info() map[string]any {
	return map[string]any{"name": s.Name, "version": s.Version}
}

// structured says whether a revision has structuredContent and
// outputSchema (added in 2025-06-18), and tool titles.
func (e era) structured() bool { return e.modern || e.version >= "2025-06-18" }

func (t Tool) describe(e era) map[string]any {
	d := map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"inputSchema": t.InputSchema,
	}
	if e.version >= "2025-03-26" || e.modern {
		d["annotations"] = t.Annotations
	}
	if e.structured() {
		d["title"] = t.Title
		if t.OutputSchema != nil {
			d["outputSchema"] = t.OutputSchema
		}
	}
	return d
}

func (s *Server) call(ctx context.Context, e era, p params) (map[string]any, error) {
	i := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name })
	if i < 0 {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown tool: " + p.Name}
	}
	t := s.Tools[i]
	args := p.Arguments
	if args == nil {
		args = map[string]json.RawMessage{}
	}
	if err := checkArgs(t.InputSchema, args); err != nil {
		return toolError(err), nil
	}
	doc, err := t.Call(ctx, args)
	if err != nil {
		return toolError(err), nil
	}
	text, err := marshal(doc)
	if err != nil {
		return nil, err
	}
	// The JSON as text too, for clients that don't read structuredContent.
	result := map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": false,
	}
	if e.structured() {
		result["structuredContent"] = doc
	}
	return result, nil
}

func toolError(err error) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": err.Error()}},
		"isError": true,
	}
}

// checkArgs rejects arguments the tool doesn't take. Each tool checks the
// values itself, so a wrong one gets a specific message.
func checkArgs(schema map[string]any, args map[string]json.RawMessage) error {
	props, _ := schema["properties"].(map[string]any)
	for k := range args {
		if _, ok := props[k]; !ok {
			return fmt.Errorf("unknown argument %q", k)
		}
	}
	return nil
}
