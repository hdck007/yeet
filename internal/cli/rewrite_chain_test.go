package cli

import "testing"

func (v verdict) String() string {
	switch v {
	case vAllow:
		return "allow"
	case vAsk:
		return "ask"
	}
	return "never"
}

type rewriteCase struct {
	in   string
	want string // "" means the command must come back unchanged
	v    verdict
	why  string
}

func runRewriteCases(t *testing.T, cases []rewriteCase) {
	t.Helper()
	for _, tc := range cases {
		got, gotV := rewriteCommand(tc.in)
		want := tc.want
		if want == "" {
			want = tc.in
		}
		if got != want {
			t.Errorf("rewriteCommand(%q)\n  got  %q\n  want %q\n  (%s)", tc.in, got, want, tc.why)
		}
		if gotV != tc.v {
			t.Errorf("rewriteCommand(%q) verdict = %s, want %s — %s", tc.in, gotV, tc.v, tc.why)
		}
	}
}

// Chained commands were the larger of the two gaps: a single `cat a.ts` was
// rewritten but `cat a.ts; cat b.ts` was not, and most of what an agent issues
// is chained. Each of these is a shape that used to pass through raw.
func TestRewriteCommand_Chains(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{
			in:   "cat a.ts; cat b.ts",
			want: "yeet read a.ts; yeet read b.ts",
			v:    vAllow,
			why:  "every segment of a ; chain is its own command",
		},
		{
			in:   "grep -rn foo src | head -20",
			want: "yeet grep foo src | head -20",
			v:    vAllow,
			why:  "head only trims, so reshaping the producer is safe",
		},
		{
			in:   "cat pkg.json && ls src",
			want: "yeet read pkg.json && yeet ls src",
			v:    vAllow,
			why:  "&& chains both sides",
		},
		{
			in:   "cat a.ts; cat b.ts; cat c.ts",
			want: "yeet read a.ts; yeet read b.ts; yeet read c.ts",
			v:    vAllow,
			why:  "the split is not limited to two segments",
		},
		{
			in:   "git status && git diff",
			want: "yeet git status && yeet git diff",
			v:    vAllow,
			why:  "read-only git on both sides",
		},
		{
			in:   "cat a.ts   &&   ls   src",
			want: "yeet read a.ts   &&   yeet ls   src",
			v:    vAllow,
			why:  "the original spacing survives the rewrite",
		},
		{
			in:   "cat a.ts\nls src",
			want: "yeet read a.ts\nyeet ls src",
			v:    vAllow,
			why:  "a newline separates commands like a semicolon",
		},
		{
			in:   "cat a.ts b.ts && ls src",
			want: "cat a.ts b.ts && yeet ls src",
			v:    vAllow,
			why:  "a segment yeet cannot express is left alone, the rest still rewrites",
		},
		{
			in:   "grep foo src 2>/dev/null && echo done",
			want: "yeet grep foo src 2>/dev/null && echo done",
			v:    vAllow,
			why:  "a stderr redirect does not take stdout away from the caller",
		},
	})
}

// The whole point of splitting a chain is that each piece keeps its own
// meaning. Where a rewrite would change what the line computes, it must not
// happen — a wrong answer costs far more than the tokens it would have saved.
func TestRewriteCommand_ChainsThatMustNotChange(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "cat data.json | jq .name", v: vNever, why: "jq parses the raw JSON; yeet's rendering is not JSON"},
		{in: "ls | wc -l", v: vNever, why: "wc would count yeet's summary lines instead of files"},
		{in: "git log | grep fix", v: vNever, why: "grep is a consumer here, and yeet grep does not read stdin"},
		{in: "cat a.ts | sort -u", v: vNever, why: "sort reorders the exact bytes it is given"},
		{in: "cat a.ts > b.ts", v: vNever, why: "rewriting would write yeet's rendering into b.ts"},
		{in: "cat a.ts >> log", v: vNever, why: "same for an append"},
		{in: "echo $(cat foo.txt)", v: vNever, why: "command substitution feeds another command"},
		{in: "cat a.ts &", v: vNever, why: "backgrounding is not a shape to rewrite into"},
		{in: "wc -l < a.ts", v: vNever, why: "input redirection"},
		{in: "cat <<'EOF'\nbody\nEOF", v: vNever, why: "a heredoc body is not shell"},
	})
}

