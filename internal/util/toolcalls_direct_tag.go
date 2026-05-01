package util

import (
	"html"
	"strings"
	"unicode"
)

var defaultDirectToolTagCanonicalByAlias = map[string]string{
	"bash":            "Bash",
	"shell":           "shell",
	"shell_command":   "shell_command",
	"execute_command": "execute_command",
	"powershell":      "powershell",
	"cmd":             "cmd",
	"terminal":        "terminal",
	"read":            "Read",
	"read_file":       "read_file",
	"cat":             "cat",
	"glob":            "Glob",
}

func parseDirectToolTagCalls(text string, availableToolNames []string) []ParsedToolCall {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	aliasToCanonical := buildDirectToolTagAliasMap(availableToolNames)
	if len(aliasToCanonical) == 0 {
		return nil
	}

	trimmed := strings.TrimSpace(text)
	pos := 0
	out := make([]ParsedToolCall, 0, 2)
	for {
		for pos < len(trimmed) && unicode.IsSpace(rune(trimmed[pos])) {
			pos++
		}
		if pos >= len(trimmed) {
			break
		}
		if trimmed[pos] != '<' {
			return nil
		}

		localName, bodyStart, ok := parseDirectTagStart(trimmed, pos)
		if !ok {
			return nil
		}
		canonicalName := resolveDirectTagCanonicalName(localName, aliasToCanonical)
		if canonicalName == "" {
			return nil
		}

		closeStart, closeEnd, ok := findDirectTagClose(trimmed, bodyStart, localName)
		if !ok {
			return nil
		}
		body := strings.TrimSpace(trimmed[bodyStart:closeStart])
		body = html.UnescapeString(extractRawTagValue(body))
		if body == "" {
			return nil
		}
		input := parseDirectParenInput(canonicalName, body)
		if len(input) == 0 {
			return nil
		}
		out = append(out, ParsedToolCall{
			Name:  canonicalName,
			Input: input,
		})
		pos = closeEnd
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildDirectToolTagAliasMap(availableToolNames []string) map[string]string {
	aliases := map[string]string{}
	for alias, canonical := range defaultDirectToolTagCanonicalByAlias {
		addDirectTagAlias(aliases, alias, canonical)
		addDirectTagAlias(aliases, strings.ReplaceAll(alias, "-", "_"), canonical)
		addDirectTagAlias(aliases, strings.ReplaceAll(alias, "_", "-"), canonical)
	}
	for _, name := range availableToolNames {
		canonical := strings.TrimSpace(name)
		if canonical == "" {
			continue
		}
		lowerCanonical := strings.ToLower(canonical)
		addDirectTagAlias(aliases, lowerCanonical, canonical)
		addDirectTagAlias(aliases, collapseToolNameNamespace(lowerCanonical), canonical)
		addDirectTagAlias(aliases, strings.ReplaceAll(lowerCanonical, "-", "_"), canonical)
		addDirectTagAlias(aliases, strings.ReplaceAll(lowerCanonical, "_", "-"), canonical)

		switch lowerCanonical {
		case "bash", "shell", "shell_command", "execute_command", "powershell", "cmd", "terminal":
			for _, a := range []string{"bash", "shell", "shell_command", "execute_command", "powershell", "cmd", "terminal"} {
				addDirectTagAlias(aliases, a, canonical)
			}
		case "read", "read_file", "cat":
			for _, a := range []string{"read", "read_file", "cat"} {
				addDirectTagAlias(aliases, a, canonical)
			}
		case "glob":
			addDirectTagAlias(aliases, "glob", canonical)
		}
	}
	return aliases
}

func addDirectTagAlias(dst map[string]string, alias, canonical string) {
	key := strings.ToLower(strings.TrimSpace(alias))
	if key == "" {
		return
	}
	if _, exists := dst[key]; !exists {
		dst[key] = canonical
	}
}

func resolveDirectTagCanonicalName(localName string, aliasToCanonical map[string]string) string {
	key := strings.ToLower(strings.TrimSpace(localName))
	if key == "" {
		return ""
	}
	if canonical, ok := aliasToCanonical[key]; ok {
		return canonical
	}
	key = collapseToolNameNamespace(key)
	if canonical, ok := aliasToCanonical[key]; ok {
		return canonical
	}
	key = strings.ReplaceAll(key, "-", "_")
	if canonical, ok := aliasToCanonical[key]; ok {
		return canonical
	}
	key = strings.ReplaceAll(key, "_", "-")
	if canonical, ok := aliasToCanonical[key]; ok {
		return canonical
	}
	return ""
}

func collapseToolNameNamespace(name string) string {
	if idx := strings.LastIndex(strings.TrimSpace(name), "."); idx >= 0 {
		if idx+1 < len(name) {
			return name[idx+1:]
		}
	}
	return name
}

func parseDirectTagStart(text string, start int) (localName string, bodyStart int, ok bool) {
	if start < 0 || start >= len(text) || text[start] != '<' {
		return "", 0, false
	}
	i := start + 1
	if i < len(text) && text[i] == '/' {
		return "", 0, false
	}
	nameStart := i
	for i < len(text) && isDirectTagNameChar(text[i]) {
		i++
	}
	if i == nameStart {
		return "", 0, false
	}
	raw := strings.ToLower(text[nameStart:i])
	local := raw
	if idx := strings.LastIndex(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		local = raw[idx+1:]
	}
	if local == "" {
		return "", 0, false
	}

	tagEnd := findXMLTagEnd(text, i)
	if tagEnd < 0 {
		return "", 0, false
	}
	if tagEnd > start && text[tagEnd-1] == '/' {
		return "", 0, false
	}
	return local, tagEnd + 1, true
}

func findDirectTagClose(text string, from int, localName string) (closeStart int, closeEnd int, ok bool) {
	lower := strings.ToLower(text)
	target := strings.ToLower(strings.TrimSpace(localName))
	for i := maxInt(from, 0); i < len(text); {
		idx := strings.Index(lower[i:], "</")
		if idx < 0 {
			return -1, -1, false
		}
		start := i + idx
		j := start + 2
		for j < len(text) && unicode.IsSpace(rune(text[j])) {
			j++
		}
		nameStart := j
		for j < len(text) && isDirectTagNameChar(text[j]) {
			j++
		}
		if j == nameStart {
			i = start + 2
			continue
		}
		raw := strings.ToLower(text[nameStart:j])
		local := raw
		if p := strings.LastIndex(raw, ":"); p >= 0 && p+1 < len(raw) {
			local = raw[p+1:]
		}
		if local != target || !hasXMLTagBoundary(text, j) {
			i = j
			continue
		}
		end := findXMLTagEnd(text, j)
		if end < 0 {
			return -1, -1, false
		}
		return start, end + 1, true
	}
	return -1, -1, false
}

func isDirectTagNameChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' ||
		ch == '-' ||
		ch == ':' ||
		ch == '.'
}

func extractTrailingStandaloneDirectTagCandidate(text string, availableToolNames []string) (payload string, prefix string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", false
	}
	if len(buildDirectToolTagAliasMap(availableToolNames)) == 0 {
		return "", "", false
	}

	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '<' {
			continue
		}
		candidate := trimmed[i:]
		if calls := parseDirectToolTagCalls(candidate, availableToolNames); len(calls) == 0 {
			continue
		}
		// Re-check with prebuilt map: parse function may evolve, keep this extraction strict.
		if _, _, startOk := parseDirectTagStart(candidate, 0); !startOk {
			continue
		}
		return strings.TrimSpace(candidate), strings.TrimSpace(trimmed[:i]), true
	}
	return "", "", false
}
