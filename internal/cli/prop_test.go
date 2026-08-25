package cli

import (
	"strings"
	"testing"
)

// Every rewrite is a partial edit of a split command line, so two properties
// have to hold for arbitrary input, not just the cases someone thought to
// write down:
//
//  1. splitting and rejoining must be byte-identical, or a chain with one
//     rewritten segment corrupts the segments around it;
//  2. the operators and whitespace between segments must survive a rewrite,
//     because `&&` silently becoming `;` changes when the second command runs.
//
// The generator below walks a grammar of shapes rather than random bytes: the
// interesting inputs here are structural (a quote containing a separator, a
// redirect next to a pipe), and random bytes almost never produce those.
func generateCommands() []string {
	verbs := []string{
		"cat a.ts", "ls src", "grep -rn foo src", "git status", "git commit -m x",
		"ps aux", "kubectl get pods", "docker ps", "npm install", "vitest run",
		"echo hi", "rm -rf build", "make", "cat a.ts b.ts", "head -20",
		"jq .name", "wc -l", "cat 'a;b.txt'", `grep "x|y" src`,
		"grep foo src 2>/dev/null", "cat a.ts > out", "tsc --noEmit",
	}
	seps := []string{"; ", " && ", " || ", " | ", "\n", ";", "  &&  "}

	var out []string
	out = append(out, verbs...)
	// Pairs and triples cover every operator against every verb shape.
	for i, a := range verbs {
		for j, s := range seps {
			b := verbs[(i+j+1)%len(verbs)]
			out = append(out, a+s+b)
			c := verbs[(i+2*j+3)%len(verbs)]
			out = append(out, a+s+b+seps[(j+1)%len(seps)]+c)
		}
	}
	// Leading and trailing whitespace, which the join has to preserve exactly.
	for _, c := range []string{"cat a.ts", "cat a.ts && ls", "ls | head"} {
		out = append(out, "  "+c, c+"  ", "  "+c+"  ", "\n"+c, c+"\n")
	}
	return out
}

func TestProperty_SplitJoinIsLossless(t *testing.T) {
	cmds := generateCommands()
	checked := 0
	for _, in := range cmds {
		parts, ok := splitChain(in)
		if !ok {
			continue
		}
		checked++
		if got := joinChain(parts); got != in {
			t.Errorf("join(split(%q)) = %q — not byte-identical", in, got)
		}
	}
	if checked < 100 {
		t.Fatalf("only %d inputs were actually split; the generator is not exercising the property", checked)
	}
	t.Logf("round-tripped %d generated command lines", checked)
}

// A rewrite may replace a segment's command, and nothing else. The operators
// between segments, their count, and the whitespace around every segment all
// have to come through untouched.
func TestProperty_RewritePreservesChainStructure(t *testing.T) {
	rewritten := 0
	for _, in := range generateCommands() {
		out, v := rewriteCommand(in)
		if v == vNever {
			if out != in {
				t.Errorf("rewriteCommand(%q) returned %q for a vNever verdict; want the input back", in, out)
			}
			continue
		}
		rewritten++

		inParts, ok1 := splitChain(in)
		outParts, ok2 := splitChain(out)
		if !ok1 || !ok2 {
			t.Errorf("rewriteCommand(%q) = %q, which no longer splits", in, out)
			continue
		}
		if len(inParts) != len(outParts) {
			t.Errorf("rewriteCommand(%q) = %q changed the segment count %d -> %d",
				in, out, len(inParts), len(outParts))
			continue
		}
		for i := range inParts {
			if inParts[i].sep != outParts[i].sep {
				t.Errorf("rewriteCommand(%q) = %q changed separator %d from %q to %q",
					in, out, i, inParts[i].sep, outParts[i].sep)
			}
			if inParts[i].lead != outParts[i].lead || inParts[i].tail != outParts[i].tail {
				t.Errorf("rewriteCommand(%q) = %q changed the whitespace around segment %d", in, out, i)
			}
			// A segment is either untouched or a yeet command. It is never
			// some third thing.
			a, b := inParts[i].cmd, outParts[i].cmd
			if a != b && !strings.HasPrefix(b, "yeet ") && !strings.Contains(b, " yeet ") {
				t.Errorf("rewriteCommand(%q) = %q turned segment %d from %q into %q, "+
					"which is neither the original nor a yeet command", in, out, i, a, b)
			}
		}
	}
	if rewritten < 50 {
		t.Fatalf("only %d generated inputs were rewritten; the property is not being exercised", rewritten)
	}
	t.Logf("structure preserved across %d rewrites", rewritten)
}

// Auto-allow tells the host to skip the permission prompt. A chain may only get
// it when every segment is either a yeet command or something that cannot
// affect anything but its own stdout. This walks the generated corpus and
// checks the verdict against that rule directly, rather than trusting the
// hand-written cases to have covered every combination.
func TestProperty_AutoAllowOnlyWhenEverySegmentIsSafe(t *testing.T) {
	for _, in := range generateCommands() {
		out, v := rewriteCommand(in)
		if v != vAllow {
			continue
		}
		parts, ok := splitChain(out)
		if !ok {
			t.Errorf("auto-allowed %q but the result does not split", out)
			continue
		}
		for _, p := range parts {
			if p.cmd == "" {
				continue
			}
			stripped, _ := stripEnvPrefix(p.cmd)
			fields := strings.Fields(stripped)
			if len(fields) == 0 {
				continue
			}
			if fields[0] == "yeet" || benignVerbs[fields[0]] {
				continue
			}
			t.Errorf("rewriteCommand(%q) = %q was auto-allowed, but segment %q is "+
				"neither a yeet command nor a verb that only writes to stdout",
				in, out, p.cmd)
		}
	}
}
