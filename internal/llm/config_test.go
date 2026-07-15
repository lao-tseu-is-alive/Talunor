package llm_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

func TestFromEnvOllamaDefault(t *testing.T) {
	// No provider set → Ollama with the default model.
	t.Setenv(llm.EnvProvider, "")
	t.Setenv(llm.EnvModel, "")
	p, model, err := llm.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if p.Name() != "ollama" {
		t.Errorf("name = %q; want ollama", p.Name())
	}
	if model != llm.DefaultOllamaModel {
		t.Errorf("model = %q; want %q", model, llm.DefaultOllamaModel)
	}
}

func TestFromEnvOllamaModelOverride(t *testing.T) {
	t.Setenv(llm.EnvProvider, "ollama")
	t.Setenv(llm.EnvModel, "qwen2.5-coder:14b")
	p, model, err := llm.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if p.Name() != "ollama" || model != "qwen2.5-coder:14b" {
		t.Errorf("got %s/%s; want ollama/qwen2.5-coder:14b", p.Name(), model)
	}
}

func TestFromEnvOpenRouter(t *testing.T) {
	t.Setenv(llm.EnvProvider, "openrouter")
	t.Setenv(llm.EnvOpenRouterKey, "sk-or-test")
	t.Setenv(llm.EnvModel, "")
	p, model, err := llm.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if p.Name() != "openrouter" {
		t.Errorf("name = %q; want openrouter", p.Name())
	}
	if model != llm.DefaultOpenRouterModel {
		t.Errorf("model = %q; want default %q", model, llm.DefaultOpenRouterModel)
	}
}

func TestFromEnvOpenRouterRequiresKey(t *testing.T) {
	t.Setenv(llm.EnvProvider, "openrouter")
	t.Setenv(llm.EnvOpenRouterKey, "") // missing
	if _, _, err := llm.FromEnv(); err == nil {
		t.Fatal("expected error when OPENROUTER_API_KEY is unset")
	}
}

func TestFromEnvUnknownProvider(t *testing.T) {
	t.Setenv(llm.EnvProvider, "wat")
	if _, _, err := llm.FromEnv(); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// TestOpenRouterSendsAuthAndHeaders verifies the OpenRouter adapter (built via
// FromEnv with a URL override) attaches the bearer key and the attribution
// header. The test server captures the request headers, then replays one SSE
// chunk so Collect returns cleanly.
func TestOpenRouterSendsAuthAndHeaders(t *testing.T) {
	var gotAuth, gotTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTitle = r.Header.Get("X-Title")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+delta("ok", "")+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	t.Setenv(llm.EnvProvider, "openrouter")
	t.Setenv(llm.EnvOpenRouterKey, "sk-or-xyz")
	t.Setenv(llm.EnvOpenRouterURL, srv.URL)
	t.Setenv(llm.EnvModel, "some/model")
	p, _, err := llm.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	if _, err := llm.Collect(t.Context(), p,
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}}, llm.Options{}); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if gotAuth != "Bearer sk-or-xyz" {
		t.Errorf("Authorization = %q; want %q", gotAuth, "Bearer sk-or-xyz")
	}
	if gotTitle != "Talunor" {
		t.Errorf("X-Title = %q; want Talunor", gotTitle)
	}
}
