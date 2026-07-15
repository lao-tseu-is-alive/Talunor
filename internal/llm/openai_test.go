package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

// sseServer returns a test server that replays the given SSE lines (each is sent
// as one `data: <line>` event, flushed) then closes the stream.
func sseServer(t *testing.T, status int, events ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"boom"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
}

func delta(content, reasoning string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q,"reasoning":%q},"finish_reason":null}]}`,
		content, reasoning)
}

func TestChatStreamAssembly(t *testing.T) {
	srv := sseServer(t, http.StatusOK,
		delta("Hel", ""),
		delta("lo, ", ""),
		delta("world", ""),
		"[DONE]",
	)
	defer srv.Close()

	p := llm.NewOpenAICompatible("test", srv.URL, "", "m")
	got, err := llm.Collect(context.Background(), p,
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got != "Hello, world" {
		t.Errorf("got %q; want %q", got, "Hello, world")
	}
}

func TestChatSeparatesReasoningFromContent(t *testing.T) {
	srv := sseServer(t, http.StatusOK,
		delta("", "thinking a"),
		delta("", "bout it"),
		delta("answer", ""),
		"[DONE]",
	)
	defer srv.Close()

	p := llm.NewOpenAICompatible("test", srv.URL, "", "m")
	ch, err := p.Chat(context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var content, reasoning strings.Builder
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("chunk err: %v", c.Err)
		}
		content.WriteString(c.Content)
		reasoning.WriteString(c.Reasoning)
	}
	if content.String() != "answer" {
		t.Errorf("content = %q; want %q", content.String(), "answer")
	}
	if reasoning.String() != "thinking about it" {
		t.Errorf("reasoning = %q; want %q", reasoning.String(), "thinking about it")
	}
}

func TestChatNon200IsSetupError(t *testing.T) {
	srv := sseServer(t, http.StatusInternalServerError)
	defer srv.Close()

	p := llm.NewOpenAICompatible("test", srv.URL, "", "m")
	_, err := p.Chat(context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{})
	if err == nil {
		t.Fatal("expected setup error on non-200")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should mention the status", err.Error())
	}
}

func TestChatInStreamError(t *testing.T) {
	srv := sseServer(t, http.StatusOK,
		delta("partial", ""),
		`{"error":{"message":"model overloaded"}}`,
	)
	defer srv.Close()

	p := llm.NewOpenAICompatible("test", srv.URL, "", "m")
	_, err := llm.Collect(context.Background(), p,
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{})
	if err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("expected in-stream error, got %v", err)
	}
}

func TestChatConnectionRefusedIsError(t *testing.T) {
	// Nothing listening on this port.
	p := llm.NewOpenAICompatible("test", "http://127.0.0.1:0", "", "m")
	_, err := p.Chat(context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// TestChatAssemblesStreamedToolCalls verifies fragmented tool_calls deltas are
// accumulated (id/name once, arguments concatenated) and emitted as one terminal
// chunk, and that the request carries the offered tools.
func TestChatAssemblesStreamedToolCalls(t *testing.T) {
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sentBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		// tool_call fragmented across three deltas, then finish + DONE.
		frags := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"calculator","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"expression\":\"12"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"*7\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		}
		for _, f := range frags {
			fmt.Fprintf(w, "data: %s\n\n", f)
		}
	}))
	defer srv.Close()

	p := llm.NewOpenAICompatible("test", srv.URL, "", "m")
	ch, err := p.Chat(context.Background(),
		[]llm.Message{{Role: llm.RoleUser, Content: "what is 12*7?"}},
		llm.Options{Tools: []llm.ToolSpec{{
			Name: "calculator", Description: "math", Parameters: []byte(`{"type":"object"}`),
		}}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var calls []llm.ToolCall
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("chunk err: %v", c.Err)
		}
		if len(c.ToolCalls) > 0 {
			calls = c.ToolCalls
		}
	}
	if len(calls) != 1 {
		t.Fatalf("got %d tool calls; want 1", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Name != "calculator" {
		t.Errorf("tool call id/name = %q/%q; want call_1/calculator", calls[0].ID, calls[0].Name)
	}
	if calls[0].Args != `{"expression":"12*7"}` {
		t.Errorf("assembled args = %q; want the joined JSON", calls[0].Args)
	}
	// The request must have offered the tool.
	if !strings.Contains(sentBody, `"tools"`) || !strings.Contains(sentBody, `"calculator"`) {
		t.Errorf("request body missing tools: %s", sentBody)
	}
}

// TestToolCallMarshalsToOpenAIShape checks the echo shape used in follow-ups.
func TestToolCallMarshalsToOpenAIShape(t *testing.T) {
	b, err := json.Marshal(llm.ToolCall{ID: "call_9", Name: "clock", Args: `{"timezone":"UTC"}`})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"id":"call_9"`, `"type":"function"`, `"name":"clock"`, `"arguments":"{\"timezone\":\"UTC\"}"`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshaled tool call %s missing %s", got, want)
		}
	}
}