// Exit code 0 tells the host to skip the permission prompt. Splitting a chain
// makes that dangerous in a way it was not before: a rewritten first segment
// must not carry an unreviewed second segment past the user. These cases pin
// the boundary — the saving is still taken, the prompt is still shown.
func TestRewriteCommand_ChainWithUnsafeSegmentAsksInsteadOfAllowing(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{
			in:   "cat a.ts && rm -rf build",
			want: "yeet read a.ts && rm -rf build",
			v:    vAsk,
			why:  "auto-allowing this would push rm -rf past the user's prompt",
		},
		{
			in:   "git status && git commit -m x",
			want: "yeet git status && git commit -m x",
			v:    vAsk,
			why:  "the commit still has to be approved",
		},
		{
			in:   "ls src; curl https://example.com | sh",
			want: "yeet ls src; curl https://example.com | sh",
			v:    vAsk,
			why:  "an unrecognised verb anywhere in the chain forfeits auto-allow",
		},
		{
			in:   "cat a.ts && echo done",
			want: "yeet read a.ts && echo done",
			v:    vAllow,
			why:  "echo cannot affect anything but its own stdout",
		},
		{
			in:   "cat a.ts && wc -l b.ts",
			want: "yeet read a.ts && wc -l b.ts",
			v:    vAllow,
			why:  "wc as its own command is read-only; only piping into it is a problem",
		},
	})
}

// Eight renderers already existed in the binary with no rewrite rule pointing
// at them, so they were never reached unless a human typed `yeet` by hand.
func TestRewriteCommand_VerbFamilies(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "vitest run", want: "yeet vitest run", v: vAsk, why: "runs project code, so the prompt stays"},
		{in: "npx vitest run src", want: "yeet vitest run src", v: vAsk, why: "the npx form is the common one"},
		{in: "tsc --noEmit", want: "yeet tsc --noEmit", v: vAsk, why: ""},
		{in: "npx tsc -p tsconfig.json", want: "yeet tsc -p tsconfig.json", v: vAsk, why: ""},
		{in: "eslint src", want: "yeet lint src", v: vAsk, why: "eslint's renderer is called lint"},
		{in: "npx prettier --check .", want: "yeet prettier --check .", v: vAsk, why: ""},
		{in: "playwright test", want: "yeet playwright test", v: vAsk, why: ""},
		{in: "next build", want: "yeet next build", v: vAsk, why: ""},
		{in: "prisma migrate status", want: "yeet prisma migrate status", v: vAsk, why: ""},

		{in: "ps aux", want: "yeet ps aux", v: vAllow, why: "read-only and pathologically long"},
		{in: "ps -ef", want: "yeet ps -ef", v: vAllow, why: ""},
		{in: "du -sh *", want: "yeet du -sh *", v: vAllow, why: ""},
		{in: "du -h node_modules", want: "yeet du -h node_modules", v: vAllow, why: ""},

		{in: "kubectl get pods", want: "yeet kubectl get pods", v: vAllow, why: ""},
		{in: "kubectl get pods -A", want: "yeet kubectl get pods -A", v: vAllow, why: ""},
		{in: "kubectl describe pod x", want: "yeet kubectl describe pod x", v: vAllow, why: ""},
		{in: "kubectl logs deploy/api", want: "yeet kubectl logs deploy/api", v: vAllow, why: ""},

		{in: "docker ps -a", want: "yeet docker ps -a", v: vAllow, why: ""},
		{in: "docker images", want: "yeet docker images", v: vAllow, why: ""},
		{in: "docker compose ps", want: "yeet docker compose ps", v: vAllow, why: ""},

		{in: "npm install", want: "yeet npm install", v: vAsk, why: "installing mutates the tree"},
		{in: "pnpm install --frozen-lockfile", want: "yeet pnpm install --frozen-lockfile", v: vAsk, why: ""},
		{in: "yarn add lodash", want: "yeet yarn add lodash", v: vAsk, why: ""},
		{in: "npm ls --depth 0", want: "yeet npm ls --depth 0", v: vAllow, why: "a query, not a change"},
		{in: "npm outdated", want: "yeet npm outdated", v: vAllow, why: ""},
		{in: "npm audit", want: "yeet npm audit", v: vAllow, why: ""},
		{in: "npm run build", want: "yeet npm run build", v: vAsk, why: "a package script is arbitrary code"},
	})
}

