package util

import "strings"

func NormalizeToolCallInputsForExecution(calls []ParsedToolCall) []ParsedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(calls))
	for _, tc := range calls {
		name := strings.TrimSpace(tc.Name)
		if name == "" {
			continue
		}
		input := sanitizeToolCallInputMap(tc.Input)
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

func sanitizeToolCallInputMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = sanitizeToolCallInputValue(v)
	}
	return out
}

func sanitizeToolCallInputValue(v any) any {
	switch x := v.(type) {
	case string:
		return sanitizeToolCallStringValue(x)
	case map[string]any:
		return sanitizeToolCallInputMap(x)
	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, sanitizeToolCallInputValue(item))
		}
		return out
	default:
		return v
	}
}

func sanitizeToolCallStringValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if inner, ok := extractStandaloneCDATA(trimmed); ok {
		return strings.TrimSpace(inner)
	}
	return raw
}
