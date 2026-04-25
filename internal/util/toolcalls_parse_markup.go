package util

import (
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strings"
)

var xmlToolCallPattern = regexp.MustCompile(`(?is)<tool_call>\s*(.*?)\s*</tool_call>`)
var functionCallPattern = regexp.MustCompile(`(?is)<function_call>\s*([^<]+?)\s*</function_call>`)
var functionParamPattern = regexp.MustCompile(`(?is)<function\s+parameter\s+name="([^"]+)"\s*>\s*(.*?)\s*</function\s+parameter>`)
var antmlFunctionCallPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?function_call[^>]*(?:name|function)="([^"]+)"[^>]*>\s*(.*?)\s*</(?:[a-z0-9_]+:)?function_call>`)
var antmlArgumentPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?argument\s+name="([^"]+)"\s*>\s*(.*?)\s*</(?:[a-z0-9_]+:)?argument>`)
var antmlParametersPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?parameters\s*>\s*(\{.*?\})\s*</(?:[a-z0-9_]+:)?parameters>`)
var invokeCallPattern = regexp.MustCompile(`(?is)<invoke\s+name="([^"]+)"\s*>(.*?)</invoke>`)
var invokeParamPattern = regexp.MustCompile(`(?is)<parameter\s+name="([^"]+)"\s*>\s*(.*?)\s*</parameter>`)

func parseXMLToolCalls(text string) []ParsedToolCall {
	matches := xmlToolCallPattern.FindAllString(text, -1)
	out := make([]ParsedToolCall, 0, len(matches)+1)
	for _, block := range matches {
		call, ok := parseSingleXMLToolCall(block)
		if !ok {
			continue
		}
		out = append(out, call)
	}
	if len(out) > 0 {
		return out
	}
	if call, ok := parseFunctionCallTagStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if calls := parseAntmlFunctionCallStyles(text); len(calls) > 0 {
		return calls
	}
	if calls := parseInvokeFunctionCallStyles(text); len(calls) > 0 {
		return calls
	}
	return nil
}

func parseSingleXMLToolCall(block string) (ParsedToolCall, bool) {
	inner := strings.TrimSpace(block)
	inner = strings.TrimPrefix(inner, "<tool_call>")
	inner = strings.TrimSuffix(inner, "</tool_call>")
	inner = strings.TrimSpace(inner)
	if strings.HasPrefix(inner, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(inner), &payload); err == nil {
			name := strings.TrimSpace(asString(payload["tool"]))
			if name == "" {
				name = strings.TrimSpace(asString(payload["tool_name"]))
			}
			if name != "" {
				input := map[string]any{}
				if params, ok := payload["params"].(map[string]any); ok {
					input = params
				} else if params, ok := payload["parameters"].(map[string]any); ok {
					input = params
				}
				return ParsedToolCall{Name: name, Input: input}, true
			}
		}
	}

	dec := xml.NewDecoder(strings.NewReader(block))
	name := ""
	params := map[string]any{}
	inParams := false
	inTool := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			tag := strings.ToLower(t.Name.Local)
			switch tag {
			case "tool":
				inTool = true
				for _, attr := range t.Attr {
					if strings.EqualFold(strings.TrimSpace(attr.Name.Local), "name") && strings.TrimSpace(name) == "" {
						name = strings.TrimSpace(attr.Value)
					}
				}
			case "parameters":
				inParams = true
			case "tool_name", "name":
				var v string
				if err := dec.DecodeElement(&v, &t); err == nil && strings.TrimSpace(v) != "" {
					name = strings.TrimSpace(v)
				}
			case "input", "arguments", "argument", "args", "params":
				var v string
				if err := dec.DecodeElement(&v, &t); err == nil && strings.TrimSpace(v) != "" {
					if parsed := parseToolCallInput(strings.TrimSpace(v)); len(parsed) > 0 {
						for k, vv := range parsed {
							params[k] = vv
						}
					}
				}
			default:
				if inParams || inTool {
					if (tag == "parameter" || tag == "argument") && len(t.Attr) > 0 {
						paramName := ""
						for _, attr := range t.Attr {
							if strings.EqualFold(strings.TrimSpace(attr.Name.Local), "name") {
								paramName = strings.TrimSpace(attr.Value)
								break
							}
						}
						if paramName != "" {
							var v string
							if err := dec.DecodeElement(&v, &t); err == nil {
								params[paramName] = strings.TrimSpace(v)
							}
							continue
						}
					}
					var v string
					if err := dec.DecodeElement(&v, &t); err == nil {
						params[t.Name.Local] = strings.TrimSpace(v)
					}
				}
			}
		case xml.EndElement:
			tag := strings.ToLower(t.Name.Local)
			if tag == "parameters" {
				inParams = false
			}
			if tag == "tool" {
				inTool = false
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{Name: strings.TrimSpace(name), Input: params}, true
}

