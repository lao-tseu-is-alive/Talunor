// Package tui is Talunor's terminal UI: a Bubble Tea front-end over the agent
// loop, with Glamour rendering the assistant's markdown.
//
// The interesting bridge is turning the agent's streaming channel into Bubble
// Tea messages: waitForChunk reads one llm.Chunk and returns it as a tea.Msg;
// each Update re-issues it to pull the next, so tokens land live in the UI event
// loop without a background goroutine writing shared state. During streaming the
// answer is shown as raw text (cheap, no flicker); once complete it is
// re-rendered through Glamour.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/lao-tseu-is-alive/Talunor/internal/agent"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleInfo      = "info" // local command output (help, /mem, /list), shown dimmed.
)

var (
	userHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	asstHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")) // magenta
	dimStyle   = lipgloss.NewStyle().Faint(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // yellow
	statusBar  = lipgloss.NewStyle().Faint(true)
)

// Stream bridge messages.
type (
	streamMsg       llm.Chunk // one chunk from the agent's reply stream.
	streamClosedMsg struct{}  // the reply stream ended cleanly.
)

// waitForChunk reads the next chunk from ch as a tea.Cmd.
func waitForChunk(ch <-chan llm.Chunk) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return streamMsg(c)
	}
}

type turn struct {
	role     string
	raw      string // original text/markdown.
	rendered string // display block, recomputed on resize.
}

// Model is the Bubble Tea model. It is used as a pointer so the streaming
// accumulators are never copied by the event loop.
type Model struct {
	ctx          context.Context
	ag           *agent.Agent
	providerName string
	modelName    string
	memCount     int

	vp        viewport.Model
	ti        textinput.Model
	glam      *glamour.TermRenderer
	glamStyle string // "dark" | "light", detected once before the program starts.

	turns        []turn
	streaming    bool
	stream       <-chan llm.Chunk
	curContent   string
	curReasoning string
	pending      *llm.ApprovalRequest // a tool call awaiting the user's y/n.
	errText      string

	width, height int
	ready         bool
}

// New builds the model. Sub-view sizes and the Glamour renderer are set on the
// first WindowSizeMsg.
func New(ctx context.Context, ag *agent.Agent, providerName, modelName string, memCount int) *Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Talunor…  (/help for commands)"
	ti.Prompt = "you> "
	ti.Focus()
	ti.CharLimit = 0

	return &Model{
		ctx:          ctx,
		ag:           ag,
		providerName: providerName,
		modelName:    modelName,
		memCount:     memCount,
		vp:           viewport.New(0, 0),
		ti:           ti,
		glamStyle:    "dark", // safe default; Run overrides it after detection.
	}
}

// Run starts the program.
func Run(ctx context.Context, ag *agent.Agent, providerName, modelName string, memCount int) error {
	m := New(ctx, ag, providerName, modelName, memCount)
	// Detect the terminal background NOW, before Bubble Tea takes over the
	// terminal. Doing it here means the OSC 11 query/response is handled
	// synchronously; querying later (e.g. via glamour.WithAutoStyle inside the
	// event loop) leaks the response onto the screen as garbage.
	m.glamStyle = "dark"
	if !lipgloss.HasDarkBackground() {
		m.glamStyle = "light"
	}
	// No mouse capture: enabling it would let the app grab mouse events but
	// disable the terminal's own click-drag text selection. Keeping selection
	// (to copy/share a transcript) matters more than wheel scrolling — keyboard
	// scrolling (↑/↓, PgUp/PgDn) covers navigation.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd { return textinput.Blink }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		// A pending tool approval captures y/n before anything else.
		if m.pending != nil {
			return m.answerApproval(msg)
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if !m.streaming {
				return m, m.submit()
			}
			return m, nil
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyCtrlU, tea.KeyCtrlD, tea.KeyUp, tea.KeyDown:
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg) // scroll the transcript.
			return m, cmd
		}
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd

	case streamMsg:
		c := llm.Chunk(msg)
		if c.Err != nil {
			m.finishStream()
			m.errText = c.Err.Error()
			m.refresh()
			return m, nil
		}
		if c.Approval != nil {
			// Pause the stream and wait for the user's y/n (handled in KeyMsg).
			m.pending = c.Approval
			m.refresh()
			return m, nil
		}
		m.curContent += c.Content
		m.curReasoning += c.Reasoning
		m.refresh()
		return m, waitForChunk(m.stream)

	case streamClosedMsg:
		if answer := m.curContent; answer != "" {
			m.appendTurn(roleAssistant, answer)
		}
		m.finishStream()
		if n, err := m.ag.MemoryCount(m.ctx); err == nil {
			m.memCount = n
		}
		m.refresh()
		return m, nil
	}

	// Mouse events and anything else: let the viewport handle scrolling.
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	if !m.ready {
		return "initializing…"
	}
	return m.vp.View() + "\n" + m.status() + "\n" + m.ti.View()
}

// submit handles the current input: a slash command runs locally, anything else
// is sent through a cognitive turn.
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.ti.Value())
	if text == "" {
		return nil
	}
	m.ti.Reset()
	m.errText = ""

	if strings.HasPrefix(text, "/") {
		return m.runCommand(text)
	}

	m.appendTurn(roleUser, text)

	ch, err := m.ag.Turn(m.ctx, text)
	if err != nil {
		m.errText = err.Error()
		m.refresh()
		return nil
	}
	m.streaming = true
	m.stream = ch
	m.curContent = ""
	m.curReasoning = ""
	m.refresh()
	return waitForChunk(ch)
}

