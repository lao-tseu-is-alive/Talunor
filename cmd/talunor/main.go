// Command talunor is the interactive agent over the cognitive loop
// (perceive → recall → reason → store). It uses a persistent database, so
// long-term memory accumulates across sessions and is recalled into later
// conversations.
//
// By default it launches the Bubble Tea TUI (markdown via Glamour). Pass --plain
// for a minimal line-based REPL, or --list to dump stored memories and exit.
//
// Commands (TUI and REPL): /help, /mem, /list [n], /forget <id>, /clear (TUI),
// /exit.
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
	"github.com/lao-tseu-is-alive/Talunor/internal/config"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/render"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
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

	// Load .env (if present) before reading any configuration; real env wins.
	if err := config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

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

	provider, model, err := llm.FromEnv()
	if err != nil {
		return err
	}

	cfg := agent.DefaultConfig()
	// Reflection makes a second model call per turn; on a paid provider that
	// doubles cost, so allow disabling it with TALUNOR_REFLECT=0.
	if !envBool("TALUNOR_REFLECT", true) {
		cfg.Extractor = agent.DisableReflection()
	}
	// Tools: the agent can do arithmetic, tell the time, and search its own
	// memory. Disable with TALUNOR_TOOLS=0 (e.g. for a model without tool support).
	if envBool("TALUNOR_TOOLS", true) {
		cfg.Tools = tools.NewRegistry(
			tools.Calculator{},
			tools.Clock{},
			tools.NewRecallMemory(store),
		)
	}
	ag := agent.New(store, provider, cfg)
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
	case "/forget":
		id, ok := agent.MemoryID(fields)
		if !ok {
			fmt.Println("usage: /forget <id>  (the #id shown by /list)")
			return false, nil
		}
		msg, err := ag.ForgetMemory(ctx, id)
		if err != nil {
			return false, err
		}
		fmt.Println(msg)
	default:
		fmt.Printf("unknown command %q — try /help\n", fields[0])
	}
	return false, nil
}

// envBool reads a boolean-ish env var; "0", "false", "no", "off" (any case) are
// false, anything else non-empty is true, and unset returns def.
func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
