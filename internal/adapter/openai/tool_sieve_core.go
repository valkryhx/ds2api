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
		start := findToolSegmentStart(state, pending, toolNames)
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

		safe, hold := splitSafeContentForToolDetection(state, pending, toolNames)
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

func splitSafeContentForToolDetection(state *toolStreamSieveState, s string, toolNames []string) (safe, hold string) {
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
	if directIdx := findPartialDirectToolTagStart(s, toolNames); directIdx >= 0 {
		if insideCodeFenceWithState(state, s[:directIdx]) {
			return s, ""
		}
		if directIdx > 0 {
			return s[:directIdx], s[directIdx:]
		}
		return "", s
	}
	return s, ""
}

func findToolSegmentStart(state *toolStreamSieveState, s string, toolNames []string) int {
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
			break
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
	if directIdx := findDirectToolTagStart(s, toolNames); directIdx >= 0 {
		if !insideCodeFenceWithState(state, s[:directIdx]) {
			return directIdx
		}
	}
	return -1
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
		if hasOpenDirectToolTag(captured, toolNames) {
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
		if hasOpenDirectToolTag(captured, toolNames) {
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
	"<bash",
	"<shell",
	"<shell_command",
	"<execute_command",
	"<powershell",
	"<cmd",
	"<terminal",
	"<read",
	"<read_file",
	"<cat",
	"<glob",
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

var directToolTagFallbackNames = []string{
	"bash",
	"shell",
	"shell_command",
	"execute_command",
	"powershell",
	"cmd",
	"terminal",
	"read",
	"read_file",
	"cat",
	"glob",
}

func findDirectToolTagStart(s string, toolNames []string) int {
	if s == "" {
		return -1
	}
	allowed := buildDirectToolTagLookup(toolNames)
	lower := strings.ToLower(s)
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		local, _, ok := scanDirectOpenTag(s, lower, i)
		if !ok {
			continue
		}
		if _, ok := allowed[local]; ok {
			return i
		}
	}
	return -1
}

func hasOpenDirectToolTag(captured string, toolNames []string) bool {
	if captured == "" {
		return false
	}
	allowed := buildDirectToolTagLookup(toolNames)
	lower := strings.ToLower(captured)
	for i := 0; i < len(captured); i++ {
		if captured[i] != '<' {
			continue
		}
		local, bodyStart, ok := scanDirectOpenTag(captured, lower, i)
		if !ok {
			continue
		}
		if _, ok := allowed[local]; !ok {
			continue
		}
		if _, _, closed := findDirectCloseTag(captured, lower, bodyStart, local); !closed {
			return true
		}
	}
	return false
}

func findPartialDirectToolTagStart(s string, toolNames []string) int {
	if s == "" {
		return -1
	}
	lastLT := strings.LastIndex(s, "<")
	if lastLT < 0 {
		return -1
	}
	start := includeDuplicateLeadingLessThan(s, lastLT)
	tail := s[start:]
	if strings.Contains(tail, ">") {
		return -1
	}

	allowed := buildDirectToolTagLookup(toolNames)
	lowerTail := strings.ToLower(tail)
	if !strings.HasPrefix(lowerTail, "<") || strings.HasPrefix(lowerTail, "</") {
		return -1
	}

	nameStart := 1
	nameEnd := nameStart
	for nameEnd < len(lowerTail) && isDirectToolTagNameChar(lowerTail[nameEnd]) {
		nameEnd++
	}
	if nameEnd == nameStart {
		return -1
	}
	raw := lowerTail[nameStart:nameEnd]
	local := raw
	if idx := strings.LastIndex(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		local = raw[idx+1:]
	}
	if _, ok := allowed[local]; !ok {
		// Allow prefix match for streaming partial tags like "<bas".
		for k := range allowed {
			if strings.HasPrefix(k, local) {
				return start
			}
		}
		return -1
	}
	return start
}

func buildDirectToolTagLookup(toolNames []string) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(v string) {
		key := strings.ToLower(strings.TrimSpace(v))
		if key == "" {
			return
		}
		out[key] = struct{}{}
		out[strings.ReplaceAll(key, "-", "_")] = struct{}{}
		out[strings.ReplaceAll(key, "_", "-")] = struct{}{}
		if idx := strings.LastIndex(key, "."); idx >= 0 && idx+1 < len(key) {
			local := key[idx+1:]
			out[local] = struct{}{}
			out[strings.ReplaceAll(local, "-", "_")] = struct{}{}
			out[strings.ReplaceAll(local, "_", "-")] = struct{}{}
		}
	}
	for _, name := range directToolTagFallbackNames {
		add(name)
	}
	for _, name := range toolNames {
		add(name)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "bash", "shell", "shell_command", "execute_command", "powershell", "cmd", "terminal":
			for _, a := range []string{"bash", "shell", "shell_command", "execute_command", "powershell", "cmd", "terminal"} {
				add(a)
			}
		case "read", "read_file", "cat":
			for _, a := range []string{"read", "read_file", "cat"} {
				add(a)
			}
		case "glob":
			add("glob")
		}
	}
	return out
}

func scanDirectOpenTag(s, lower string, start int) (local string, bodyStart int, ok bool) {
	if start < 0 || start >= len(s) || s[start] != '<' {
		return "", 0, false
	}
	if start+1 < len(s) && s[start+1] == '/' {
		return "", 0, false
	}
	nameStart := start + 1
	nameEnd := nameStart
	for nameEnd < len(s) && isDirectToolTagNameChar(s[nameEnd]) {
		nameEnd++
	}
	if nameEnd == nameStart {
		return "", 0, false
	}
	raw := lower[nameStart:nameEnd]
	local = raw
	if idx := strings.LastIndex(raw, ":"); idx >= 0 && idx+1 < len(raw) {
		local = raw[idx+1:]
	}
	if !hasDirectTagBoundary(s, nameEnd) {
		return "", 0, false
	}
	tagEnd := findDirectTagEnd(s, nameEnd)
	if tagEnd < 0 {
		return "", 0, false
	}
	if tagEnd > start && s[tagEnd-1] == '/' {
		return "", 0, false
	}
	return local, tagEnd + 1, true
}

func findDirectCloseTag(s, lower string, from int, local string) (closeStart int, closeEnd int, ok bool) {
	target := strings.ToLower(strings.TrimSpace(local))
	for i := maxInt(from, 0); i < len(s); {
		idx := strings.Index(lower[i:], "</")
		if idx < 0 {
			return -1, -1, false
		}
		start := i + idx
		nameStart := start + 2
		nameEnd := nameStart
		for nameEnd < len(s) && isDirectToolTagNameChar(s[nameEnd]) {
			nameEnd++
		}
		if nameEnd == nameStart {
			i = start + 2
			continue
		}
		raw := strings.ToLower(s[nameStart:nameEnd])
		localName := raw
		if p := strings.LastIndex(raw, ":"); p >= 0 && p+1 < len(raw) {
			localName = raw[p+1:]
		}
		if localName != target || !hasDirectTagBoundary(s, nameEnd) {
			i = nameEnd
			continue
		}
		end := findDirectTagEnd(s, nameEnd)
		if end < 0 {
			return -1, -1, false
		}
		return start, end + 1, true
	}
	return -1, -1, false
}

func isDirectToolTagNameChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' ||
		ch == '-' ||
		ch == ':' ||
		ch == '.'
}

func hasDirectTagBoundary(text string, idx int) bool {
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

func findDirectTagEnd(text string, from int) int {
	quote := byte(0)
	for i := maxInt(from, 0); i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return i
		}
	}
	return -1
}
