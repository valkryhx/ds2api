package util

import (
	"regexp"
	"strings"
)

var toolCallPattern = regexp.MustCompile(`\{\s*["']tool_calls["']\s*:\s*\[(.*?)\]\s*\}`)
var fencedJSONPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
var standaloneFencedJSONPattern = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*(.*?)\\s*```\\s*$")
var fencedBlockPattern = regexp.MustCompile("(?s)```.*?```")
var exampleContextPattern = regexp.MustCompile(`(?is)(示例|example|例如|比如|for example|请勿执行|不要执行|仅示例|仅供参考|do not execute|don't execute)`)

func buildToolCallCandidates(text string) []string {
	trimmed := strings.TrimSpace(text)
	candidates := []string{trimmed}

	// fenced code block candidates: ```json ... ```
	for _, match := range fencedJSONPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}

	// best-effort extraction around tool call keywords in mixed text payloads.
	candidates = append(candidates, extractToolCallObjects(trimmed)...)

	// best-effort object slice: from first '{' to last '}'
	first := strings.Index(trimmed, "{")
	last := strings.LastIndex(trimmed, "}")
	if first >= 0 && last > first {
		candidates = append(candidates, strings.TrimSpace(trimmed[first:last+1]))
	}

	// legacy regex extraction fallback
	if m := toolCallPattern.FindStringSubmatch(trimmed); len(m) >= 2 {
		candidates = append(candidates, "{"+`"tool_calls":[`+m[1]+"]}")
	}

	uniq := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		uniq = append(uniq, c)
	}
	return uniq
}

func extractToolCallObjects(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	out := []string{}
	offset := 0
	keywords := []string{"tool_calls", "function.name:", "[tool_call_history]"}
	for {
		bestIdx := -1
		matchedKeyword := ""
		for _, kw := range keywords {
			idx := strings.Index(lower[offset:], kw)
			if idx >= 0 {
				absIdx := offset + idx
				if bestIdx < 0 || absIdx < bestIdx {
					bestIdx = absIdx
					matchedKeyword = kw
				}
			}
		}

		if bestIdx < 0 {
			break
		}

		idx := bestIdx
		// Avoid backtracking too far to prevent OOM on malicious or very long strings
		searchLimit := idx - 2000
		if searchLimit < offset {
			searchLimit = offset
		}

		start := strings.LastIndex(text[searchLimit:idx], "{")
		if start >= 0 {
			start += searchLimit
		}

		if start < 0 {
			offset = idx + len(matchedKeyword)
			continue
		}

		foundObj := false
		for start >= searchLimit {
			candidate, end, ok := extractJSONObject(text, start)
			if ok {
				// Move forward to avoid repeatedly matching the same object.
				offset = end
				out = append(out, strings.TrimSpace(candidate))
				foundObj = true
				break
			}
			// Try previous '{'
			if start > searchLimit {
				prevStart := strings.LastIndex(text[searchLimit:start], "{")
				if prevStart >= 0 {
					start = searchLimit + prevStart
					continue
				}
			}
			break
		}

		if !foundObj {
			offset = idx + len(matchedKeyword)
		}
	}
	return out
}

func extractJSONObject(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return "", 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	// Limit scan length to avoid OOM on unclosed objects
	maxLen := start + 50000
	if maxLen > len(text) {
		maxLen = len(text)
	}
	for i := start; i < maxLen; i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return text[start : i+1], i + 1, true
			}
		}
	}
	return "", 0, false
}

func looksLikeToolExampleContext(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	return strings.Contains(t, "```")
}

func looksLikeToolExamplePrefix(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return exampleContextPattern.MatchString(trimmed)
}

func extractStandaloneFencedPayload(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	m := standaloneFencedJSONPattern.FindStringSubmatch(trimmed)
	if len(m) < 2 {
		return "", false
	}
	payload := strings.TrimSpace(m[1])
	if payload == "" {
		return "", false
	}
	return payload, true
}

// stripThinkingBlocks removes common thinking/reasoning blocks from model output.
// It handles:
//   - <thinking>...</thinking> (XML-style)
//   - ```think ... ``` or ```thinking ... ``` (code fences)
//   - [THINK]...[/THINK] (markdown-style)
func stripThinkingBlocks(s string) string {
	if s == "" {
		return s
	}

	// Pattern 1: <thinking>...</thinking> (case-insensitive, non-greedy, allow attributes)
	thinkingTagRegex := regexp.MustCompile(`(?is)<thinking\b[^>]*>.*?</thinking>`)
	s = thinkingTagRegex.ReplaceAllString(s, "")

	// Pattern 2: ```think ... ``` or ```thinking ... ``` code blocks
	// Use escaped backticks: \x60\x60\x60 is ` ` `
	thinkFenceRegex := regexp.MustCompile(`(?s)\x60\x60\x60think\s*\n.*?\n\x60\x60\x60`)
	s = thinkFenceRegex.ReplaceAllString(s, "")
	thinkingFenceRegex := regexp.MustCompile(`(?s)\x60\x60\x60thinking\s*\n.*?\n\x60\x60\x60`)
	s = thinkingFenceRegex.ReplaceAllString(s, "")

	// Pattern 3: [THINK]...[/THINK] (case-insensitive, non-greedy)
	thinkMarkdownRegex := regexp.MustCompile(`(?is)\[THINK\].*?\[/THINK\]`)
	s = thinkMarkdownRegex.ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}

func extractTrailingStandaloneJSONObjectCandidate(text string) (payload string, prefix string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", false
	}

	// First, strip thinking blocks to clean the input
	cleaned := stripThinkingBlocks(trimmed)
	if cleaned == "" {
		cleaned = trimmed // fallback in case everything was stripped
	}

	offset := 0
	for {
		rel := strings.Index(cleaned[offset:], "{")
		if rel < 0 {
			return "", "", false
		}
		start := offset + rel
		obj, end, parsed := extractJSONObject(cleaned, start)
		if parsed {
			if strings.TrimSpace(cleaned[end:]) == "" {
				return strings.TrimSpace(obj), strings.TrimSpace(cleaned[:start]), true
			}
		}
		offset = start + 1
		if offset >= len(cleaned) {
			return "", "", false
		}
	}
}

func stripFencedCodeBlocks(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return fencedBlockPattern.ReplaceAllString(text, " ")
}
