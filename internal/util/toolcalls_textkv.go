package util

import (
	"regexp"
	"strings"
)

var textKVNamePattern = regexp.MustCompile(`(?is)function\.name:\s*([a-zA-Z0-9_\-.]+)`)
var callMarkerPattern = regexp.MustCompile(`(?is)\[\s*(?:调用|call)\s+([a-zA-Z0-9_\-.]+)\s*\]`)

func parseTextKVToolCalls(text string) []ParsedToolCall {
	if calls := parseFunctionNameStyleToolCalls(text); len(calls) > 0 {
		return calls
	}
	return parseCallMarkerStyleToolCalls(text)
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
