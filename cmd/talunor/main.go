// Command talunor is the interactive agent over the cognitive loop
// (perceive → recall → reason → store). It uses a persistent database, so
// long-term memory accumulates across sessions and is recalled into later
// conversations.
//
// By default it launches the Bubble Tea TUI (markdown via Glamour). Pass --plain
// for a minimal line-based REPL, or --list to dump stored memories and exit.
//
// Commands (TUI and REPL): /help, /mem, /list [n], /clear (TUI), /exit.
//
// Environment: TALUNOR_MODEL, TALUNOR_OLLAMA_URL (see cmd/chat), TALUNOR_DB and
// the extension/model path overrides (see internal/memory.DefaultConfig).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/agent"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/render"
	"github.com/lao-tseu-is-alive/Talunor/internal/tui"
	"github.com/lao-tseu-is-alive/Talunor/internal/version"
)

func main() {
	plain := flag.Bool("plain", false, "use the plain line-based REPL instead of the TUI")
	list := flag.Int("list", 0, "dump the most recent N stored memories and exit")
	flag.Parse()
	if err := run(*plain, *list); err != nil {
		fmt.Fprintln(os.Stderr, "talunor: "+err.Error())
		os.Exit(1)
	}
}

func run(plain bool, list int) error {
	// Ctrl-C cancels the current turn / exits cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store, err := memory.Open(memory.DefaultConfig())
	if err != nil {
		return err
	}
	defer store.Close()

	// --list: inspect stored memory non-interactively, then exit.
	if list > 0 {
		mems, err := store.List(ctx, list)
		if err != nil {
			return err
		}
		fmt.Printf("database: %s\n%s", store.Path(), agent.FormatMemories(mems))
		return nil
	}

	model := envOr("TALUNOR_MODEL", llm.DefaultOllamaModel)
	var provider llm.Provider = llm.NewOllama(model)
	if url := os.Getenv("TALUNOR_OLLAMA_URL"); url != "" {
		provider = llm.NewOpenAICompatible("ollama", url, "", model)
	}

	ag := agent.New(store, provider, agent.DefaultConfig())
	n, _ := store.Count(ctx)

	if plain {
		fmt.Printf("%s\n%s → %s | %d memories | db: %s\ntype /help for commands\n\n",
			version.String(), provider.Name(), model, n, store.Path())
		return repl(ctx, ag)
	}
	return tui.Run(ctx, ag, provider.Name(), model, n)
}

func repl(ctx context.Context, ag *agent.Agent) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		fmt.Print("you> ")
		if !in.Scan() {
			fmt.Println()
			return in.Err() // nil on EOF (Ctrl-D).
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			done, err := command(ctx, line, ag)
			if done || err != nil {
				return err
			}
			continue // handled; do not send the command to the agent.
		}

		fmt.Print("\ntalunor> ")
		out, err := ag.Turn(ctx, line)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			fmt.Fprintf(os.Stderr, "\n[error: %v]\n\n", err)
			continue
		}
		if err := render.Stream(os.Stdout, out); err != nil {
			fmt.Fprintf(os.Stderr, "\n[stream error: %v]\n", err)
		}
		fmt.Println()
	}
}

// command handles slash commands. It returns done=true when the REPL should end.
func command(ctx context.Context, line string, ag *agent.Agent) (done bool, err error) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/exit", "/quit":
		return true, nil
	case "/help":
		fmt.Println(ag.Help())
	case "/mem":
		stats, err := ag.MemoryStats(ctx)
		if err != nil {
			return false, err
		}
		fmt.Println(stats)
	case "/list":
		n := 10
		if len(fields) > 1 {
			if v, e := strconv.Atoi(fields[1]); e == nil {
				n = v
			}
		}
		out, err := ag.ListMemories(ctx, n)
		if err != nil {
			return false, err
		}
		fmt.Println(out)
	default:
		fmt.Printf("unknown command %q — try /help\n", fields[0])
	}
	return false, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
