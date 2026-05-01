package openai

import (
	"strings"

	"ds2api/internal/util"
)

func processToolSieveChunk(state *toolStreamSieveState, chunk string, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	if chunk != "" {
		state.pending.WriteString(chunk)
	}
	events := make([]toolStreamEvent, 0, 2)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, toolStreamEvent{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}
	for {
		if state.capturing {
			if state.pending.Len() > 0 {
				state.capture.WriteString(state.pending.String())
				state.pending.Reset()
			}
			prefix, calls, suffix, ready := consumeToolCapture(state, toolNames)
			if !ready {
				break
			}
			captured := state.capture.String()
			state.capture.Reset()
			state.capturing = false
			state.resetIncrementalToolState()
			if len(calls) > 0 {
				if prefix != "" {
					state.noteText(prefix)
					events = append(events, toolStreamEvent{Content: prefix})
				}
				if suffix != "" {
					state.pending.WriteString(suffix)
				}
				_ = captured
				state.pendingToolCalls = calls
				continue
			}
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, toolStreamEvent{Content: prefix})
			}
			if suffix != "" {
				state.pending.WriteString(suffix)
			}
			continue
		}

		pending := state.pending.String()
		if pending == "" {
			break
		}
		start := findToolSegmentStart(state, pending)
		if start >= 0 {
			prefix := pending[:start]
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, toolStreamEvent{Content: prefix})
			}
			state.pending.Reset()
			state.capture.WriteString(pending[start:])
			state.capturing = true
			state.resetIncrementalToolState()
			continue
		}

		safe, hold := splitSafeContentForToolDetection(state, pending)
		if safe == "" {
			break
		}
		state.pending.Reset()
		state.pending.WriteString(hold)
		state.noteText(safe)
		events = append(events, toolStreamEvent{Content: safe})
	}

	return events
}

func flushToolSieve(state *toolStreamSieveState, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	events := processToolSieveChunk(state, "", toolNames)
	if len(state.pendingToolCalls) > 0 {
		events = append(events, toolStreamEvent{ToolCalls: state.pendingToolCalls})
		state.pendingToolRaw = ""
		state.pendingToolCalls = nil
	}
	if state.capturing {
		consumedPrefix, consumedCalls, consumedSuffix, ready := consumeToolCapture(state, toolNames)
		if ready {
			if consumedPrefix != "" {
				state.noteText(consumedPrefix)
				events = append(events, toolStreamEvent{Content: consumedPrefix})
			}
			if len(consumedCalls) > 0 {
				events = append(events, toolStreamEvent{ToolCalls: consumedCalls})
			}
			if consumedSuffix != "" {
				state.noteText(consumedSuffix)
				events = append(events, toolStreamEvent{Content: consumedSuffix})
			}
		} else {
			content := state.capture.String()
			if content != "" {
				recovered := util.SanitizeLooseCDATA(content)
				if recovered != content {
					if prefix, calls, suffix, recoveredReady := consumeXMLToolCapture(recovered, toolNames); recoveredReady && len(calls) > 0 {
						if prefix != "" {
							state.noteText(prefix)
							events = append(events, toolStreamEvent{Content: prefix})
						}
						events = append(events, toolStreamEvent{ToolCalls: calls})
						if suffix != "" {
							state.noteText(suffix)
							events = append(events, toolStreamEvent{Content: suffix})
						}
					} else {
						state.noteText(content)
						events = append(events, toolStreamEvent{Content: content})
					}
				} else {
					state.noteText(content)
					events = append(events, toolStreamEvent{Content: content})
				}
			}
		}
		state.capture.Reset()
		state.capturing = false
		state.resetIncrementalToolState()
	}
	if state.pending.Len() > 0 {
		content := state.pending.String()
		if calls := parseStandaloneFencedToolCalls(content, toolNames); len(calls) > 0 {
			events = append(events, toolStreamEvent{ToolCalls: calls})
			state.pending.Reset()
			return events
		}
		parsed := util.ParseStandaloneToolCallsDetailed(content, toolNames)
		if len(parsed.Calls) > 0 {
			if looksLikeToolExampleContext(content) {
				state.noteText(content)
				events = append(events, toolStreamEvent{Content: content})
			} else {
				events = append(events, toolStreamEvent{ToolCalls: parsed.Calls})
			}
		} else {
			state.noteText(content)
			events = append(events, toolStreamEvent{Content: content})
		}
		state.pending.Reset()
	}
	return events
}

