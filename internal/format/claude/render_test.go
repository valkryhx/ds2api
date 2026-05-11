package claude

import (
	"strings"
	"testing"
)

func TestBuildMessageResponseDetectsToolCallsFromThinkingFallback(t *testing.T) {
	resp := BuildMessageResponse(
		"msg_1",
		"claude-sonnet-4-5",
		[]any{map[string]any{"role": "user", "content": "hi"}},
		`{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`,
		"",
		[]string{"search"},
	)

	if resp["stop_reason"] != "tool_use" {
		t.Fatalf("expected stop_reason=tool_use, got=%#v", resp["stop_reason"])
	}
	content, _ := resp["content"].([]map[string]any)
	if len(content) < 2 {
		t.Fatalf("expected thinking + tool_use content blocks, got=%#v", resp["content"])
	}
	last := content[len(content)-1]
	if last["type"] != "tool_use" {
		t.Fatalf("expected last content block tool_use, got=%#v", last["type"])
	}
	if last["name"] != "search" {
		t.Fatalf("expected tool name search, got=%#v", last["name"])
	}
}

func TestBuildMessageResponseSkipsThinkingFallbackWhenFinalTextExists(t *testing.T) {
	resp := BuildMessageResponse(
		"msg_1",
		"claude-sonnet-4-5",
		[]any{map[string]any{"role": "user", "content": "hi"}},
		`{"tool_calls":[{"name":"search","input":{"q":"go"}}]}`,
		"normal answer",
		[]string{"search"},
	)

	if resp["stop_reason"] != "end_turn" {
		t.Fatalf("expected stop_reason=end_turn, got=%#v", resp["stop_reason"])
	}

	content, _ := resp["content"].([]map[string]any)
	foundText := false
	foundTool := false
	for _, block := range content {
		if block["type"] == "text" && block["text"] == "normal answer" {
			foundText = true
		}
		if block["type"] == "tool_use" {
			foundTool = true
		}
	}
	if !foundText {
		t.Fatalf("expected text block with finalText, got=%#v", resp["content"])
	}
	if foundTool {
		t.Fatalf("unexpected tool_use block when finalText exists, got=%#v", resp["content"])
	}
}

func TestDetectClaudeToolCallsKeepsCompleteDSMLBeforeMalformedTail(t *testing.T) {
	finalText := strings.Join([]string{
		`<|DSML|tool_calls>`,
		`  <|DSML|invoke name="shell">`,
		`    <|DSML|parameter name="command"><![CDATA[powershell.exe -Command "Set-Content -Path 'D:\git_codes\ds2api\123.md' -Value 'hello'" ]]></|DSML|parameter>`,
		`  </|DSML|invoke>`,
		`</|DSML|tool_calls>`,
		``,
		`<|DSML|tool_calls>`,
		`  <|DSML|invoke name="Write">`,
		`    <|DSML|parameter name="file_path"></|DSML|parameter>`,
		`    <|DSML|parameter name="content"><![CDATA[hello]]></|DSML|parameter>`,
		`  </|DSML|invoke>`,
		`</|DSML>`,
	}, "\n")

	detected := DetectClaudeToolCalls(finalText, "", []string{"shell", "Write"})
	if len(detected.Calls) != 1 {
		t.Fatalf("expected one complete DSML call before malformed tail, got %#v", detected)
	}
	if detected.Calls[0].Name != "shell" {
		t.Fatalf("expected shell tool call, got %#v", detected.Calls[0])
	}
	if !strings.Contains(detected.Calls[0].Input["command"].(string), `Set-Content -Path 'D:\git_codes\ds2api\123.md'`) {
		t.Fatalf("unexpected command payload: %#v", detected.Calls[0].Input)
	}
}

func TestBuildMessageResponseDropsToolUseWithEmptyRequiredSchemaField(t *testing.T) {
	resp := BuildMessageResponse(
		"msg_1",
		"claude-sonnet-4-5",
		[]any{map[string]any{"role": "user", "content": "write file"}},
		"",
		`<|DSML|tool_calls><|DSML|invoke name="Write"><|DSML|parameter name="file_path"></|DSML|parameter><|DSML|parameter name="content"><![CDATA[hello]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`,
		[]string{"Write"},
		[]any{map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type":     "object",
				"required": []any{"file_path", "content"},
				"properties": map[string]any{
					"file_path": map[string]any{"type": "string"},
					"content":   map[string]any{"type": "string"},
				},
			},
		}},
	)

	if resp["stop_reason"] == "tool_use" {
		t.Fatalf("expected invalid Write call to be dropped, got=%#v", resp)
	}
	content, _ := resp["content"].([]map[string]any)
	for _, block := range content {
		if block["type"] == "tool_use" {
			t.Fatalf("unexpected tool_use block for empty required file_path: %#v", resp["content"])
		}
		if block["type"] == "text" && strings.Contains(asString(block["text"]), "<|DSML|tool_calls>") {
			t.Fatalf("unexpected raw DSML leak for empty required file_path: %#v", resp["content"])
		}
	}
}

func TestDetectClaudeToolCallsUndeclaredWriteStillInterceptedWhenOnlyToolSearchDeclared(t *testing.T) {
	finalText := `<|DSML|tool_calls><|DSML|invoke name="Write"><|DSML|parameter name="file_path"><![CDATA[D:\git_codes\ds2api\e123.md]]></|DSML|parameter><|DSML|parameter name="content"><![CDATA[# translated]]></|DSML|parameter></|DSML|invoke></|DSML|tool_calls>`

	detected := DetectClaudeToolCalls(finalText, "", []string{"ToolSearch"})
	if len(detected.Calls) != 1 {
		t.Fatalf("expected undeclared Write to still be intercepted, got %#v", detected)
	}
	if detected.Calls[0].Name != "Write" {
		t.Fatalf("expected Write tool call, got %#v", detected.Calls[0])
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
