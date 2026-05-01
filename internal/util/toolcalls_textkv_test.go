package util

import (
	"testing"
)

func TestParseTextKVToolCalls_Basic(t *testing.T) {
	text := `
function.name: execute_command
function.arguments: {"command":"cd scripts && python check_syntax.py example.py","cwd":null,"timeout":30}

Some other text thinking...
`
	calls := ParseToolCalls(text, []string{"execute_command"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "execute_command" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "cd scripts && python check_syntax.py example.py" {
		t.Fatalf("unexpected command arg: %v", calls[0].Input["command"])
	}
}

func TestParseTextKVToolCalls_AlreadyCalledHistoryIgnored(t *testing.T) {
	text := `
[TOOL_CALL_HISTORY]
status: already_called
origin: assistant
not_user_input: true
tool_call_id: call_3fcd15235eb94f7eae3a8de5a9cfa36b
function.name: execute_command
function.arguments: {"command":"cd scripts && python check_syntax.py example.py","cwd":null,"timeout":30}
[/TOOL_CALL_HISTORY]
`
	calls := ParseToolCalls(text, []string{"execute_command"})
	if len(calls) != 0 {
		t.Fatalf("expected already_called history block to be ignored, got %#v", calls)
	}
}

func TestParseTextKVToolCalls_Multiple(t *testing.T) {
	text := `
function.name: read_file
function.arguments: {
	"path": "abc.txt"
}

function.name: bash
function.arguments: {"command": "ls"}
`
	calls := ParseToolCalls(text, []string{"read_file", "bash"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("unexpected 1st name: %s", calls[0].Name)
	}
	if calls[1].Name != "bash" {
		t.Fatalf("unexpected 2nd name: %s", calls[1].Name)
	}
}

func TestParseTextKVToolCalls_Standalone(t *testing.T) {
	text := "function.name: read_file\nfunction.arguments: {\"path\":\"README.md\"}"
	calls := ParseStandaloneToolCalls(text, []string{"read_file"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
}

func TestParseTextKVToolCalls_CallMarkerChinese(t *testing.T) {
	text := `[调用 Read] {"file_path":"D:/git_codes/google_adk_helloworld_git/docs/GOOGLE_ADK_SourceCode_Helper.md","limit":100}`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Read" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["file_path"] != "D:/git_codes/google_adk_helloworld_git/docs/GOOGLE_ADK_SourceCode_Helper.md" {
		t.Fatalf("unexpected file_path: %#v", calls[0].Input["file_path"])
	}
}

func TestParseTextKVToolCalls_CallMarkerEnglish(t *testing.T) {
	text := `[Call Bash] {"command":"pwd","description":"check cwd"}`
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("unexpected command: %#v", calls[0].Input["command"])
	}
}

func TestParseTextKVToolCalls_CallMarkerMixedTextMultiple(t *testing.T) {
	text := `继续
[调用 Read] {"file_path":"a.md","offset":200}
[调用 Glob] {"pattern":"*.py","path":"D:/work"}
内容是什么`
	calls := ParseToolCalls(text, []string{"Read", "Glob"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "Read" || calls[1].Name != "Glob" {
		t.Fatalf("unexpected names: %#v", calls)
	}
}

func TestParseTextKVToolCalls_DirectParenBash(t *testing.T) {
	text := "使用 bash 查看当天时间\nBash(date +%Y-%m-%d)"
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "date +%Y-%m-%d" {
		t.Fatalf("unexpected command: %#v", calls[0].Input["command"])
	}
}

func TestParseTextKVToolCalls_DirectParenJSONInput(t *testing.T) {
	text := `Read({"file_path":"README.md","offset":10})`
	calls := ParseToolCalls(text, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Read" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["file_path"] != "README.md" {
		t.Fatalf("unexpected file_path: %#v", calls[0].Input["file_path"])
	}
	if calls[0].Input["offset"] != float64(10) {
		t.Fatalf("unexpected offset: %#v", calls[0].Input["offset"])
	}
}

func TestParseTextKVToolCalls_DirectParenMultipleLines(t *testing.T) {
	text := `继续
Bash(pwd)
Read(README.md)
Done`
	calls := ParseToolCalls(text, []string{"Bash", "Read"})
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Name != "Bash" || calls[1].Name != "Read" {
		t.Fatalf("unexpected names: %#v", calls)
	}
	if calls[0].Input["command"] != "pwd" {
		t.Fatalf("unexpected bash command: %#v", calls[0].Input["command"])
	}
	if calls[1].Input["file_path"] != "README.md" {
		t.Fatalf("unexpected read file_path: %#v", calls[1].Input["file_path"])
	}
}

func TestParseTextKVToolCalls_ToolUseLabelNameNextLine(t *testing.T) {
	text := "Tool use:\nmcp__exa__web_search_exa\n{\"query\":\"DeepSeek V4 测评\"}"
	calls := ParseToolCalls(text, []string{"mcp__exa__web_search_exa"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "mcp__exa__web_search_exa" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["query"] != "DeepSeek V4 测评" {
		t.Fatalf("unexpected query: %#v", calls[0].Input["query"])
	}
}

func TestParseTextKVToolCalls_ToolUseLabelInlineNameAndJSON(t *testing.T) {
	text := "Tool use: Bash\n{\"command\":\"date +%Y-%m-%d\"}"
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "date +%Y-%m-%d" {
		t.Fatalf("unexpected command: %#v", calls[0].Input["command"])
	}
}

func TestParseTextKVToolCalls_ToolUseLabelInlineDirectParen(t *testing.T) {
	text := "Tool use: Bash(date +%Y-%m-%d)"
	calls := ParseToolCalls(text, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("unexpected name: %s", calls[0].Name)
	}
	if calls[0].Input["command"] != "date +%Y-%m-%d" {
		t.Fatalf("unexpected command: %#v", calls[0].Input["command"])
	}
}

func TestNormalizeToolCallInputsForExecution_UnwrapsCommandCDATA(t *testing.T) {
	calls := []ParsedToolCall{
		{
			Name: "Bash",
			Input: map[string]any{
				"command": "   <![CDATA[git -C D:/git_repos/ds2api log --oneline -10]]>   ",
			},
		},
	}
	got := NormalizeToolCallInputsForExecution(calls)
	if len(got) != 1 {
		t.Fatalf("expected one call, got %#v", got)
	}
	if got[0].Input["command"] != "git -C D:/git_repos/ds2api log --oneline -10" {
		t.Fatalf("unexpected normalized command: %#v", got[0].Input["command"])
	}
}
