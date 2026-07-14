package llm_test

import (
	"context"
	"fmt"
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
