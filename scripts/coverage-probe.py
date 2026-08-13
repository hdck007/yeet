#!/usr/bin/env python3
"""Report which shell commands yeet intercepts and which pass through.

A command that passes through is a command the agent runs raw, so whatever it
prints lands in context unfiltered. Those are the gaps worth closing next.

  python3 scripts/coverage-probe.py [path-to-yeet]
"""

import subprocess
import sys

YEET = sys.argv[1] if len(sys.argv) > 1 else "yeet"

COMMANDS = [
    # file reading
    "cat foo.go", "head -50 foo.go", "tail -100 app.log", "sed -n '1,50p' f.go",
    # search / listing
    "grep -rn TODO .", "rg TODO", "find . -name '*.go'", "ls -la", "ls -R",
    "tree", "wc -l foo.go", "du -sh *",
    # vcs
    "git status", "git diff", "git log --oneline -20", "git show HEAD",
    "git branch -a", "git stash list",
    # forge
    "gh pr list", "gh pr view 12", "gh run list", "gh issue list",
    # node
    "npm test", "npm install", "npm run build", "yarn build", "pnpm install",
    "tsc --noEmit", "eslint .", "prettier --check .", "vitest run", "jest",
    # other ecosystems
    "go build ./...", "go test ./...", "pytest", "python -m pytest",
    "cargo build", "cargo test", "bundle exec rspec", "make",
    # misc noise producers
    "docker ps", "ps aux", "env", "jq . file.json", "curl https://example.com",
]


def main():
    print(f"{'COMMAND':<32} RESULT")
    print("-" * 92)
    gaps = []
    for c in COMMANDS:
        p = subprocess.run([YEET, "rewrite", c], capture_output=True, text=True)
        out = p.stdout.strip()
        tag = {
            0: out,
            1: "-- passthrough (no coverage)",
            2: "DENY",
            3: "ASK: " + out,
        }.get(p.returncode, f"err{p.returncode}")
        if p.returncode == 1:
            gaps.append(c)
        print(f"{c:<32} {tag}")

    print(f"\n{len(gaps)} of {len(COMMANDS)} have no coverage:")
    for g in gaps:
        print("   ", g)


if __name__ == "__main__":
    main()
