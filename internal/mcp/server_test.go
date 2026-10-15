package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testServer() *Server {
	return &Server{
		Name: "lagdoc", Version: "1.2.3", Instructions: "read only",
		Tools: []Tool{{
			Name: "echo", Title: "Echo", Description: "says hi",
			InputSchema:  object(map[string]any{"fail": typ("boolean")}),
			OutputSchema: closed(map[string]any{"hi": str}),
			Annotations:  readOnly("Echo", false),
			Call: func(_ context.Context, args map[string]json.RawMessage) (any, error) {
				if string(args["fail"]) == "true" {
					return nil, errors.New("it failed, try again")
				}
				return map[string]any{"hi": "there\nfriend"}, nil
			},
		}},
	}
}

// session sends lines and returns one decoded response per output line.
func session(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("output line isn't one JSON message: %q", l)
		}
		got = append(got, m)
	}
	return got
}

const modernMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("not a result: %v", m)
	}
	return r
}

func errCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestLegacySession(t *testing.T) {
	got := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"two","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":5,"method":"server/discover"}`,
	)
	if len(got) != 5 {
		t.Fatalf("notifications get no answer; want 5 responses, got %d: %v", len(got), got)
	}
	init := result(t, got[0])
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["version"] != "1.2.3" || init["instructions"] != "read only" {
		t.Errorf("initialize: %v", init)
	}
	if _, ok := init["resultType"]; ok {
		t.Error("legacy results have no resultType")
	}
	if got[1]["id"] != "two" {
		t.Errorf("string ids are echoed: %v", got[1]["id"])
	}
	tool := result(t, got[1])["tools"].([]any)[0].(map[string]any)
	if tool["outputSchema"] == nil || tool["title"] != "Echo" || tool["annotations"] == nil {
		t.Errorf("2025-06-18 has titles, annotations and output schemas: %v", tool)
	}
	call := result(t, got[2])
	if call["isError"] != false || call["structuredContent"].(map[string]any)["hi"] != "there\nfriend" {
		t.Errorf("call: %v", call)
	}
	if text := call["content"].([]any)[0].(map[string]any)["text"]; text != `{"hi":"there\nfriend"}` {
		t.Errorf("the JSON is also returned as text: %v", text)
	}
	if len(result(t, got[3])) != 0 {
		t.Errorf("ping: %v", got[3])
	}
	if errCode(got[4]) != codeMethodNotFound {
		t.Errorf("server/discover is modern-only: %v", got[4])
	}
}

func TestLegacyVersionNegotiation(t *testing.T) {
	for requested, want := range map[string]string{
		"2024-11-05": "2024-11-05", "2025-11-25": "2025-11-25", "2030-01-01": LatestLegacy, "": LatestLegacy,
	} {
		got := session(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+requested+`"}}`)
		if v := result(t, got[0])["protocolVersion"]; v != want {
			t.Errorf("requested %q: got %v, want %s", requested, v, want)
		}
	}
	got := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}}`,
	)
	tool := result(t, got[1])["tools"].([]any)[0].(map[string]any)
	for _, k := range []string{"annotations", "outputSchema", "title"} {
		if _, ok := tool[k]; ok {
			t.Errorf("2024-11-05 has no %s", k)
		}
	}
	if _, ok := result(t, got[2])["structuredContent"]; ok {
		t.Error("2024-11-05 has no structuredContent")
	}
}

func TestModernSession(t *testing.T) {
	got := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+modernMeta+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+modernMeta+`}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo",`+modernMeta+`}}`,
		`{"jsonrpc":"2.0","id":4,"method":"ping","params":{`+modernMeta+`}}`,
	)
	for i, m := range got {
		r := result(t, m)
		if r["resultType"] != "complete" {
			t.Errorf("response %d has no resultType: %v", i, r)
		}
		info := r["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
		if info["name"] != "lagdoc" {
			t.Errorf("response %d: serverInfo %v", i, info)
		}
	}
	d := result(t, got[0])
	if v := d["supportedVersions"].([]any); len(v) != 1 || v[0] != ModernVersion || d["capabilities"].(map[string]any)["tools"] == nil {
		t.Errorf("discover: %v", d)
	}
	if result(t, got[2])["structuredContent"] == nil {
		t.Error("modern calls have structuredContent")
	}
}

func TestModernErrors(t *testing.T) {
	got := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25","io.modelcontextprotocol/clientCapabilities":{}}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/list","params":{`+modernMeta+`}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope",`+modernMeta+`}}`,
	)
	if errCode(got[0]) != codeUnsupportedVersion {
		t.Errorf("legacy version without initialize: %v", got[0])
	}
	data := got[0]["error"].(map[string]any)["data"].(map[string]any)
	if data["requested"] != "2025-11-25" || data["supported"].([]any)[0] != ModernVersion {
		t.Errorf("error data: %v", data)
	}
	if errCode(got[1]) != codeInvalidParams || !strings.Contains(got[1]["error"].(map[string]any)["message"].(string), "clientCapabilities") {
		t.Errorf("missing capabilities: %v", got[1])
	}
	if msg := got[2]["error"].(map[string]any)["message"].(string); errCode(got[2]) != codeInvalidParams || !strings.Contains(msg, "2026-07-28") || !strings.Contains(msg, "2025-11-25") {
		t.Errorf("no version and no initialize must name the versions: %v", got[2])
	}
	if errCode(got[3]) != codeMethodNotFound {
		t.Errorf("unknown method: %v", got[3])
	}
	if errCode(got[4]) != codeInvalidParams {
		t.Errorf("unknown tool: %v", got[4])
	}
}

func TestToolErrorsAreResults(t *testing.T) {
	got := session(t, testServer(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"fail":true},`+modernMeta+`}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"bogus":1},`+modernMeta+`}}`,
	)
	for i, want := range []string{"it failed, try again", `unknown argument "bogus"`} {
		r := result(t, got[i])
		if r["isError"] != true || r["content"].([]any)[0].(map[string]any)["text"] != want {
			t.Errorf("call %d: %v", i, r)
		}
	}
}

func TestMalformedInput(t *testing.T) {
	got := session(t, testServer(),
		`not json`,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":9,"result":{}}`, // a response from the client: ignored
		``,
	)
	want := []float64{codeParse, codeInvalidRequest, codeInvalidRequest, codeInvalidRequest}
	if len(got) != len(want) {
		t.Fatalf("got %d responses: %v", len(got), got)
	}
	for i, w := range want {
		if errCode(got[i]) != w || got[i]["id"] != nil {
			t.Errorf("line %d: %v", i, got[i])
		}
	}
}

func TestOversizedLine(t *testing.T) {
	huge := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", maxMessage) + `"}}`
	got := session(t, testServer(), huge, `{"jsonrpc":"2.0","id":2,"method":"ping","params":{`+modernMeta+`}}`)
	if len(got) != 2 || errCode(got[0]) != codeInvalidRequest || got[1]["id"] != float64(2) {
		t.Errorf("an oversized line is refused and the next one still served: %v", got)
	}
}
