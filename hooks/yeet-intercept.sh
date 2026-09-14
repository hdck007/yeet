#!/usr/bin/env bash
# yeet-intercept.sh — PreToolUse hook for Claude Code's Grep and Glob tools.
#
# This does NOT block the tool. It answers the call with yeet's condensed output
# and hands it back in the same turn: a PreToolUse "deny" puts its reason in
# front of the model, so the model receives data rather than a refusal and does
# not need a second turn.
#
# Why not just block, as yeet used to? A blocked tool costs a turn — the model
# must reformulate the call as `yeet grep ...` in Bash. In a 12-session A/B a
# turn cost ~32,000 billed input tokens (every turn re-sends the accumulated
# context), while condensing every tool result in a whole run saved ~1,900. The
# blocking configuration measured 58% WORSE than using no yeet at all.
#
# The rule this file follows: only intercept when yeet can serve the request
# faithfully. Anything else exits 0 and lets the native tool run. An intercept
# that answers a different question than the one asked is worse than no
# intercept, because the model re-runs the search itself and pays the turn
# anyway — which is exactly what an earlier version of this hook caused.

command -v jq   >/dev/null 2>&1 || exit 0
command -v yeet >/dev/null 2>&1 || exit 0

IN=$(cat)
TOOL=$(printf '%s' "$IN" | jq -r '.tool_name // empty')
PAT=$(printf  '%s' "$IN" | jq -r '.tool_input.pattern // empty')
P=$(printf    '%s' "$IN" | jq -r '.tool_input.path // "."')
MODE=$(printf '%s' "$IN" | jq -r '.tool_input.output_mode // "content"')
GLOB=$(printf '%s' "$IN" | jq -r '.tool_input.glob // empty')
TYPE=$(printf '%s' "$IN" | jq -r '.tool_input.type // empty')
HEAD=$(printf '%s' "$IN" | jq -r '.tool_input.head_limit // empty')
MULTI=$(printf '%s' "$IN" | jq -r '.tool_input["-A"] // .tool_input["-B"] // empty')

[ -n "$PAT" ] || exit 0

case "$TOOL" in
  Grep)
    # `yeet grep` only produces content. files_with_matches and count ask a
    # different question and it has no flag for either, so they must reach the
    # real tool.
    [ "$MODE" = "content" ] || exit 0
    # No faithful equivalent for glob/type filtering or -A/-B windows.
    [ -n "$GLOB" ]  && exit 0
    [ -n "$TYPE" ]  && exit 0
    [ -n "$MULTI" ] && exit 0
    OUT=$(yeet grep "$PAT" "$P" 2>&1) || exit 0
    [ -n "$HEAD" ] && OUT=$(printf '%s' "$OUT" | head -n "$HEAD")
    ;;
  Glob)
    [ -n "$HEAD" ] && exit 0
    OUT=$(yeet glob "$PAT" "$P" 2>&1) || exit 0
    ;;
  *) exit 0 ;;
esac

# Empty output is not an answer the model can act on — let the native tool try.
[ -n "$(printf '%s' "$OUT" | tr -d '[:space:]')" ] || exit 0

# Cap what a single intercept can inject, so a pathological match set cannot
# dwarf the context it was meant to protect.
OUT=$(printf '%s' "$OUT" | head -c 6000)

jq -n --arg r "$OUT" '{
  hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: "deny",
    permissionDecisionReason: $r
  }}'
