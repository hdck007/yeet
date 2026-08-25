# Yeet — Token-Optimized CLI Wrapper

**Always use yeet commands. Never use raw Bash equivalents or built-in Read/Glob/Grep/Edit/Write tools.**

## Mandatory replacements

| NEVER use | ALWAYS use instead |
|---|---|
| Read / Glob / Grep / Edit / Write tools | equivalent `yeet` command |
| `cat` / `head` / `tail` | `yeet read <file>` |
| `ls` / `find` / `grep -rn` | `yeet ls` / `yeet find` / `yeet grep` |

## Decision flow — always follow this order

1. **`yeet grep "symbol" path`** — START HERE. Returns match lines with file + line number.
2. **`yeet read <file> --lines N-M`** — when you need more lines around a known line number.
3. **`yeet read <file> -l aggressive`** — full API shape: signatures with original line numbers (91% reduction).
4. **`yeet read <file> -l minimal`** — LAST RESORT only if absolutely necessary.

> **Rule:** grep → `--lines` if you need more → `-l aggressive` for shape.
> **After `yeet edit` or `yeet write`: trust the confirmation — do NOT re-read to verify.**
> Bare `yeet read` on files >150 lines warns and stops — follow the decision flow above.

## git & gh

`git status`, `git diff`, `git log`, and `gh pr view` are among the largest
outputs an agent ever reads — a `git diff` on a real branch runs to hundreds of
kilobytes. Use the yeet forms:

```bash
yeet git status                 # grouped by staged/unstaged/untracked, with counts
yeet git diff [ref]             # per-file +/- summary, largest changes first
yeet git log -20                # one line per commit
yeet git show <ref>             # commit header + per-file +/-
yeet git branch                 # branch names with relative dates
yeet git stash list / show
yeet git worktree list
yeet git push / pull / fetch    # collapsed to "ok <what happened>"

yeet gh pr list                 # number, title, author, review state
yeet gh pr view <n>             # state, branches, file/line counts, URL
yeet gh pr checks <n>           # pass/fail counts; only failures listed
yeet gh issue list / view <n>
yeet gh run list                # id, conclusion, workflow, branch
yeet gh run view <id>           # per-job status; only failing steps listed
yeet gh repo view / list
```

**Read-only subcommands only.** Anything that changes state — `git commit`,
`git push`, `git reset`, `gh pr merge`, `gh pr create` — must be run with the
real binary. The proxy hook already refuses to rewrite those; do not route them
through yeet by hand.

`yeet git diff` reports *where* things changed, not what. When you need the
actual lines, either read the specific files it named, or use
`yeet git diff --content`, which keeps the changed lines with the index/mode/@@
noise stripped and caps the output (40 lines per file, 400 overall).

Add `-u` to any git or gh command for the smallest possible output. It drops
fields that are useful but not essential — the branch a CI run tested, the
branch a stash came from, a repo's default branch. The default keeps them,
because a saving that costs you a follow-up command is not a saving.

`git -C <path>`, `--no-pager`, `-c key=val`, `--git-dir`, and the other git
global options are understood and passed through, so those forms stay compact
too.

## Tests, builds, and package managers

A passing test run prints a line per test; a failing one prints a stack trace per
failure. An install prints a deprecation warning per transitive dependency. In
every case the useful part is a few lines and the rest is volume.

```bash
yeet vitest [args]              # failures only, with the stack trimmed
yeet tsc [args]                 # errors grouped by file, capped per file
yeet lint [args]                # violations grouped by rule
yeet playwright [args]          # failures only
yeet prettier [args]            # only the files that need formatting
yeet next build [args]          # routes and bundle sizes (build only)
yeet prisma [args]              # decoration and ASCII art stripped

yeet npm  <cmd>                 # what changed, plus a count of the warnings
yeet pnpm <cmd>
yeet yarn <cmd>
```

## Processes, disks, clusters, containers

```bash
yeet ps aux                     # totals, CPU/memory leaders, then grouped by program
yeet du -sh *                   # largest first, long tail collapsed
yeet kubectl get pods           # health summary; only the not-ready rows in full
yeet kubectl describe <res>     # status, conditions and events; annotations dropped
yeet kubectl logs <res>         # repeated lines collapsed, both ends kept
yeet docker ps                  # grouped by status; id/command/created columns dropped
yeet docker images
yeet docker logs <c>            # repeated lines collapsed
```

`yeet ps` labels interpreters by what they are running, so a machine with
fourteen `node` processes reports `node:vitest`, `node:server` and so on rather
than fourteen identical rows.

**Read-only subcommands only**, same boundary as git and gh: `kubectl apply`,
`kubectl delete`, `kubectl exec`, `docker run`, `docker rm`, `docker compose up`
and every other mutation must reach the real binary. The proxy hook refuses to
rewrite them; do not route them through yeet by hand.

## Reference

```bash
# Search & explore
yeet grep "pattern" [path]          # grouped matches with file + line numbers
yeet grep "pattern" [path] -C 2         # with 2 context lines (use when match line alone is not enough)
yeet glob "**/*.go" [path]          # files matching pattern, sorted by modification time
yeet find "*.go" [path]             # compact dir-grouped file list
yeet ls [path]                      # directory listing: dirs first, files with sizes
yeet ls -laR [path]                 # recursive listing
yeet tree [path]                    # tree view
yeet smart <file>                   # 2-line summary: type/size/declarations with line numbers

# Read
yeet read <file> --lines N-M        # exact lines (original line numbers preserved)
yeet read <file> --lines N-M -n     # same, with line numbers shown
yeet read <file> -l aggressive      # signatures only — always includes line numbers
yeet read <file> -l minimal         # full content minus comments/blanks (last resort)
yeet read <file> -n                 # full content with line numbers

# Edit & write
yeet edit <file> --old 'old' --new 'new'        # replace first match
yeet edit <file> --old 'old' --new 'new' --all  # replace all
cat <<'WRITE' | yeet write path/to/file         # write/overwrite file
content here
WRITE
```

## Chained commands

The proxy hook splits a chain on `;`, `&&`, `||`, `|` and newlines and rewrites
each command in it, so `cat pkg.json && ls src` becomes
`yeet read pkg.json && yeet ls src` without you doing anything.

Two shapes it deliberately leaves alone, because a rewrite there would change
the answer rather than shorten it:

- **A command that reads the pipe.** `git log | grep fix` stays as it is —
  `yeet grep` searches the working tree, not standard input.
- **A consumer that parses the exact bytes.** `cat data.json | jq .name`,
  `ls | wc -l` — reshaping the producer would make jq fail and wc count the
  wrong thing. Piping into `head`, `tail`, `less`, `more` or `cat` is fine and
  is rewritten.

Writing `yeet` yourself in every segment is still the most reliable thing to do:

> `cd /path && yeet read file.go` ✓   `cd /path && cat file.go` ✓ (rewritten for you)
> `yeet read data.json | jq .name` ✗ — read the file, then work from what it says
