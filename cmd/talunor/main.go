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
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lao-tseu-is-alive/Talunor/internal/agent"
	"github.com/lao-tseu-is-alive/Talunor/internal/config"
	"github.com/lao-tseu-is-alive/Talunor/internal/history"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/render"
	"github.com/lao-tseu-is-alive/Talunor/internal/sandbox"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
	"github.com/lao-tseu-is-alive/Talunor/internal/tui"
	"github.com/lao-tseu-is-alive/Talunor/internal/version"
	"github.com/lao-tseu-is-alive/Talunor/internal/webfetch"
)

func main() {
	plain := flag.Bool("plain", false, "use the plain line-based REPL instead of the TUI")
	list := flag.Int("list", 0, "dump the most recent N stored memories and exit")
	reembed := flag.Bool("reembed", false, "re-embed every stored memory with the current model, then exit")
	flag.Parse()
	if err := run(*plain, *list, *reembed); err != nil {
		fmt.Fprintln(os.Stderr, "talunor: "+err.Error())
		os.Exit(1)
	}
}

func run(plain bool, list int, reembed bool) error {
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

	// --reembed: realign every stored memory with the currently loaded embedding
	// model, then exit. Run this after the model changes (a startup warning points
	// here when the provenance check trips).
	if reembed {
		return reembedMemories(ctx, store)
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
	// TALUNOR_DEBUG turns on a structured trace of recall/tools/reflection (see
	// debugLogger). It logs to a file by default so the TUI's screen stays clean;
	// the closer is deferred until the program exits.
	dbg, dbgClose, dbgDest, err := debugLogger(store.Path())
	if err != nil {
		return err
	}
	if dbgClose != nil {
		defer dbgClose.Close()
	}
	if dbg != nil {
		cfg.Debug = dbg
		fmt.Fprintf(os.Stderr, "talunor: debug trace → %s\n", dbgDest)
	}
	// Tools: the agent can do arithmetic, tell the time, and search its own
	// memory. Disable with TALUNOR_TOOLS=0 (e.g. for a model without tool support).
	if envBool("TALUNOR_TOOLS", true) {
		reg := tools.NewRegistry(
			tools.Calculator{},
			tools.Clock{},
			tools.NewRecallMemory(store),
		)
		// The sandboxed bash tool is opt-in (TALUNOR_BASH=1) and approval-gated.
		// If the sandbox can't be set up on this host, warn and carry on without
		// it rather than failing the whole app.
		if envBool("TALUNOR_BASH", false) {
			if sb, err := sandbox.FromEnv(); err != nil {
				fmt.Fprintf(os.Stderr, "talunor: bash tool disabled: %v\n", err)
			} else {
				reg.Register(tools.NewBash(sb, sandbox.DefaultLimits()))
				fmt.Fprintf(os.Stderr, "talunor: bash tool enabled (sandbox: %s, approval-gated)\n", sb.Name())
			}
		}
		// The web_fetch tool is opt-in (TALUNOR_WEBFETCH=1). It is SSRF-guarded and
		// approval-gated, except for hosts on TALUNOR_WEBFETCH_ALLOW which skip the
		// prompt (the guard still applies).
		if envBool("TALUNOR_WEBFETCH", false) {
			lim := webFetchLimits()
			allow := splitList(os.Getenv("TALUNOR_WEBFETCH_ALLOW"))
			reg.Register(tools.NewWebFetch(webfetch.New(lim, nil), allow))
			msg := "talunor: web_fetch tool enabled (SSRF-guarded, approval-gated"
			if len(allow) > 0 {
				msg += fmt.Sprintf(", allowlist: %s", strings.Join(allow, ","))
			}
			fmt.Fprintln(os.Stderr, msg+")")
		}
		cfg.Tools = reg
	}
	ag := agent.New(store, provider, cfg)
	n, _ := store.Count(ctx)

	// If the embedding stack changed since these memories were written, recall of
	// the old ones is degraded until they are re-embedded. Warn once at startup and
	// point at the fix (the check itself is silent when everything lines up).
	if p := store.Provenance(); p != memory.ProvenanceOK {
		fmt.Fprintf(os.Stderr,
			"talunor: ⚠ embedding provenance %s\n         recall of older memories may be degraded — run `talunor --reembed` to realign.\n",
			p)
	}

	// Persistent, deduplicated prompt history (recalled with ↑/↓ in the TUI),
	// stored next to the memory database so it survives across sessions.
	hist := history.New(history.DefaultPath(store.Path()))

	if plain {
		fmt.Printf("%s\n%s → %s | %d memories | db: %s\ntype /help for commands\n\n",
			version.String(), provider.Name(), model, n, store.Path())
		return repl(ctx, ag, hist)
	}
	return tui.Run(ctx, ag, hist, provider.Name(), model, n)
}

// repl is the plain line-based front-end. It shares hist with the TUI so
// prompts recorded here are recallable there; the scanner-based input cannot do
// ↑/↓ line editing itself, so recall in this mode is write-only.
func repl(ctx context.Context, ag *agent.Agent, hist *history.History) error {
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
		hist.Add(line) // record for cross-session recall (used by the TUI).
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
		if err := render.StreamWithApproval(os.Stdout, out, approver(in)); err != nil {
			fmt.Fprintf(os.Stderr, "\n[stream error: %v]\n", err)
		}
		fmt.Println()
	}
}

