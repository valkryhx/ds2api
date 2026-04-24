package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"ds2api/internal/util"
)

func injectToolPrompt(messages []map[string]any, tools []any, policy util.ToolChoicePolicy) ([]map[string]any, []string) {
	if policy.IsNone() {
		return messages, nil
	}
	toolSchemas := make([]string, 0, len(tools))
	names := make([]string, 0, len(tools))
	isAllowed := func(name string) bool {
		if strings.TrimSpace(name) == "" {
			return false
		}
		if len(policy.Allowed) == 0 {
			return true
		}
		_, ok := policy.Allowed[name]
		return ok
	}

	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			fn = tool
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		schema, _ := fn["parameters"].(map[string]any)
		name = strings.TrimSpace(name)
		if !isAllowed(name) {
			continue
		}
		names = append(names, name)
		if desc == "" {
			desc = "No description available"
		}
		b, _ := json.Marshal(schema)
		toolSchemas = append(toolSchemas, fmt.Sprintf("Tool: %s\nDescription: %s\nParameters: %s", name, desc, string(b)))
	}
	if len(toolSchemas) == 0 {
		return messages, names
	}
	toolPrompt := strings.Join([]string{
		"You have access to these tools:",
		"",
		strings.Join(toolSchemas, "\n\n"),
		"",
		"TOOL CALL OUTPUT CONTRACT (STRICT)",
		"When you need to call tools, output ONLY a raw JSON object (no markdown fences, no prose) like this:",
		`{"tool_calls": [{"name": "tool_name", "input": {"param": "value"}}]}`,
		"",
		"EXAMPLE",
		"User: Please check the weather in Beijing and Shanghai, and update my todo list.",
		"Assistant:",
		`{"tool_calls": [`,
		`  {"name": "get_weather", "input": {"city": "Beijing"}},`,
		`  {"name": "get_weather", "input": {"city": "Shanghai"}},`,
		`  {"name": "update_todo", "input": {"todos": [{"content": "Buy milk"}, {"content": "Write report"}]}}`,
		"]}",
		"",
		"History markers in conversation:",
		"- [TOOL_CALL_HISTORY]...[/TOOL_CALL_HISTORY] means a tool call you already made earlier.",
		"- [TOOL_RESULT_HISTORY]...[/TOOL_RESULT_HISTORY] means the runtime returned a tool result (not user input).",
		"",
		"IMPORTANT:",
		"1) If calling tools, output ONLY the raw JSON object. Do NOT wrap it in ``` fences.",
		"2) The response must start with '{' and end with '}' when calling tools.",
		"3) The JSON must contain top-level key \"tool_calls\" with an array value [].",
		"4) Each call item MUST be exactly this shape: {\"name\": \"...\", \"input\": {...}}.",
		"5) \"input\" MUST be a JSON object (use {} when no parameters).",
		"6) JSON SYNTAX STRICTLY REQUIRED: all property names MUST use double quotes.",
		"7) ARRAY FORMAT: lists MUST be wrapped in [] (never comma-separated objects without brackets).",
		"8) NEVER use pseudo-call text formats such as '[调用 Read] {...}', '[Call Read] {...}', '调用工具:' or 'Tool use:'.",
		"9) NEVER use XML/markup call formats like <tool_calls>, <tool_call>, <function_call>, <invoke>, or parameter tags.",
		"10) Do NOT output function.name/function.arguments text blocks.",
		"11) Do NOT emit [TOOL_CALL_HISTORY] / [TOOL_RESULT_HISTORY] blocks as new tool calls.",
		"12) If no tool call is needed, answer normally and do NOT output tool_calls JSON.",
		"13) After receiving a tool result, you MUST use it to produce the final answer.",
		"14) Only call another tool when the previous result is missing required data or returned an error.",
		"15) Do not repeat a tool call that is already satisfied by an existing [TOOL_RESULT_HISTORY] block.",
	}, "\n")
	if policy.Mode == util.ToolChoiceRequired {
		toolPrompt += "\n5) For this response, you MUST call at least one tool from the allowed list."
	}
	if policy.Mode == util.ToolChoiceForced && strings.TrimSpace(policy.ForcedName) != "" {
		toolPrompt += "\n5) For this response, you MUST call exactly this tool name: " + strings.TrimSpace(policy.ForcedName)
		toolPrompt += "\n6) Do not call any other tool."
	}

	for i := range messages {
		if messages[i]["role"] == "system" {
			old, _ := messages[i]["content"].(string)
			messages[i]["content"] = strings.TrimSpace(old + "\n\n" + toolPrompt)
			return messages, names
		}
	}
	messages = append([]map[string]any{{"role": "system", "content": toolPrompt}}, messages...)
	return messages, names
}

func formatIncrementalStreamToolCallDeltas(deltas []toolCallDelta, ids map[int]string) []map[string]any {
	if len(deltas) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(deltas))
	for _, d := range deltas {
		if d.Name == "" && d.Arguments == "" {
			continue
		}
		callID, ok := ids[d.Index]
		if !ok || callID == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			ids[d.Index] = callID
		}
		item := map[string]any{
			"index": d.Index,
			"id":    callID,
			"type":  "function",
		}
		fn := map[string]any{}
		if d.Name != "" {
			fn["name"] = d.Name
		}
		if d.Arguments != "" {
			fn["arguments"] = d.Arguments
		}
		if len(fn) > 0 {
			item["function"] = fn
		}
		out = append(out, item)
	}
	return out
}

func filterIncrementalToolCallDeltasByAllowed(deltas []toolCallDelta, allowedNames []string, seenNames map[int]string) []toolCallDelta {
	if len(deltas) == 0 {
		return nil
	}
	allowed := namesToSet(allowedNames)
	if len(allowed) == 0 {
		for _, d := range deltas {
			if d.Name != "" {
				seenNames[d.Index] = "__blocked__"
			}
		}
		return nil
	}
	out := make([]toolCallDelta, 0, len(deltas))
	for _, d := range deltas {
		if d.Name != "" {
			if _, ok := allowed[d.Name]; !ok {
				seenNames[d.Index] = "__blocked__"
				continue
			}
			seenNames[d.Index] = d.Name
			out = append(out, d)
			continue
		}
		name := strings.TrimSpace(seenNames[d.Index])
		if name == "" || name == "__blocked__" {
			continue
		}
		out = append(out, d)
	}
	return out
}

func formatFinalStreamToolCallsWithStableIDs(calls []util.ParsedToolCall, ids map[int]string) []map[string]any {
	if len(calls) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for i, c := range calls {
		callID := ""
		if ids != nil {
			callID = strings.TrimSpace(ids[i])
		}
		if callID == "" {
			callID = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if ids != nil {
				ids[i] = callID
			}
		}
		args, _ := json.Marshal(c.Input)
		out = append(out, map[string]any{
			"index": i,
			"id":    callID,
			"type":  "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": string(args),
			},
		})
	}
	return out
}
