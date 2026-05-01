package util

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"regexp"
	"strings"
)

var xmlAttrPattern = regexp.MustCompile(`(?is)\b([a-z0-9_:-]+)\s*=\s*("([^"]*)"|'([^']*)')`)
var xmlToolCallsClosePattern = regexp.MustCompile(`(?is)</tool_calls>`)
var xmlInvokeStartPattern = regexp.MustCompile(`(?is)<invoke\b[^>]*\bname\s*=\s*("([^"]*)"|'([^']*)')`)
var cdataBRSeparatorPattern = regexp.MustCompile(`(?i)<br\s*/?>`)

// Compatibility: preserve legacy parser paths used by existing tests and clients.
var xmlToolCallPattern = regexp.MustCompile(`(?is)<tool_call>\s*(.*?)\s*</tool_call>`)
var functionCallPattern = regexp.MustCompile(`(?is)<function_call>\s*([^<]+?)\s*</function_call>`)
var functionParamPattern = regexp.MustCompile(`(?is)<function\s+parameter\s+name="([^"]+)"\s*>\s*(.*?)\s*</function\s+parameter>`)
var antmlFunctionCallPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?function_call[^>]*(?:name|function)="([^"]+)"[^>]*>\s*(.*?)\s*</(?:[a-z0-9_]+:)?function_call>`)
var antmlArgumentPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?argument\s+name="([^"]+)"\s*>\s*(.*?)\s*</(?:[a-z0-9_]+:)?argument>`)
var antmlParametersPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?parameters\s*>\s*(\{.*?\})\s*</(?:[a-z0-9_]+:)?parameters>`)
var invokeCallPattern = regexp.MustCompile(`(?is)<invoke\s+name="([^"]+)"\s*>(.*?)</invoke>`)
var invokeParamPattern = regexp.MustCompile(`(?is)<parameter\s+name="([^"]+)"\s*>\s*(.*?)\s*</parameter>`)
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
var toolCallMarkupAttrPattern = regexp.MustCompile(`(?is)(name|function|tool)\s*=\s*"([^"]+)"`)
var toolCallMarkupNamedArgPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:parameter|argument)\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?(?:parameter|argument)>`)
var toolCallMarkupNamedArgAttrPattern = regexp.MustCompile(`(?is)\bname\s*=\s*"([^"]+)"`)
var toolCallMarkupNameTagNames = []string{"name", "function"}
var toolCallMarkupNamePatternByTag = map[string]*regexp.Regexp{
	"name":     regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?name\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?name>`),
	"function": regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?function\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?function>`),
}
var anyTagPatternCompat = regexp.MustCompile(`(?is)<[^>]+>`)
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

