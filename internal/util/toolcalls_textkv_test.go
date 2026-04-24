package util

import (
	"testing"
)

func TestParseTextKVToolCalls_Basic(t *testing.T) {
	text := `
[TOOL_CALL_HISTORY]
status: already_called
origin: assistant
not_user_input: true
tool_call_id: call_3fcd15235eb94f7eae3a8de5a9cfa36b
function.name: execute_command
function.arguments: {"command":"cd scripts && python check_syntax.py example.py","cwd":null,"timeout":30}
[/TOOL_CALL_HISTORY]

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
