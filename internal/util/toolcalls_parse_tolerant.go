package util

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

var trailingJSONCommaPattern = regexp.MustCompile(`,\s*([}\]])`)

func decodeToolCallJSONPayload(payload string) (decoded any, truncatedRecovered bool, ok bool) {
	raw := strings.TrimSpace(payload)
	if raw == "" {
		return nil, false, false
	}
	if json.Unmarshal([]byte(raw), &decoded) == nil {
		return decoded, false, true
	}

	repaired := repairInvalidJSONBackslashes(raw)
	repaired = RepairLooseJSON(repaired)
	if json.Unmarshal([]byte(repaired), &decoded) == nil {
		return decoded, false, true
	}

	tolerant, truncatedLike := repairTolerantJSON(repaired)
	if json.Unmarshal([]byte(tolerant), &decoded) == nil {
		return decoded, truncatedLike, true
	}

	if recovered, ok := decodeLastBalancedJSONPrefix(tolerant); ok {
		return recovered, true, true
	}
	return nil, false, false
}

func repairTolerantJSON(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s) + 8)

	stack := make([]byte, 0, 16)
	inString := false
	escaped := false
	quote := byte(0)
	closedString := false
	appendedClosers := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				b.WriteByte(ch)
				continue
			}
			if ch == '\\' {
				escaped = true
				b.WriteByte(ch)
				continue
			}
			switch ch {
			case '\n':
				b.WriteString(`\n`)
				continue
			case '\r':
				b.WriteString(`\r`)
				continue
			case '\t':
				b.WriteString(`\t`)
				continue
			}
			if ch == quote {
				inString = false
				quote = 0
			}
			b.WriteByte(ch)
			continue
		}

		if ch == '"' {
			inString = true
			quote = '"'
			b.WriteByte(ch)
			continue
		}

		switch ch {
		case '{':
			stack = append(stack, '}')
			b.WriteByte(ch)
		case '[':
			stack = append(stack, ']')
			b.WriteByte(ch)
		case '}', ']':
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				if top == ch {
					stack = stack[:len(stack)-1]
				}
			}
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}

	if inString {
		b.WriteByte('"')
		closedString = true
	}
	for i := len(stack) - 1; i >= 0; i-- {
		b.WriteByte(stack[i])
		appendedClosers = true
	}

	repaired := b.String()
	for {
		next := trailingJSONCommaPattern.ReplaceAllString(repaired, `$1`)
		if next == repaired {
			break
		}
		repaired = next
	}
	return repaired, closedString || appendedClosers
}

func decodeLastBalancedJSONPrefix(input string) (any, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, false
	}
	end := len(trimmed)
	for end > 0 {
		idx := strings.LastIndexAny(trimmed[:end], "}]")
		if idx < 0 {
			return nil, false
		}
		candidate := strings.TrimSpace(trimmed[:idx+1])
		if candidate == "" {
			return nil, false
		}
		var decoded any
		if json.Unmarshal([]byte(candidate), &decoded) == nil {
			return decoded, true
		}
		if idx <= 0 {
			return nil, false
		}
		for idx > 0 && !utf8.RuneStart(trimmed[idx]) {
			idx--
		}
		end = idx
	}
	return nil, false
}

func shouldHoldRecoveredTruncatedToolCalls(calls []ParsedToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, tc := range calls {
		if toolCallNeedsContinuation(tc) {
			return true
		}
	}
	return false
}

var largePayloadToolNames = map[string]struct{}{
	"write":        {},
	"edit":         {},
	"multiedit":    {},
	"editnotebook": {},
	"notebookedit": {},
	"write_file":   {},
	"edit_file":    {},
	"apply_patch":  {},
}

var largePayloadArgFields = map[string]struct{}{
	"content":    {},
	"text":       {},
	"new_string": {},
	"new_str":    {},
	"file_text":  {},
	"code":       {},
}

func toolCallNeedsContinuation(tc ParsedToolCall) bool {
	name := strings.ToLower(strings.TrimSpace(tc.Name))
	if _, ok := largePayloadToolNames[name]; ok {
		return true
	}
	for key, value := range tc.Input {
		s, ok := value.(string)
		if !ok {
			continue
		}
		if _, hit := largePayloadArgFields[strings.ToLower(strings.TrimSpace(key))]; hit {
			return true
		}
		if len(s) >= 1500 {
			return true
		}
	}
	return false
}
