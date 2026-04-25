package util

import (
	"encoding/json"
	"regexp"
	"strings"
)

var toolCallMarkupTagNames = []string{"tool_call", "tool_c", "tool_calls", "function_call", "invoke", "function"}
var toolCallMarkupTagPatternByName = map[string]*regexp.Regexp{
	"tool_call":     regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_call\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?tool_call>`),
	"tool_c":        regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_c\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?tool_c>`),
	"tool_calls":    regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_calls\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?tool_calls>`),
	"function_call": regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?function_call\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?function_call>`),
	"invoke":        regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?invoke\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?invoke>`),
	"function":      regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?function\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?function>`),
}
var toolCallMarkupSelfClosingPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?invoke\b([^>]*)/>`)
var toolCallMarkupKVPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?([a-z0-9_\-.]+)\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?([a-z0-9_\-.]+)>`)
var toolCallMarkupAttrPattern = regexp.MustCompile(`(?is)(name|function|tool)\s*=\s*"([^"]+)"`)
var toolCallMarkupNamedArgPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:parameter|argument)\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?(?:parameter|argument)>`)
var toolCallMarkupNamedArgAttrPattern = regexp.MustCompile(`(?is)\bname\s*=\s*"([^"]+)"`)
var anyTagPattern = regexp.MustCompile(`(?is)<[^>]+>`)
var toolCallMarkupNameTagNames = []string{"name", "function"}
var toolCallMarkupNamePatternByTag = map[string]*regexp.Regexp{
	"name":     regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?name\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?name>`),
	"function": regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?function\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?function>`),
}
var toolCallMarkupArgsTagNames = []string{"input", "arguments", "argument", "parameters", "parameter", "args", "params"}
var toolCallMarkupArgsPatternByTag = map[string]*regexp.Regexp{
	"input":      regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?input\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?input>`),
	"arguments":  regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?arguments\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?arguments>`),
	"argument":   regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?argument\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?argument>`),
	"parameters": regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?parameters\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?parameters>`),
	"parameter":  regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?parameter\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?parameter>`),
	"args":       regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?args\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?args>`),
	"params":     regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?params\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?params>`),
}

func parseMarkupToolCalls(text string) []ParsedToolCall {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	out := make([]ParsedToolCall, 0)
	for _, tagName := range toolCallMarkupTagNames {
		pattern := toolCallMarkupTagPatternByName[tagName]
		for _, m := range pattern.FindAllStringSubmatch(trimmed, -1) {
			if len(m) < 3 {
				continue
			}
			attrs := strings.TrimSpace(m[1])
			inner := strings.TrimSpace(m[2])
			if parsed := parseMarkupSingleToolCall(attrs, inner); parsed.Name != "" {
				out = append(out, parsed)
			}
		}
	}
	for _, m := range toolCallMarkupSelfClosingPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(m) < 2 {
			continue
		}
		if parsed := parseMarkupSingleToolCall(strings.TrimSpace(m[1]), ""); parsed.Name != "" {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseMarkupSingleToolCall(attrs string, inner string) ParsedToolCall {
	if parsed := parseToolCallsPayload(inner); len(parsed) > 0 {
		return parsed[0]
	}

	name := ""
	if m := toolCallMarkupAttrPattern.FindStringSubmatch(attrs); len(m) >= 3 {
		name = strings.TrimSpace(m[2])
	}
	if name == "" {
		name = findMarkupTagValue(inner, toolCallMarkupNameTagNames, toolCallMarkupNamePatternByTag)
	}
	if name == "" {
		return ParsedToolCall{}
	}

	input := map[string]any{}
	if namedArgs := parseMarkupNamedArguments(inner); len(namedArgs) > 0 {
		input = namedArgs
	} else if argsRaw := findMarkupTagValue(inner, toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag); argsRaw != "" {
		input = parseMarkupInput(argsRaw)
	} else if kv := parseMarkupKVObject(inner); len(kv) > 0 {
		input = kv
	}
	return ParsedToolCall{Name: name, Input: input}
}

func parseMarkupInput(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	if namedArgs := parseMarkupNamedArguments(raw); len(namedArgs) > 0 {
		return namedArgs
	}
	if parsed := parseToolCallInput(raw); len(parsed) > 0 {
		return parsed
	}
	if kv := parseMarkupKVObject(raw); len(kv) > 0 {
		return kv
	}
	return map[string]any{"_raw": stripTagText(raw)}
}

func parseMarkupNamedArguments(text string) map[string]any {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil
	}
	out := map[string]any{}
	for _, m := range toolCallMarkupNamedArgPattern.FindAllStringSubmatch(raw, -1) {
		if len(m) < 3 {
			continue
		}
		attrRaw := strings.TrimSpace(m[1])
		valueRaw := strings.TrimSpace(m[2])
		attrMatch := toolCallMarkupNamedArgAttrPattern.FindStringSubmatch(attrRaw)
		if len(attrMatch) < 2 {
			continue
		}
		key := strings.TrimSpace(attrMatch[1])
		if key == "" {
			continue
		}
		if parsed, ok := parseJSONValue(valueRaw); ok {
			out[key] = parsed
			continue
		}
		out[key] = stripTagText(valueRaw)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseJSONValue(raw string) (any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	var out any
	if json.Unmarshal([]byte(raw), &out) == nil {
		return out, true
	}
	repaired := repairInvalidJSONBackslashes(raw)
	if repaired != raw && json.Unmarshal([]byte(repaired), &out) == nil {
		return out, true
	}
	repairedLoose := RepairLooseJSON(raw)
	if repairedLoose != raw && json.Unmarshal([]byte(repairedLoose), &out) == nil {
		return out, true
	}
	return nil, false
}

func parseMarkupKVObject(text string) map[string]any {
	matches := toolCallMarkupKVPattern.FindAllStringSubmatch(strings.TrimSpace(text), -1)
	if len(matches) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		key := strings.TrimSpace(m[1])
		endKey := strings.TrimSpace(m[3])
		if key == "" {
			continue
		}
		if !strings.EqualFold(key, endKey) {
			continue
		}
		value := strings.TrimSpace(stripTagText(m[2]))
		if value == "" {
			continue
		}
		var jsonValue any
		if json.Unmarshal([]byte(value), &jsonValue) == nil {
			out[key] = jsonValue
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stripTagText(text string) string {
	return strings.TrimSpace(anyTagPattern.ReplaceAllString(text, ""))
}

func findMarkupTagValue(text string, tagNames []string, patternByTag map[string]*regexp.Regexp) string {
	for _, tag := range tagNames {
		pattern := patternByTag[tag]
		if pattern == nil {
			continue
		}
		if m := pattern.FindStringSubmatch(text); len(m) >= 2 {
			value := strings.TrimSpace(m[1])
			if value != "" {
				return value
			}
		}
	}
	return ""
}
