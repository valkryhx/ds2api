package util

import (
	"regexp"
	"strings"
)

var textKVNamePattern = regexp.MustCompile(`(?is)function\.name:\s*([a-zA-Z0-9_\-.]+)`)
var callMarkerPattern = regexp.MustCompile(`(?is)\[\s*(?:调用|call)\s+([a-zA-Z0-9_\-.]+)\s*\]`)
var directParenCallNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-.]*$`)

func parseTextKVToolCalls(text string) []ParsedToolCall {
	if calls := parseFunctionNameStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	if calls := parseCallMarkerStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	return parseDirectParenStyleToolCalls(text)
}

func parseFunctionNameStyleToolCalls(text string) []ParsedToolCall {
	var out []ParsedToolCall
	matches := textKVNamePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}

	for i, match := range matches {
		name := text[match[2]:match[3]]

		offset := match[1]
		endSearch := len(text)
		if i+1 < len(matches) {
			endSearch = matches[i+1][0]
		}

		searchArea := text[offset:endSearch]
		argIdx := strings.Index(searchArea, "function.arguments:")
		if argIdx < 0 {
			continue
		}

		startIdx := offset + argIdx + len("function.arguments:")
		braceIdx := strings.IndexByte(text[startIdx:endSearch], '{')
		if braceIdx < 0 {
			continue
		}

		actualStart := startIdx + braceIdx
		objJson, _, ok := extractJSONObject(text, actualStart)
		if !ok {
			continue
		}

		input := parseToolCallInput(objJson)
		out = append(out, ParsedToolCall{
			Name:  name,
			Input: input,
		})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func parseCallMarkerStyleToolCalls(text string) []ParsedToolCall {
	var out []ParsedToolCall
	matches := callMarkerPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}

	for i, match := range matches {
		name := strings.TrimSpace(text[match[2]:match[3]])
		if name == "" {
			continue
		}

		offset := match[1]
		endSearch := len(text)
		if i+1 < len(matches) {
			endSearch = matches[i+1][0]
		}

		searchArea := text[offset:endSearch]
		braceIdx := strings.IndexByte(searchArea, '{')
		if braceIdx < 0 {
			continue
		}
		objStart := offset + braceIdx
		objJSON, _, ok := extractJSONObject(text, objStart)
		if !ok {
			continue
		}

		out = append(out, ParsedToolCall{
			Name:  name,
			Input: parseToolCallInput(objJSON),
		})
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func parseDirectParenStyleToolCalls(text string) []ParsedToolCall {
	lines := strings.Split(text, "\n")
	out := make([]ParsedToolCall, 0)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = strings.TrimSpace(line[2:])
		}
		openIdx := strings.IndexByte(line, '(')
		closeIdx := strings.LastIndexByte(line, ')')
		if openIdx <= 0 || closeIdx <= openIdx || closeIdx != len(line)-1 {
			continue
		}

		name := strings.TrimSpace(line[:openIdx])
		if !directParenCallNamePattern.MatchString(name) {
			continue
		}
		argsRaw := strings.TrimSpace(line[openIdx+1 : closeIdx])
		if argsRaw == "" {
			continue
		}

		input := parseDirectParenInput(name, argsRaw)
		out = append(out, ParsedToolCall{
			Name:  name,
			Input: input,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseDirectParenInput(name string, argsRaw string) map[string]any {
	trimmed := strings.TrimSpace(argsRaw)
	if trimmed == "" {
		return map[string]any{}
	}

	// Prefer explicit JSON object arguments when present.
	if strings.HasPrefix(trimmed, "{") {
		if parsed := parseToolCallInput(trimmed); len(parsed) > 0 {
			if _, hasRaw := parsed["_raw"]; !hasRaw {
				return parsed
			}
		}
	}

	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "bash", "shell", "shell_command", "execute_command", "powershell", "cmd", "terminal":
		return map[string]any{"command": trimmed}
	case "read", "read_file", "cat":
		return map[string]any{"file_path": trimmed}
	default:
		return map[string]any{"_raw": trimmed}
	}
}
