package util

import "strings"

type ParsedToolCall struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ToolCallParseResult struct {
	Calls             []ParsedToolCall
	SawToolCallSyntax bool
	RejectedByPolicy  bool
	RejectedToolNames []string
}

const toolChoiceNoneBlockName = "__tool_choice_none_block__"

func ParseToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseToolCallsDetailed(text, availableToolNames).Calls
}

func ParseToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	result := ToolCallParseResult{}
	if strings.TrimSpace(text) == "" {
		return result
	}
	text = stripFencedCodeBlocks(text)
	if strings.TrimSpace(text) == "" {
		return result
	}
	result.SawToolCallSyntax = looksLikeToolCallSyntax(text)

	candidates := buildToolCallCandidates(text)
	var parsed []ParsedToolCall
	for _, candidate := range candidates {
		tc := parseToolCallsPayload(candidate)
		if len(tc) == 0 {
			tc = parseXMLToolCalls(candidate)
		}
		if len(tc) == 0 {
			tc = parseMarkupToolCalls(candidate)
		}
		if len(tc) == 0 {
			tc = parseTextKVToolCalls(candidate)
		}
		if len(tc) > 0 {
			parsed = tc
			result.SawToolCallSyntax = true
			break
		}
	}
	if len(parsed) == 0 {
		parsed = parseXMLToolCalls(text)
		if len(parsed) == 0 {
			parsed = parseTextKVToolCalls(text)
			if len(parsed) == 0 {
				return result
			}
		}
		result.SawToolCallSyntax = true
	}

	calls, rejectedNames := filterToolCallsDetailed(parsed, availableToolNames)
	result.Calls = calls
	result.RejectedToolNames = rejectedNames
	result.RejectedByPolicy = len(rejectedNames) > 0 && len(calls) == 0
	return result
}

func ParseStandaloneToolCalls(text string, availableToolNames []string) []ParsedToolCall {
	return ParseStandaloneToolCallsDetailed(text, availableToolNames).Calls
}

func ParseStandaloneToolCallsDetailed(text string, availableToolNames []string) ToolCallParseResult {
	result := ToolCallParseResult{}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return result
	}
	if fencedPayload, ok := extractStandaloneFencedPayload(trimmed); ok {
		if strings.Contains(strings.ToLower(fencedPayload), "tool_calls") {
			parsed := parseToolCallsPayload(fencedPayload)
			if len(parsed) > 0 {
				calls, rejectedNames := filterToolCallsDetailed(parsed, availableToolNames)
				if len(calls) == 0 && shouldBypassToolAllowList(availableToolNames) {
					calls = normalizeParsedToolCallsNoPolicy(parsed)
					if len(calls) > 0 {
						rejectedNames = nil
					}
				}
				result.Calls = calls
				result.RejectedToolNames = rejectedNames
				result.RejectedByPolicy = len(rejectedNames) > 0 && len(calls) == 0
				result.SawToolCallSyntax = len(parsed) > 0
				return result
			}
		}
	}

	// Preprocess: strip thinking blocks (e.g., <thinking>...</thinking>) which
	// can interfere with tool call extraction, especially for deepseek-v4-pro-think model.
	cleaned := stripThinkingBlocks(trimmed)
	if cleaned == "" {
		cleaned = trimmed // fallback
	}

	candidates := []string{cleaned}
	if fencedPayload, ok := extractStandaloneFencedPayload(cleaned); ok {
		// Compatibility: standalone fenced JSON snippets are examples by default.
		// Only allow fenced payloads when they are XML/DSML-style tool markup.
		if parsed := parseMarkupToolCalls(fencedPayload); len(parsed) > 0 {
			candidates = append([]string{fencedPayload}, candidates...)
		}
	} else if trailingPayload, prefix, ok := extractTrailingStandaloneJSONObjectCandidate(cleaned); ok {
		// Allow "prose + trailing tool payload" when the tail is a pure JSON object and
		// the prose does not look like an explicit example context.
		if !looksLikeToolExamplePrefix(prefix) {
			candidates = append([]string{trailingPayload}, candidates...)
		}
	} else if looksLikeToolExampleContext(cleaned) {
		return result
	}
	for _, c := range candidates {
		if looksLikeToolCallSyntax(c) {
			result.SawToolCallSyntax = true
			break
		}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		parsed := parseToolCallsPayload(candidate)
		if len(parsed) == 0 {
			parsed = parseXMLToolCalls(candidate)
		}
		if len(parsed) == 0 {
			parsed = parseMarkupToolCalls(candidate)
		}
		if len(parsed) == 0 {
			parsed = parseTextKVToolCalls(candidate)
		}
		if len(parsed) > 0 {
			result.SawToolCallSyntax = true
			calls, rejectedNames := filterToolCallsDetailed(parsed, availableToolNames)
			// Compatibility fallback: allow parsed standalone tool calls to pass
			// through when policy filtering rejects them, unless tool_choice=none
			// sentinel explicitly requests hard blocking.
			if len(calls) == 0 && shouldBypassToolAllowList(availableToolNames) {
				calls = normalizeParsedToolCallsNoPolicy(parsed)
				if len(calls) > 0 {
					rejectedNames = nil
				}
			}
			result.Calls = calls
			result.RejectedToolNames = rejectedNames
			result.RejectedByPolicy = len(rejectedNames) > 0 && len(calls) == 0
			return result
		}
	}
	return result
}

