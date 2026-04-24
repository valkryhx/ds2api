package util

import (
	"regexp"
	"strings"
)

var textKVNamePattern = regexp.MustCompile(`(?is)function\.name:\s*([a-zA-Z0-9_\-.]+)`)
var callMarkerPattern = regexp.MustCompile(`(?is)\[\s*(?:调用|call)\s+([a-zA-Z0-9_\-.]+)\s*\]`)
var toolUseLabelPattern = regexp.MustCompile(`(?is)^\s*(?:tool\s*use|tool\s*call|调用工具)\s*:\s*(.*)$`)
var directParenCallNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-.]*$`)

func parseTextKVToolCalls(text string) []ParsedToolCall {
	if calls := parseFunctionNameStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	if calls := parseCallMarkerStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	if calls := parseDirectParenStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	return parseToolUseLabelStyleToolCalls(text)
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
		if call, ok := parseSingleDirectParenCall(raw); ok {
			out = append(out, call)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseSingleDirectParenCall(raw string) (ParsedToolCall, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return ParsedToolCall{}, false
	}
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		line = strings.TrimSpace(line[2:])
	}
	openIdx := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if openIdx <= 0 || closeIdx <= openIdx || closeIdx != len(line)-1 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(line[:openIdx])
	if !directParenCallNamePattern.MatchString(name) {
		return ParsedToolCall{}, false
	}
	argsRaw := strings.TrimSpace(line[openIdx+1 : closeIdx])
	if argsRaw == "" {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{
		Name:  name,
		Input: parseDirectParenInput(name, argsRaw),
	}, true
}

func parseToolUseLabelStyleToolCalls(text string) []ParsedToolCall {
	lines := strings.Split(text, "\n")
	out := make([]ParsedToolCall, 0)

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		m := toolUseLabelPattern.FindStringSubmatch(line)
		if len(m) == 0 {
			continue
		}

		remainder := strings.TrimSpace(m[1])
		if call, ok := parseSingleDirectParenCall(remainder); ok {
			out = append(out, call)
			continue
		}

		name := ""
		if remainder != "" {
			fields := strings.Fields(remainder)
			if len(fields) > 0 && directParenCallNamePattern.MatchString(fields[0]) {
				name = fields[0]
			}
		}

		if name == "" {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			if j >= len(lines) {
				continue
			}
			nextLine := strings.TrimSpace(lines[j])
			if call, ok := parseSingleDirectParenCall(nextLine); ok {
				out = append(out, call)
				i = j
				continue
			}
			if !directParenCallNamePattern.MatchString(nextLine) {
				continue
			}
			name = nextLine
			i = j
		}

		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if toolUseLabelPattern.MatchString(strings.TrimSpace(lines[j])) {
				end = j
				break
			}
		}

		segment := strings.TrimSpace(strings.Join(lines[i+1:end], "\n"))
		if segment == "" {
			continue
		}

		if braceIdx := strings.IndexByte(segment, '{'); braceIdx >= 0 {
			if obj, _, ok := extractJSONObject(segment, braceIdx); ok {
				out = append(out, ParsedToolCall{
					Name:  name,
					Input: parseToolCallInput(obj),
				})
				continue
			}
		}

		firstArg := ""
		for _, segLine := range strings.Split(segment, "\n") {
			trimmed := strings.TrimSpace(segLine)
			if trimmed == "" {
				continue
			}
			firstArg = trimmed
			break
		}
		if firstArg == "" {
			continue
		}
		out = append(out, ParsedToolCall{
			Name:  name,
			Input: parseDirectParenInput(name, firstArg),
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