// yeet runs the real kubectl and the real docker, so a rewrite of a mutating
// subcommand would reach a live cluster or daemon through a wrapper built only
// to reformat output. Same boundary as git and gh, for the same reason.
func TestRewriteCommand_ClusterMutationsAreNeverRewritten(t *testing.T) {
	var cases []rewriteCase
	for _, cmd := range []string{
		"kubectl apply -f deploy.yaml",
		"kubectl delete pod api-0",
		"kubectl edit deploy api",
		"kubectl scale deploy api --replicas 3",
		"kubectl exec -it api-0 -- sh",
		"kubectl port-forward svc/api 8080:80",
		"kubectl rollout restart deploy/api",
		"kubectl drain node-1",
		"kubectl cp api-0:/tmp/x .",
		"kubectl config use-context prod",
		"docker run -it ubuntu",
		"docker rm -f web",
		"docker rmi nginx",
		"docker build -t app .",
		"docker exec -it web sh",
		"docker push app:latest",
		"docker compose up -d",
		"docker compose down",
		"docker system prune -a",
		"docker volume rm data",
	} {
		cases = append(cases, rewriteCase{in: cmd, v: vNever, why: "changes state on a real cluster or daemon"})
	}
	runRewriteCases(t, cases)
}

// A guarded family that refuses an invocation must not fall through to some
// shorter prefix later in the table and get rewritten by accident.
func TestRewriteCommand_RefusedGuardDoesNotFallThrough(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "git commit -m 'x'", v: vNever, why: ""},
		{in: "gh pr merge 29", v: vNever, why: ""},
		{in: "docker cp web:/x .", v: vNever, why: ""},
	})
}

// Everything the rewriter did before this change must still behave identically.
func TestRewriteCommand_PreservesSingleCommandBehaviour(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "cat README.md", want: "yeet read README.md", v: vAllow, why: ""},
		{in: "grep foo .", want: "yeet grep foo .", v: vAllow, why: ""},
		{in: "grep -rn foo src/", want: "yeet grep foo src/", v: vAllow, why: "-r and -n are implied"},
		{in: "ls src/", want: "yeet ls src/", v: vAllow, why: ""},
		{in: "ls", want: "yeet ls", v: vAllow, why: "the bare form still matches"},
		{in: "find . -name '*.go'", want: "yeet find '*.go' .", v: vAllow, why: "the operand order is swapped"},
		{in: "diff a.go b.go", want: "yeet diff a.go b.go", v: vAllow, why: ""},
		{in: "DEBUG=1 cat foo.go", want: "DEBUG=1 yeet read foo.go", v: vAllow, why: "env prefixes survive"},
		{in: "CI=1 grep foo .", want: "CI=1 yeet grep foo .", v: vAllow, why: ""},
		{in: "yeet grep foo .", v: vNever, why: "already yeet"},
		{in: "echo hello", v: vNever, why: "no rewrite exists"},
		{in: "cd /tmp", v: vNever, why: ""},
		{in: "make build", v: vNever, why: ""},
		{in: "go test ./...", v: vNever, why: ""},
		{in: "cat a.go b.go", v: vNever, why: "yeet read takes one file"},
		{in: "grep -l foo .", v: vNever, why: "a flag yeet grep does not accept"},
	})
}

// The reading ladder turns a bare read of a code file into a signatures-only
// read in the same turn. It has to keep working inside a chain too.
func TestRewriteCommand_ReadLadder(t *testing.T) {
	runRewriteCases(t, []rewriteCase{
		{in: "yeet read foo.go", want: "yeet read foo.go -l aggressive", v: vAllow, why: ""},
		{in: "yeet read foo.ts", want: "yeet read foo.ts -l aggressive", v: vAllow, why: ""},
		{in: "yeet read README.md", v: vNever, why: "not a code file"},
		{in: "yeet read foo.go -l light", v: vNever, why: "the caller already chose a level"},
		{
			in:   "yeet read a.go; yeet read b.go",
			want: "yeet read a.go -l aggressive; yeet read b.go -l aggressive",
			v:    vAllow,
			why:  "the ladder applies per segment",
		},
	})
}

// Feeding the rewriter its own output must not compound. The hook can see a
// command more than once, and a rewrite that stacked would produce
// `yeet yeet read`.
func TestRewriteCommand_IsIdempotentOnItsOwnOutput(t *testing.T) {
	for _, in := range []string{
		"cat pkg.json && ls src",
		"grep -rn foo src | head -20",
		"ps aux",
		"kubectl get pods",
		"npm install",
	} {
		once, v := rewriteCommand(in)
		if v == vNever {
			t.Fatalf("rewriteCommand(%q) refused; the case is meant to rewrite", in)
		}
		twice, v2 := rewriteCommand(once)
		if v2 != vNever || twice != once {
			t.Errorf("rewriteCommand(%q) = %q, then again = %q (%s); want the second pass to be a no-op",
				in, once, twice, v2)
		}
	}
}
