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
)

var (
	userHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	asstHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")) // magenta
	dimStyle   = lipgloss.NewStyle().Faint(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red
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

	vp   viewport.Model
	ti   textinput.Model
	glam *glamour.TermRenderer

	turns        []turn
	streaming    bool
	stream       <-chan llm.Chunk
	curContent   string
	curReasoning string
	errText      string

	width, height int
	ready         bool
}

// New builds the model. Sub-view sizes and the Glamour renderer are set on the
// first WindowSizeMsg.
func New(ctx context.Context, ag *agent.Agent, providerName, modelName string, memCount int) *Model {
	ti := textinput.New()
	ti.Placeholder = "Ask Talunor…"
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
	}
}

// Run starts the program.
func Run(ctx context.Context, ag *agent.Agent, providerName, modelName string, memCount int) error {
	p := tea.NewProgram(New(ctx, ag, providerName, modelName, memCount),
		tea.WithAltScreen(), tea.WithMouseCellMotion())
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
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if !m.streaming {
				return m, m.submit()
			}
			return m, nil
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyCtrlU, tea.KeyCtrlD:
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

// submit sends the current input through a cognitive turn.
func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(m.ti.Value())
	if text == "" {
		return nil
	}
	m.ti.Reset()
	m.errText = ""
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

func (m *Model) finishStream() {
	m.streaming = false
	m.stream = nil
	m.curContent = ""
	m.curReasoning = ""
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
	default:
		return m.wrap(raw)
	}
}

// conversation builds the full transcript, including the in-progress reply.
func (m *Model) conversation() string {
	if len(m.turns) == 0 && !m.streaming {
		return dimStyle.Render("Talunor is ready. Ask anything — your memory persists across sessions.")
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
	return statusBar.Render(fmt.Sprintf("%s · %s · %d memories · %s · enter send · ctrl+c quit",
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

	if r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
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