// approver prompts on the terminal for a tool-approval decision, reading the
// answer from the REPL's scanner (safe: no other read is in flight during a
// turn). Only an explicit "y"/"yes" allows; anything else (incl. EOF) denies.
func approver(in *bufio.Scanner) render.ApproveFunc {
	return func(req *llm.ApprovalRequest) bool {
		fmt.Printf("\n\x1b[33m⚠️  Talunor wants to run tool %q with:\x1b[0m\n    %s\n[y/N] ",
			req.Tool, req.Args)
		if !in.Scan() {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(in.Text())) {
		case "y", "yes":
			return true
		default:
			return false
		}
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
	case "/debug":
		fmt.Println(ag.DebugCommand(fields))
	default:
		fmt.Printf("unknown command %q — try /help\n", fields[0])
	}
	return false, nil
}

// reembedMemories recomputes every memory's embedding with the currently loaded
// model, printing progress, and reports the result. It is the fix for a tripped
// embedding-provenance check: after a model change, old vectors sit in a stale
// space and recall degrades until they are realigned.
func reembedMemories(ctx context.Context, store *memory.Store) error {
	before := store.Provenance()
	fmt.Printf("re-embedding all memories with %s (dim %d)…\n", store.EmbedModelName(), store.Dim())
	n, err := store.ReEmbed(ctx, func(done, total int) {
		fmt.Printf("\r  %d/%d", done, total)
	})
	if err != nil {
		fmt.Println()
		return fmt.Errorf("re-embed: %w", err)
	}
	if n > 0 {
		fmt.Println()
	}
	fmt.Printf("✓ re-embedded %d memories (provenance: %s → %s)\n", n, before, store.Provenance())
	return nil
}

// debugLogger builds the agent's trace logger from TALUNOR_DEBUG:
//
//	unset / 0 / false / no / off → disabled (nil logger).
//	stderr                       → the terminal's stderr (handy with --plain).
//	1 / true / yes / on          → a file "talunor-debug.log" next to the DB.
//	<path>                       → that file (created/appended).
//
// It returns the logger, an optional Closer (nil for stderr/disabled), and a
// human-readable destination for the startup notice. Logging to a file by
// default matters: the TUI owns the alt-screen, so trace lines on stdout/stderr
// would corrupt it — a file you can `tail -f` keeps the two streams apart.
func debugLogger(dbPath string) (*slog.Logger, io.Closer, string, error) {
	v := strings.TrimSpace(os.Getenv("TALUNOR_DEBUG"))
	switch strings.ToLower(v) {
	case "", "0", "false", "no", "off":
		return nil, nil, "", nil
	case "stderr":
		return newTextLogger(os.Stderr), nil, "stderr", nil
	}

	dest := v
	if lv := strings.ToLower(v); lv == "1" || lv == "true" || lv == "yes" || lv == "on" {
		dir := filepath.Dir(dbPath)
		if dir == "" {
			dir = "."
		}
		dest = filepath.Join(dir, "talunor-debug.log")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, "", fmt.Errorf("open debug log %q: %w", dest, err)
	}
	return newTextLogger(f), f, dest, nil
}

// newTextLogger returns a slog text logger at debug level over w.
func newTextLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// webFetchLimits builds the web_fetch limits from DefaultLimits, overriding
// MaxBytes (TALUNOR_WEBFETCH_MAX_BYTES, bytes) and Timeout
// (TALUNOR_WEBFETCH_TIMEOUT, a Go duration like "10s") when set.
func webFetchLimits() webfetch.Limits {
	lim := webfetch.DefaultLimits()
	if v := strings.TrimSpace(os.Getenv("TALUNOR_WEBFETCH_MAX_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			lim.MaxBytes = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("TALUNOR_WEBFETCH_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			lim.Timeout = d
		}
	}
	return lim
}

// splitList parses a comma-separated env value into trimmed, non-empty items.
func splitList(v string) []string {
	var out []string
	for p := range strings.SplitSeq(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
