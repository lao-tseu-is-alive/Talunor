package llm

import (
	"fmt"
	"os"
	"strings"
)

// Environment variables that select and configure the chat provider.
const (
	// EnvProvider picks the backend: "ollama" (default) or "openrouter".
	EnvProvider = "TALUNOR_PROVIDER"
	// EnvModel overrides the model for the selected provider.
	EnvModel = "TALUNOR_MODEL"
	// EnvOllamaURL overrides the Ollama base URL.
	EnvOllamaURL = "TALUNOR_OLLAMA_URL"
	// EnvOpenRouterURL overrides the OpenRouter base URL (rarely needed).
	EnvOpenRouterURL = "TALUNOR_OPENROUTER_URL"
	// EnvOpenRouterKey is the OpenRouter API key (required for that provider).
	EnvOpenRouterKey = "OPENROUTER_API_KEY"
)

// FromEnv builds the chat provider selected by TALUNOR_PROVIDER (default
// "ollama"), reading the provider-specific variables above. It returns the
// provider and the resolved model name (handy for status lines), or an error
// when a required variable is missing or the provider is unknown.
//
// One adapter serves both backends because Ollama and OpenRouter speak the same
// OpenAI-compatible API; only the base URL, key, and headers differ.
func FromEnv() (Provider, string, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv(EnvProvider)))
	model := strings.TrimSpace(os.Getenv(EnvModel))

	switch name {
	case "", "ollama":
		p := NewOllama(model)
		if url := os.Getenv(EnvOllamaURL); url != "" {
			p = NewOpenAICompatible("ollama", url, "", p.Model())
		}
		return p, p.Model(), nil

	case "openrouter":
		key := strings.TrimSpace(os.Getenv(EnvOpenRouterKey))
		if key == "" {
			return nil, "", fmt.Errorf("%s=openrouter requires %s to be set", EnvProvider, EnvOpenRouterKey)
		}
		p := NewOpenRouter(model, key)
		if url := os.Getenv(EnvOpenRouterURL); url != "" {
			// Keep the attribution headers when overriding the URL.
			custom := NewOpenAICompatible("openrouter", url, key, p.Model())
			custom.headers = p.headers
			p = custom
		}
		return p, p.Model(), nil

	default:
		return nil, "", fmt.Errorf("unknown %s=%q (want: ollama, openrouter)", EnvProvider, name)
	}
}