func parseMarkupToolCalls(text string) []ParsedToolCall {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	normalized, ok := normalizeDSMLToolCallMarkup(trimmed)
	if !ok {
		return nil
	}
	parsed := parseXMLToolCalls(normalized)
	if len(parsed) == 0 {
		parsed = parseMarkupToolCallsLegacy(normalized)
	}
	if len(parsed) == 0 && strings.Contains(strings.ToLower(normalized), "<![cdata[") {
		recovered := SanitizeLooseCDATA(normalized)
		if recovered != normalized {
			parsed = parseXMLToolCalls(recovered)
			if len(parsed) == 0 {
				parsed = parseMarkupToolCallsLegacy(recovered)
			}
		}
	}
	return parsed
}

func normalizeParsedToolCallsNoPolicy(parsed []ParsedToolCall) []ParsedToolCall {
	if len(parsed) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(parsed))
	for _, tc := range parsed {
		name := strings.TrimSpace(tc.Name)
		if name == "" {
			continue
		}
		input := tc.Input
		if input == nil {
			input = map[string]any{}
		}
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

func shouldBypassToolAllowList(availableToolNames []string) bool {
	for _, name := range availableToolNames {
		if strings.EqualFold(strings.TrimSpace(name), toolChoiceNoneBlockName) {
			return false
		}
	}
	return true
}

func filterToolCallsDetailed(parsed []ParsedToolCall, availableToolNames []string) ([]ParsedToolCall, []string) {
	allowed := map[string]struct{}{}
	allowedCanonical := map[string]string{}
	for _, name := range availableToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
		lower := strings.ToLower(trimmed)
		if _, exists := allowedCanonical[lower]; !exists {
			allowedCanonical[lower] = trimmed
		}
	}
	if len(allowed) == 0 {
		rejectedSet := map[string]struct{}{}
		rejected := make([]string, 0, len(parsed))
		for _, tc := range parsed {
			if tc.Name == "" {
				continue
			}
			if _, ok := rejectedSet[tc.Name]; ok {
				continue
			}
			rejectedSet[tc.Name] = struct{}{}
			rejected = append(rejected, tc.Name)
		}
		return nil, rejected
	}
	out := make([]ParsedToolCall, 0, len(parsed))
	rejectedSet := map[string]struct{}{}
	rejected := make([]string, 0)
	for _, tc := range parsed {
		if tc.Name == "" {
			continue
		}
		matchedName := resolveAllowedToolName(tc.Name, allowed, allowedCanonical)
		if matchedName == "" {
			if _, ok := rejectedSet[tc.Name]; !ok {
				rejectedSet[tc.Name] = struct{}{}
				rejected = append(rejected, tc.Name)
			}
			continue
		}
		tc.Name = matchedName
		if tc.Input == nil {
			tc.Input = map[string]any{}
		}
		out = append(out, tc)
	}
	return out, rejected
}

func resolveAllowedToolName(name string, allowed map[string]struct{}, allowedCanonical map[string]string) string {
	return resolveAllowedToolNameWithLooseMatch(name, allowed, allowedCanonical)
}

func parseToolCallsPayload(payload string) []ParsedToolCall {
	decoded, truncatedRecovered, ok := decodeToolCallJSONPayload(payload)
	if !ok {
		return nil
	}
	parsed := parseToolCallPayloadValue(decoded)
	if len(parsed) == 0 {
		return nil
	}
	// Strategy split:
	// - truncated short tool payloads are executable;
	// - truncated large payload tools (e.g. write/edit with long content) are held.
	if truncatedRecovered && shouldHoldRecoveredTruncatedToolCalls(parsed) {
		return nil
	}
	return parsed
}

func parseToolCallPayloadValue(decoded any) []ParsedToolCall {
	switch v := decoded.(type) {
	case map[string]any:
		if tc, ok := v["tool_calls"]; ok {
			return parseToolCallList(tc)
		}
		if parsed, ok := parseToolCallItem(v); ok {
			return []ParsedToolCall{parsed}
		}
	case []any:
		return parseToolCallList(v)
	}
	return nil
}

func looksLikeToolCallSyntax(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "tool_calls") || strings.Contains(lower, "function.name:") {
		return true
	}
	hasDSML, hasCanonical := ContainsToolCallWrapperSyntaxOutsideIgnored(text)
	if hasDSML || hasCanonical {
		return true
	}
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<function_call") ||
		strings.Contains(lower, "<invoke")
}

func parseToolCallList(v any) []ParsedToolCall {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := parseToolCallItem(m); ok {
			out = append(out, tc)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseToolCallItem(m map[string]any) (ParsedToolCall, bool) {
	name, _ := m["name"].(string)
	inputRaw, hasInput := m["input"]
	if fn, ok := m["function"].(map[string]any); ok {
		if name == "" {
			name, _ = fn["name"].(string)
		}
		if !hasInput {
			if v, ok := fn["arguments"]; ok {
				inputRaw = v
				hasInput = true
			}
		}
	}
	if !hasInput {
		for _, key := range []string{"arguments", "args", "parameters", "params"} {
			if v, ok := m[key]; ok {
				inputRaw = v
				hasInput = true
				break
			}
		}
	}
	if !hasInput {
		if implicit, ok := extractImplicitToolInput(m); ok {
			inputRaw = implicit
			hasInput = true
		}
	}
	if strings.TrimSpace(name) == "" {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{
		Name:  strings.TrimSpace(name),
		Input: parseToolCallInput(inputRaw),
	}, true
}

func extractImplicitToolInput(m map[string]any) (map[string]any, bool) {
	if len(m) == 0 {
		return nil, false
	}
	excluded := map[string]struct{}{
		"name":         {},
		"input":        {},
		"function":     {},
		"arguments":    {},
		"args":         {},
		"parameters":   {},
		"params":       {},
		"id":           {},
		"type":         {},
		"index":        {},
		"tool_call_id": {},
		"call_id":      {},
	}
	out := map[string]any{}
	for k, v := range m {
		if _, skip := excluded[k]; skip {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
