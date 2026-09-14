package cli

import (
	"os"
	"strings"
)

// Note marks output that yeet has condensed.
//
// Why this exists: in a 12-session A/B, agents that ran `git diff` under yeet's
// rewrite hook received a shape they did not recognise and concluded the tool
// was broken. They then spent turns diagnosing it — `which git`, `type git`,
// `git --version` — before re-running `command git diff` to bypass yeet. Each
// such detour cost a turn (~32k billed input tokens, since every turn re-sends
// the accumulated context) to avoid a few hundred bytes.
//
// A single line naming the transformation removes the ambiguity: the reader can
// tell that the shape is deliberate and that no content was lost. It costs ~60
// bytes against a turn that costs ~32,000 tokens.
//
// Set YEET_NO_NOTE=1 to suppress it.
func Note(_ string) string {
	if os.Getenv("YEET_NO_NOTE") == "1" {
		return ""
	}
	return "<note-for-llms>yeet condensed this output; nothing was lost, do not re-run.</note-for-llms>\n"
}

// WithNote prefixes rendered output with Note, unless the output is empty.
func WithNote(command, rendered string) string {
	if strings.TrimSpace(rendered) == "" {
		return rendered
	}
	return Note(command) + rendered
}
