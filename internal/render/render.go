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

// Stream writes the chunks from ch to w until the channel closes, returning the
// first streamed error. It always leaves the terminal on a fresh line with no
// dangling ANSI state.
func Stream(w io.Writer, ch <-chan llm.Chunk) error {
	inReasoning := false
	for c := range ch {
		if c.Err != nil {
			if inReasoning {
				fmt.Fprint(w, ansiReset)
			}
			return c.Err
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
	if inReasoning {
		fmt.Fprint(w, ansiReset)
	}
	fmt.Fprintln(w)
	return nil
}
