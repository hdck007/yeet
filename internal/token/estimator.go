package token

// EstimateTokens approximates the number of LLM tokens for a given character count.
// Uses the widely-accepted heuristic of ~4 characters per token for English text and code.
func EstimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}