func splitSafeContentForToolDetection(state *toolStreamSieveState, s string) (safe, hold string) {
	if s == "" {
		return "", ""
	}
	if hasLeadingCodeFence(s) {
		return "", s
	}
	suspiciousStart := findSuspiciousPrefixStart(s)
	if suspiciousStart >= 0 {
		if suspiciousStart > 0 {
			return s[:suspiciousStart], s[suspiciousStart:]
		}
		return "", s
	}
	if xmlIdx := findPartialXMLToolTagStart(s); xmlIdx >= 0 {
		if insideCodeFenceWithState(state, s[:xmlIdx]) {
			return s, ""
		}
		if xmlIdx > 0 {
			return s[:xmlIdx], s[xmlIdx:]
		}
		return "", s
	}
	return s, ""
}

func findToolSegmentStart(state *toolStreamSieveState, s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	offset := 0
	for {
		bestKeyIdx := -1
		matchedKeyword := ""
		for _, kw := range toolSieveKeywords {
			idx := strings.Index(lower[offset:], kw)
			if idx >= 0 {
				absIdx := offset + idx
				if bestKeyIdx < 0 || absIdx < bestKeyIdx {
					bestKeyIdx = absIdx
					matchedKeyword = kw
				}
			}
		}
		if bestKeyIdx < 0 {
			return -1
		}
		keyIdx := bestKeyIdx
		start := keyIdx
		if isStructuredToolKeyword(matchedKeyword) {
			start = strings.LastIndex(s[:keyIdx], "{")
			if start < 0 {
				start = keyIdx
			}
		}
		if !insideCodeFenceWithState(state, s[:start]) {
			return start
		}
		offset = keyIdx + len(matchedKeyword)
	}
}

func includeDuplicateLeadingLessThan(s string, idx int) int {
	for idx > 0 && s[idx-1] == '<' {
		idx--
	}
	return idx
}

func hasLeadingCodeFence(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return strings.HasPrefix(s[i:], "```")
		}
	}
	return false
}

func isStandaloneJSONFencedToolCall(s string) bool {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return false
	}
	if !strings.HasSuffix(trimmed, "```") {
		return false
	}
	if strings.Count(trimmed, "```") != 2 {
		return false
	}
	parsed := util.ParseStandaloneToolCallsDetailed(trimmed, nil)
	return len(parsed.Calls) > 0
}

func parseStandaloneFencedToolCalls(s string, toolNames []string) []util.ParsedToolCall {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return nil
	}
	if strings.Count(trimmed, "```") != 2 {
		return nil
	}
	payload, ok := extractStandaloneFencedJSONPayload(trimmed)
	if !ok {
		return nil
	}
	parsed := util.ParseStandaloneToolCallsDetailed(payload, toolNames)
	if len(parsed.Calls) == 0 {
		return nil
	}
	return parsed.Calls
}

func extractStandaloneFencedJSONPayload(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return "", false
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return "", false
	}
	head := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(lines[0], "```")))
	if head != "" && head != "json" {
		return "", false
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", false
	}
	payload := strings.Join(lines[1:len(lines)-1], "\n")
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", false
	}
	return payload, true
}

func isStandaloneToolUsePayload(s string) bool {
	trimmed := strings.TrimSpace(s)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "tool use:") {
		return false
	}
	parsed := util.ParseStandaloneToolCallsDetailed(trimmed, nil)
	return len(parsed.Calls) > 0
}

func findSuspiciousPrefixStart(s string) int {
	start := -1
	indices := []int{
		strings.LastIndex(s, "{"),
		strings.LastIndex(s, "["),
		strings.LastIndex(s, "```"),
	}
	for _, idx := range indices {
		if idx > start {
			start = idx
		}
	}
	return start
}

