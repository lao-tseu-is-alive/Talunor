// Command chat is the Layer 3 smoke test for the LLM provider: it sends one
// prompt to a local Ollama model and streams the reply to the terminal. Thinking
// models (e.g. qwen3) show their reasoning dimmed, then the answer in full
// brightness — a visible reminder that "reasoning" and "answer" are distinct.
//
// Usage:
//
//	chat "your prompt here"      # prompt from arguments
//	echo "your prompt" | chat    # prompt from stdin
//
// Environment:
//
//	TALUNOR_MODEL       Ollama model (default qwen3:latest)
//	TALUNOR_OLLAMA_URL  Ollama OpenAI-compatible base URL
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/render"
	"github.com/lao-tseu-is-alive/Talunor/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "chat: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	prompt, err := readPrompt()
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt given (pass as arguments or via stdin)")
	}

	model := envOr("TALUNOR_MODEL", llm.DefaultOllamaModel)
	provider := llm.NewOllama(model)
	if url := os.Getenv("TALUNOR_OLLAMA_URL"); url != "" {
		provider = llm.NewOpenAICompatible("ollama", url, "", model)
	}

	fmt.Fprintf(os.Stderr, "%s\n%s → %s\n\n", version.String(), provider.Name(), model)

	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are Talunor, a concise and helpful assistant."},
		{Role: llm.RoleUser, Content: prompt},
	}

	ch, err := provider.Chat(ctx, msgs, llm.Options{})
	if err != nil {
		return err
	}
	return render.Stream(os.Stdout, ch)
}

func readPrompt() (string, error) {
	if len(os.Args) > 1 {
		return strings.Join(os.Args[1:], " "), nil
	}
	// No args: read the prompt from stdin (e.g. piped input).
	info, _ := os.Stdin.Stat()
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", nil // interactive terminal with no args → empty prompt.
	}
	data, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