// answerApproval resolves a pending tool-approval request from a keypress: only
// 'y'/'Y' allows; Ctrl-C denies and quits; anything else denies. It then resumes
// the paused reply stream.
func (m *Model) answerApproval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	allow := msg.Type == tea.KeyRunes && len(msg.Runes) > 0 &&
		(msg.Runes[0] == 'y' || msg.Runes[0] == 'Y')
	m.pending.Respond(allow)
	m.pending = nil
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	m.refresh()
	return m, waitForChunk(m.stream) // resume streaming.
}

// runCommand handles a slash command, showing its output in the transcript.
// It returns tea.Quit for /exit, otherwise nil.
func (m *Model) runCommand(line string) tea.Cmd {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/exit", "/quit":
		return tea.Quit
	case "/help":
		m.appendInfo(m.ag.Help())
	case "/mem":
		if s, err := m.ag.MemoryStats(m.ctx); err != nil {
			m.errText = err.Error()
		} else {
			m.appendInfo(s)
		}
	case "/list":
		n := 10
		if len(fields) > 1 {
			if v, err := strconv.Atoi(fields[1]); err == nil {
				n = v
			}
		}
		if s, err := m.ag.ListMemories(m.ctx, n); err != nil {
			m.errText = err.Error()
		} else {
			m.appendInfo(s)
		}
	case "/forget":
		id, ok := agent.MemoryID(fields)
		if !ok {
			m.appendInfo("usage: /forget <id>  (the #id shown by /list)")
			break
		}
		if s, err := m.ag.ForgetMemory(m.ctx, id); err != nil {
			m.errText = err.Error()
		} else {
			m.appendInfo(s)
			if n, err := m.ag.MemoryCount(m.ctx); err == nil {
				m.memCount = n
			}
		}
	case "/clear":
		m.turns = nil
	default:
		m.appendInfo("unknown command " + fields[0] + " — try /help")
	}
	m.refresh()
	return nil
}

func (m *Model) appendInfo(text string) {
	m.turns = append(m.turns, turn{role: roleInfo, raw: text, rendered: m.renderTurn(roleInfo, text)})
}

func (m *Model) finishStream() {
	m.streaming = false
	m.stream = nil
	m.curContent = ""
	m.curReasoning = ""
	m.pending = nil
}

func (m *Model) appendTurn(role, raw string) {
	m.turns = append(m.turns, turn{role: role, raw: raw, rendered: m.renderTurn(role, raw)})
}

// renderTurn formats one completed turn for display.
func (m *Model) renderTurn(role, raw string) string {
	switch role {
	case roleUser:
		return userHeader.Render("You") + "\n" + m.wrap(raw)
	case roleAssistant:
		body := raw
		if m.glam != nil {
			if md, err := m.glam.Render(raw); err == nil {
				body = strings.Trim(md, "\n")
			} else {
				body = m.wrap(raw)
			}
		} else {
			body = m.wrap(raw)
		}
		return asstHeader.Render("Talunor") + "\n" + body
	case roleInfo:
		return dimStyle.Render(m.wrap(raw))
	default:
		return m.wrap(raw)
	}
}

// conversation builds the full transcript, including the in-progress reply.
func (m *Model) conversation() string {
	if len(m.turns) == 0 && !m.streaming {
		return dimStyle.Render("Talunor is ready. Ask anything — your memory persists across sessions.\nType /help for commands.")
	}
	var b strings.Builder
	for _, t := range m.turns {
		b.WriteString(t.rendered)
		b.WriteString("\n\n")
	}
	if m.streaming {
		b.WriteString(asstHeader.Render("Talunor"))
		b.WriteString("\n")
		if m.curReasoning != "" {
			b.WriteString(dimStyle.Render(m.wrap(m.curReasoning)))
			b.WriteString("\n\n")
		}
		b.WriteString(m.wrap(m.curContent))
	}
	if m.pending != nil {
		b.WriteString("\n\n")
		b.WriteString(warnStyle.Render(fmt.Sprintf("⚠️  Allow tool %q to run?", m.pending.Tool)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(m.wrap(m.pending.Args)))
		b.WriteString("\n")
		b.WriteString(warnStyle.Render("[y] allow    [any other key] deny"))
	}
	return b.String()
}

func (m *Model) refresh() {
	m.vp.SetContent(m.conversation())
	m.vp.GotoBottom()
}

func (m *Model) status() string {
	if m.errText != "" {
		return errStyle.Render("error: " + m.errText)
	}
	state := "ready"
	if m.streaming {
		state = "thinking…"
	}
	if m.pending != nil {
		state = "awaiting approval — press y to allow, any other key to deny"
	}
	return statusBar.Render(fmt.Sprintf("%s · %s · %d memories · %s · enter send · ↑↓/PgUp/PgDn scroll · ctrl+c quit",
		m.providerName, m.modelName, m.memCount, state))
}

// layout sizes the sub-views and rebuilds the Glamour renderer for the current
// width, then re-renders completed turns (word wrap depends on width).
func (m *Model) layout() {
	const statusH, inputH = 1, 1
	vpH := max(m.height-statusH-inputH-1, 3)
	m.vp.Width = m.width
	m.vp.Height = vpH
	m.ti.Width = m.width - len(m.ti.Prompt) - 1

	// Use an explicit, pre-detected style: glamour.WithAutoStyle would query the
	// terminal background here (inside the event loop) and leak the OSC 11
	// response onto the screen.
	if r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.glamStyle),
		glamour.WithWordWrap(m.contentWidth()),
	); err == nil {
		m.glam = r
	}
	for i := range m.turns {
		m.turns[i].rendered = m.renderTurn(m.turns[i].role, m.turns[i].raw)
	}
}

func (m *Model) contentWidth() int {
	return max(m.width-2, 20)
}

func (m *Model) wrap(s string) string {
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(s)
}