func parseXMLToolCalls(text string) []ParsedToolCall {
	wrappers := findXMLElementBlocks(text, "tool_calls")
	if len(wrappers) == 0 {
		repaired := repairMissingXMLToolCallsOpeningWrapper(text)
		if repaired != text {
			wrappers = findXMLElementBlocks(repaired, "tool_calls")
		}
	}
	if len(wrappers) > 0 {
		out := make([]ParsedToolCall, 0, len(wrappers))
		for _, wrapper := range wrappers {
			for _, block := range findXMLElementBlocks(wrapper.Body, "invoke") {
				call, ok := parseSingleInvokeElement(block)
				if !ok {
					continue
				}
				out = append(out, call)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	// Compatibility fallback chain.
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

func parseMarkupToolCallsLegacy(text string) []ParsedToolCall {
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
			if parsed := parseMarkupSingleToolCallLegacy(attrs, inner); parsed.Name != "" {
				out = append(out, parsed)
			}
		}
	}
	for _, m := range toolCallMarkupSelfClosingPattern.FindAllStringSubmatch(trimmed, -1) {
		if len(m) < 2 {
			continue
		}
		if parsed := parseMarkupSingleToolCallLegacy(strings.TrimSpace(m[1]), ""); parsed.Name != "" {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseMarkupSingleToolCallLegacy(attrs string, inner string) ParsedToolCall {
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
	if namedArgs := parseMarkupNamedArgumentsLegacy(inner); len(namedArgs) > 0 {
		input = namedArgs
	} else if argsRaw := findMarkupTagValue(inner, toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag); argsRaw != "" {
		input = parseMarkupInput(argsRaw)
	} else if kv := parseMarkupKVObject(inner); len(kv) > 0 {
		input = kv
	}
	return ParsedToolCall{Name: name, Input: input}
}

func parseMarkupNamedArgumentsLegacy(text string) map[string]any {
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
		out[key] = strings.TrimSpace(stripTagTextCompat(valueRaw))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func repairMissingXMLToolCallsOpeningWrapper(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "<tool_calls") {
		return text
	}

	closeMatches := xmlToolCallsClosePattern.FindAllStringIndex(text, -1)
	if len(closeMatches) == 0 {
		return text
	}
	invokeLoc := xmlInvokeStartPattern.FindStringIndex(text)
	if invokeLoc == nil {
		return text
	}
	closeLoc := closeMatches[len(closeMatches)-1]
	if invokeLoc[0] >= closeLoc[0] {
		return text
	}

	return text[:invokeLoc[0]] + "<tool_calls>" + text[invokeLoc[0]:closeLoc[0]] + "</tool_calls>" + text[closeLoc[1]:]
}

func parseSingleInvokeElement(block xmlElementBlock) (ParsedToolCall, bool) {
	attrs := parseXMLTagAttributes(block.Attrs)
	name := strings.TrimSpace(html.UnescapeString(attrs["name"]))
	if name == "" {
		return ParsedToolCall{}, false
	}

	inner := strings.TrimSpace(block.Body)
	if strings.HasPrefix(inner, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(inner), &payload); err == nil {
			input := map[string]any{}
			if params, ok := payload["input"].(map[string]any); ok {
				input = params
			}
			if len(input) == 0 {
				if params, ok := payload["parameters"].(map[string]any); ok {
					input = params
				}
			}
			return ParsedToolCall{Name: name, Input: input}, true
		}
	}

	input := map[string]any{}
	for _, paramMatch := range findXMLElementBlocks(inner, "parameter") {
		paramAttrs := parseXMLTagAttributes(paramMatch.Attrs)
		paramName := strings.TrimSpace(html.UnescapeString(paramAttrs["name"]))
		if paramName == "" {
			continue
		}
		value := parseInvokeParameterValue(paramName, paramMatch.Body)
		appendMarkupValue(input, paramName, value)
	}

	if len(input) == 0 {
		if strings.TrimSpace(inner) != "" {
			return ParsedToolCall{}, false
		}
		return ParsedToolCall{Name: name, Input: map[string]any{}}, true
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

type xmlElementBlock struct {
	Attrs string
	Body  string
	Start int
	End   int
}

func findXMLElementBlocks(text, tag string) []xmlElementBlock {
	if text == "" || tag == "" {
		return nil
	}
	var out []xmlElementBlock
	pos := 0
	for pos < len(text) {
		start, bodyStart, attrs, ok := findXMLStartTagOutsideCDATA(text, tag, pos)
		if !ok {
			break
		}
		closeStart, closeEnd, ok := findMatchingXMLEndTagOutsideCDATA(text, tag, bodyStart)
		if !ok {
			pos = bodyStart
			continue
		}
		out = append(out, xmlElementBlock{
			Attrs: attrs,
			Body:  text[bodyStart:closeStart],
			Start: start,
			End:   closeEnd,
		})
		pos = closeEnd
	}
	return out
}

func findXMLStartTagOutsideCDATA(text, tag string, from int) (start, bodyStart int, attrs string, ok bool) {
	lower := strings.ToLower(text)
	target := "<" + strings.ToLower(tag)
	for i := maxInt(from, 0); i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return -1, -1, "", false
		}
		if advanced {
			i = next
			continue
		}
		if strings.HasPrefix(lower[i:], target) && hasXMLTagBoundary(text, i+len(target)) {
			end := findXMLTagEnd(text, i+len(target))
			if end < 0 {
				return -1, -1, "", false
			}
			return i, end + 1, text[i+len(target) : end], true
		}
		i++
	}
	return -1, -1, "", false
}

func findMatchingXMLEndTagOutsideCDATA(text, tag string, from int) (closeStart, closeEnd int, ok bool) {
	lower := strings.ToLower(text)
	openTarget := "<" + strings.ToLower(tag)
	closeTarget := "</" + strings.ToLower(tag)
	depth := 1
	for i := maxInt(from, 0); i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(lower, i)
		if blocked {
			return -1, -1, false
		}
		if advanced {
			i = next
			continue
		}
		if strings.HasPrefix(lower[i:], closeTarget) && hasXMLTagBoundary(text, i+len(closeTarget)) {
			end := findXMLTagEnd(text, i+len(closeTarget))
			if end < 0 {
				return -1, -1, false
			}
			depth--
			if depth == 0 {
				return i, end + 1, true
			}
			i = end + 1
			continue
		}
		if strings.HasPrefix(lower[i:], openTarget) && hasXMLTagBoundary(text, i+len(openTarget)) {
			end := findXMLTagEnd(text, i+len(openTarget))
			if end < 0 {
				return -1, -1, false
			}
			if !isSelfClosingXMLTag(text[:end]) {
				depth++
			}
			i = end + 1
			continue
		}
		i++
	}
	return -1, -1, false
}

func hasXMLTagBoundary(text string, idx int) bool {
	if idx >= len(text) {
		return true
	}
	switch text[idx] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	default:
		return false
	}
}

func isSelfClosingXMLTag(startTag string) bool {
	return strings.HasSuffix(strings.TrimSpace(startTag), "/")
}

func parseXMLTagAttributes(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, m := range xmlAttrPattern.FindAllStringSubmatch(raw, -1) {
		if len(m) < 5 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		if key == "" {
			continue
		}
		value := m[3]
		if value == "" {
			value = m[4]
		}
		out[key] = value
	}
	return out
}

func parseInvokeParameterValue(paramName, raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if value, ok := extractStandaloneCDATA(trimmed); ok {
		if parsed, ok := parseJSONLiteralValue(value); ok {
			return parsed
		}
		if parsed, ok := parseStructuredCDATAParameterValue(paramName, value); ok {
			return parsed
		}
		return value
	}
	decoded := html.UnescapeString(extractRawTagValue(trimmed))
	if strings.Contains(decoded, "<") && strings.Contains(decoded, ">") {
		if parsedValue, ok := parseXMLFragmentValue(decoded); ok {
			switch v := parsedValue.(type) {
			case map[string]any:
				if len(v) > 0 {
					return v
				}
			case []any:
				return v
			case string:
				text := strings.TrimSpace(v)
				if text == "" {
					return ""
				}
				if parsedText, ok := parseJSONLiteralValue(text); ok {
					return parsedText
				}
				return v
			default:
				return v
			}
		}
		if parsed := parseStructuredToolCallInput(decoded); len(parsed) > 0 {
			if len(parsed) == 1 {
				if rawValue, ok := parsed["_raw"].(string); ok {
					return rawValue
				}
			}
			return parsed
		}
	}
	if parsed, ok := parseJSONLiteralValue(decoded); ok {
		return parsed
	}
	return decoded
}

func parseStructuredCDATAParameterValue(paramName, raw string) (any, bool) {
	if preservesCDATAStringParameter(paramName) {
		return nil, false
	}
	normalized := normalizeCDATAForStructuredParse(raw)
	if !strings.Contains(normalized, "<") || !strings.Contains(normalized, ">") {
		return nil, false
	}
	if !cdataFragmentLooksExplicitlyStructured(normalized) {
		return nil, false
	}
	parsed, ok := parseXMLFragmentValue(normalized)
	if !ok {
		return nil, false
	}
	switch v := parsed.(type) {
	case []any:
		return v, true
	case map[string]any:
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	default:
		return nil, false
	}
}

func normalizeCDATAForStructuredParse(raw string) string {
	if raw == "" {
		return ""
	}
	normalized := cdataBRSeparatorPattern.ReplaceAllString(raw, "\n")
	return html.UnescapeString(strings.TrimSpace(normalized))
}

func cdataFragmentLooksExplicitlyStructured(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}

	dec := xml.NewDecoder(strings.NewReader("<root>" + trimmed + "</root>"))
	tok, err := dec.Token()
	if err != nil {
		return false
	}
	start, ok := tok.(xml.StartElement)
	if !ok || !strings.EqualFold(start.Name.Local, "root") {
		return false
	}

	depth := 0
	directChildren := 0
	firstChildName := ""
	firstChildHasNested := false

	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				directChildren++
				if directChildren == 1 {
					firstChildName = strings.ToLower(strings.TrimSpace(t.Name.Local))
				} else {
					return true
				}
			} else if directChildren == 1 && depth == 1 {
				firstChildHasNested = true
			}
			depth++
		case xml.EndElement:
			if strings.EqualFold(t.Name.Local, "root") {
				if directChildren != 1 {
					return false
				}
				if firstChildName == "item" {
					return true
				}
				return firstChildHasNested
			}
			if depth > 0 {
				depth--
			}
		}
	}
}

func preservesCDATAStringParameter(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "content", "file_content", "text", "prompt", "query", "command", "cmd", "script", "code", "old_string", "new_string", "pattern", "path", "file_path":
		return true
	default:
		return false
	}
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

func parseMarkupInput(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	if parsed := parseStructuredToolCallInput(raw); len(parsed) > 0 {
		return parsed
	}
	if kv := parseMarkupKVObject(raw); len(kv) > 0 {
		return kv
	}
	if parsed := parseToolCallInput(raw); len(parsed) > 0 {
		return parsed
	}
	return map[string]any{"_raw": strings.TrimSpace(raw)}
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

func stripTagTextCompat(text string) string {
	return strings.TrimSpace(anyTagPatternCompat.ReplaceAllString(text, ""))
}
