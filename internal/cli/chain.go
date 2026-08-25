package cli

import "strings"

// ─── Shell chain splitting ────────────────────────────────────────────────────
// Most commands an agent issues are chains: `cat a.ts && ls src`,
// `grep -rn foo src | head -20`. Matching a rewrite rule against the whole
// string only ever fires on the first verb, so every chain used to pass through
// raw. Splitting the chain and rewriting each segment on its own recovers that.

// chainPart is one command in a shell chain. Concatenating sep+lead+cmd+tail
// over every part reproduces the original string byte for byte, so a chain in
// which nothing was rewritten can be handed back untouched.
type chainPart struct {
	// sep is the operator that introduced this part ("" for the first one).
	sep string
	// lead and tail are the whitespace around cmd, kept so reassembly is exact.
	lead string
	cmd  string
	tail string
}

// stdinFed reports whether this part reads the previous part's output rather
// than its own arguments. `git log | grep fix` is the case that matters: the
// grep there has no path operand, and `yeet grep` would search the working
// tree instead of the piped text — a wrong answer, not just a noisier one.
func (p chainPart) stdinFed() bool { return p.sep == "|" }

// splitChain breaks a command line on the shell operators that separate whole
// commands, honouring quotes so a `;` inside a pattern is not an operator.
//
// It returns ok=false for constructs whose meaning cannot be preserved by
// rewriting the pieces independently: command substitution, subshells,
// backgrounding, and `case` arms. Those pass through exactly as they do today.
func splitChain(raw string) ([]chainPart, bool) {
	var parts []chainPart
	var cur strings.Builder
	sep := ""
	var quote rune
	escaped := false

	flush := func(nextSep string) {
		text := cur.String()
		trimmed := strings.TrimSpace(text)
		lead := text[:len(text)-len(strings.TrimLeft(text, " \t\n\r"))]
		tail := ""
		if trimmed != "" {
			tail = text[len(lead)+len(trimmed):]
		} else {
			lead = text
		}
		parts = append(parts, chainPart{sep: sep, lead: lead, cmd: trimmed, tail: tail})
		cur.Reset()
		sep = nextSep
	}

	runes := []rune(raw)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if quote != 0 {
			if r == '\\' && quote == '"' {
				cur.WriteRune(r)
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
			continue
		}

		switch r {
		case '\\':
			cur.WriteRune(r)
			escaped = true
		case '\'', '"':
			quote = r
			cur.WriteRune(r)
		case '`', '(', ')':
			// Backticks and `$(...)` feed one command's output to another, and a
			// subshell has its own scope. Rewriting inside either changes what
			// the outer command receives.
			return nil, false
		case ';':
			if i+1 < len(runes) && runes[i+1] == ';' {
				return nil, false // `;;` — a case arm, not a command separator
			}
			flush(";")
		case '\n':
			flush("\n")
		case '&':
			if i+1 < len(runes) && runes[i+1] == '&' {
				flush("&&")
				i++
				continue
			}
			// A bare `&` either backgrounds the command or is part of a
			// redirection (`2>&1`, `&>log`). Redirections are handled by
			// segmentRewritable; backgrounding is not something to rewrite into.
			if i > 0 && (runes[i-1] == '>' || runes[i-1] == '<') {
				cur.WriteRune(r)
				continue
			}
			if i+1 < len(runes) && runes[i+1] == '>' {
				cur.WriteRune(r)
				continue
			}
			return nil, false
		case '|':
			if i+1 < len(runes) && runes[i+1] == '|' {
				flush("||")
				i++
				continue
			}
			flush("|")
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 || escaped {
		return nil, false // unbalanced quoting — the split cannot be trusted
	}
	flush("")

	return parts, true
}

// joinChain reverses splitChain. Passing the parts through unmodified returns
// the original string.
func joinChain(parts []chainPart) string {
	var buf strings.Builder
	for _, p := range parts {
		buf.WriteString(p.sep)
		buf.WriteString(p.lead)
		buf.WriteString(p.cmd)
		buf.WriteString(p.tail)
	}
	return buf.String()
}

// ─── Per-segment eligibility ──────────────────────────────────────────────────

// pipeSafeConsumers are the programs whose behaviour does not depend on the
// exact shape of what they are fed — they shorten or page a stream and hand it
// on. `grep -rn foo src | head -20` is still correct after the grep is
// rewritten, because head trims lines either way.
//
// Anything absent from this list reads structure out of its input. Piping
// yeet's rendered output into jq, wc, awk, sort, or a shell loop would give a
// confidently wrong answer, which costs far more than the tokens it saves.
var pipeSafeConsumers = map[string]bool{
	"head": true,
	"tail": true,
	"less": true,
	"more": true,
	"cat":  true,
}

// benignVerbs never change anything outside their own stdout. They matter for
// the permission decision, not the rewrite: a chain made only of yeet commands
// and these can keep today's auto-allow, while a chain that also contains
// something unrecognised has to be shown to the user.
var benignVerbs = map[string]bool{
	"echo": true, "printf": true, "pwd": true, "cd": true, "true": true,
	"false": true, "head": true, "tail": true, "wc": true, "sort": true,
	"uniq": true, "cut": true, "tr": true, "jq": true, "date": true,
	"basename": true, "dirname": true, "which": true, "less": true,
	"more": true, "cat": true, "column": true, "nl": true, "rev": true,
	"grep": true, "ls": true, "find": true, "diff": true, "du": true,
	"ps": true, "env": true, "printenv": true, "type": true, "test": true,
}

// stdoutRedirect reports whether a segment sends its stdout somewhere other
// than the caller. `cat a.ts > b.ts` writes a file: rewriting it would fill
// b.ts with yeet's rendering instead of the file's contents, silently
// corrupting it. `2>/dev/null` and `2>&1` only move stderr and are fine.
func stdoutRedirect(cmd string) bool {
	runes := []rune(cmd)
	var quote rune
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '<':
			return true
		case '>':
			// `2>` and `2>>` touch stderr only. Anything else — `>`, `>>`,
			// `1>`, `&>` — takes stdout away from the caller.
			if i > 0 && runes[i-1] == '2' {
				continue
			}
			if i > 1 && runes[i-1] == '>' && runes[i-2] == '2' {
				continue
			}
			return true
		}
	}
	return false
}

// segmentRewritable reports whether the part at index i may be rewritten at
// all, before any rule is consulted.
func segmentRewritable(parts []chainPart, i int) bool {
	p := parts[i]
	if p.cmd == "" {
		return false
	}
	// A stage after a pipe reads stdin. Its arguments say nothing about where
	// its input comes from, so a rewrite cannot preserve its meaning.
	if p.stdinFed() {
		return false
	}
	if stdoutRedirect(p.cmd) {
		return false
	}
	// Producing for a pipeline is only safe if every stage downstream just
	// trims or pages the stream.
	for j := i + 1; j < len(parts) && parts[j].sep == "|"; j++ {
		fields := strings.Fields(parts[j].cmd)
		if len(fields) == 0 || !pipeSafeConsumers[fields[0]] {
			return false
		}
	}
	return true
}
