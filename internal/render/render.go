// Package render turns an LLM chunk stream into terminal output. It is the
// shared console renderer used by cmd/chat and cmd/talunor: a thinking model's
// reasoning is shown dimmed, the answer in full brightness, with a blank line
// separating the two once the answer begins.
package render

import (
	"fmt"
	"io"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

const (
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

// ApproveFunc decides an approval request (true = allow). It owns its own
// prompt/response IO. A nil ApproveFunc denies every request (fail closed).
type ApproveFunc func(*llm.ApprovalRequest) bool

// Stream renders ch to w, denying any approval request (there are none in a
// plain, tool-less stream). See StreamWithApproval to handle them.
func Stream(w io.Writer, ch <-chan llm.Chunk) error {
	return StreamWithApproval(w, ch, nil)
}

// StreamWithApproval writes the chunks from ch to w until the channel closes,
// returning the first streamed error. On an approval request it clears any dim
// state, asks approve, and responds. It always leaves the terminal on a fresh
// line with no dangling ANSI state.
func StreamWithApproval(w io.Writer, ch <-chan llm.Chunk, approve ApproveFunc) error {
	inReasoning := false
	reset := func() {
		if inReasoning {
			fmt.Fprint(w, ansiReset)
			inReasoning = false
		}
	}
	for c := range ch {
		if c.Err != nil {
			reset()
			return c.Err
		}
		if c.Approval != nil {
			reset()
			allow := approve != nil && approve(c.Approval)
			c.Approval.Respond(allow)
			continue
		}
		if c.Reasoning != "" {
			if !inReasoning {
				fmt.Fprint(w, ansiDim)
				inReasoning = true
			}
			fmt.Fprint(w, c.Reasoning)
		}
		if c.Content != "" {
			if inReasoning {
				fmt.Fprint(w, ansiReset+"\n\n")
				inReasoning = false
			}
			fmt.Fprint(w, c.Content)
		}
	}
	reset()
	fmt.Fprintln(w)
	return nil
}