func consumeToolCapture(state *toolStreamSieveState, toolNames []string) (prefix string, calls []util.ParsedToolCall, suffix string, ready bool) {
	captured := state.capture.String()
	if captured == "" {
		return "", nil, "", false
	}

	if xmlPrefix, xmlCalls, xmlSuffix, xmlReady := consumeXMLToolCapture(captured, toolNames); xmlReady {
		return xmlPrefix, xmlCalls, xmlSuffix, true
	}
	lower := strings.ToLower(captured)
	keyIdx := -1
	matchedKeyword := ""
	for _, kw := range toolSieveKeywords {
		idx := strings.Index(lower, kw)
		if idx >= 0 && (keyIdx < 0 || idx < keyIdx) {
			keyIdx = idx
			matchedKeyword = kw
		}
	}
	if keyIdx < 0 {
		if isStandaloneJSONFencedToolCall(captured) {
			parsed := util.ParseStandaloneToolCallsDetailed(captured, toolNames)
			if len(parsed.Calls) > 0 {
				return "", parsed.Calls, "", true
			}
		}
		if isStandaloneToolUsePayload(captured) {
			parsed := util.ParseStandaloneToolCallsDetailed(captured, toolNames)
			if len(parsed.Calls) > 0 {
				return stripInlineToolUsePrefix(captured), parsed.Calls, "", true
			}
		}
		if hasOpenXMLToolTag(captured) || shouldKeepBareInvokeCapture(captured) {
			return "", nil, "", false
		}
		return captured, nil, "", true
	}

	if !isStructuredToolKeyword(matchedKeyword) {
		prefixPart := captured[:keyIdx]
		if insideCodeFenceWithState(state, prefixPart) {
			return captured, nil, "", true
		}
		if matchedKeyword == "<invoke" {
			if hasOpenXMLToolTag(captured) || shouldKeepBareInvokeCapture(captured) {
				return "", nil, "", false
			}
			return captured, nil, "", true
		}
		parsed := util.ParseStandaloneToolCallsDetailed(captured[keyIdx:], toolNames)
		if len(parsed.Calls) > 0 {
			if strings.HasPrefix(strings.ToLower(captured[keyIdx:]), "tool use:") {
				return stripInlineToolUsePrefix(captured), parsed.Calls, "", true
			}
			return prefixPart, parsed.Calls, "", true
		}
		if strings.HasPrefix(strings.ToLower(captured[keyIdx:]), "tool use:") {
			if fallback := util.ParseToolCallsDetailed(captured[keyIdx:], toolNames); len(fallback.Calls) > 0 {
				return stripInlineToolUsePrefix(captured), fallback.Calls, "", true
			}
		}
		if strings.HasPrefix(strings.ToLower(captured[keyIdx:]), "tool use:") &&
			strings.Contains(captured[keyIdx:], "\n") &&
			!strings.Contains(captured[keyIdx:], "(") {
			return "", nil, "", false
		}
		if parsed.SawToolCallSyntax && parsed.RejectedByPolicy {
			return prefixPart, nil, "", true
		}
		if hasOpenXMLToolTag(captured) || shouldKeepBareInvokeCapture(captured) {
			return "", nil, "", false
		}
		return captured, nil, "", true
	}

	start := strings.LastIndex(captured[:keyIdx], "{")
	if start < 0 {
		start = keyIdx
	}
	obj, end, ok := extractJSONObjectFrom(captured, start)
	if !ok {
		return "", nil, "", false
	}
	prefixPart := captured[:start]
	suffixPart := captured[end:]
	if insideCodeFenceWithState(state, prefixPart) {
		return captured, nil, "", true
	}
	parsed := util.ParseStandaloneToolCallsDetailed(obj, toolNames)
	if len(parsed.Calls) == 0 {
		if parsed.SawToolCallSyntax && parsed.RejectedByPolicy {
			return prefixPart, nil, suffixPart, true
		}
		return captured, nil, "", true
	}
	return prefixPart, parsed.Calls, suffixPart, true
}

var toolSieveKeywords = []string{
	"<|dsml|tool_calls",
	"<tool_calls",
	"<invoke",
	"<tool_call",
	"<function_call",
	"tool_calls",
	"function.name:",
	"[tool_call_history]",
	"tool use:",
	"tool call:",
	"调用工具:",
}

func isStructuredToolKeyword(kw string) bool {
	switch kw {
	case "tool_calls", "function.name:":
		return true
	default:
		return false
	}
}