func parseFunctionCallTagStyle(text string) (ParsedToolCall, bool) {
	m := functionCallPattern.FindStringSubmatch(text)
	if len(m) < 2 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return ParsedToolCall{}, false
	}
	input := map[string]any{}
	for _, pm := range functionParamPattern.FindAllStringSubmatch(text, -1) {
		if len(pm) < 3 {
			continue
		}
		key := strings.TrimSpace(pm[1])
		val := strings.TrimSpace(pm[2])
		if key != "" {
			input[key] = val
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseAntmlFunctionCallStyles(text string) []ParsedToolCall {
	matches := antmlFunctionCallPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(matches))
	for _, m := range matches {
		if call, ok := parseSingleAntmlFunctionCallMatch(m); ok {
			out = append(out, call)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseSingleAntmlFunctionCallMatch(m []string) (ParsedToolCall, bool) {
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return ParsedToolCall{}, false
	}
	body := strings.TrimSpace(m[2])
	input := map[string]any{}
	if strings.HasPrefix(body, "{") {
		if err := json.Unmarshal([]byte(body), &input); err == nil {
			return ParsedToolCall{Name: name, Input: input}, true
		}
	}
	if pm := antmlParametersPattern.FindStringSubmatch(body); len(pm) >= 2 {
		if err := json.Unmarshal([]byte(strings.TrimSpace(pm[1])), &input); err == nil {
			return ParsedToolCall{Name: name, Input: input}, true
		}
	}
	for _, am := range antmlArgumentPattern.FindAllStringSubmatch(body, -1) {
		if len(am) < 3 {
			continue
		}
		k := strings.TrimSpace(am[1])
		v := strings.TrimSpace(am[2])
		if k != "" {
			input[k] = v
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseInvokeFunctionCallStyle(text string) (ParsedToolCall, bool) {
	calls := parseInvokeFunctionCallStyles(text)
	if len(calls) == 0 {
		return ParsedToolCall{}, false
	}
	return calls[0], true
}

func parseInvokeFunctionCallStyles(text string) []ParsedToolCall {
	matches := invokeCallPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(matches))
	for _, m := range matches {
		call, ok := parseSingleInvokeFunctionCallMatch(m)
		if !ok {
			continue
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseSingleInvokeFunctionCallMatch(m []string) (ParsedToolCall, bool) {
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return ParsedToolCall{}, false
	}
	input := map[string]any{}
	for _, pm := range invokeParamPattern.FindAllStringSubmatch(m[2], -1) {
		if len(pm) < 3 {
			continue
		}
		k := strings.TrimSpace(pm[1])
		v := strings.TrimSpace(pm[2])
		if k != "" {
			if parsed, ok := parseJSONValue(v); ok {
				input[k] = parsed
			} else {
				input[k] = v
			}
		}
	}
	if len(input) == 0 {
		if argsRaw := findMarkupTagValue(m[2], toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag); argsRaw != "" {
			input = parseMarkupInput(argsRaw)
		} else if kv := parseMarkupKVObject(m[2]); len(kv) > 0 {
			input = kv
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
