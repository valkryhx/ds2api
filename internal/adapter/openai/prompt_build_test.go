package openai

import (
	"strings"
	"testing"
)

func TestBuildOpenAIFinalPrompt_HandlerPathIncludesToolRoundtripSemantics(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "查北京天气"},
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{
					"id": "call_1",
					"function": map[string]any{
						"name":      "get_weather",
						"arguments": "{\"city\":\"beijing\"}",
					},
				},
			},
		},
		map[string]any{
			"role":         "tool",
			"tool_call_id": "call_1",
			"name":         "get_weather",
			"content":      map[string]any{"temp": 18, "condition": "sunny"},
		},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, toolNames := buildOpenAIFinalPrompt(messages, tools, "")
	if len(toolNames) != 1 || toolNames[0] != "get_weather" {
		t.Fatalf("unexpected tool names: %#v", toolNames)
	}
	if !strings.Contains(finalPrompt, "tool_call_id: call_1") ||
		!strings.Contains(finalPrompt, "function.name: get_weather") ||
		!strings.Contains(finalPrompt, "[TOOL_RESULT_HISTORY]") ||
		!strings.Contains(finalPrompt, `"condition":"sunny"`) {
		t.Fatalf("handler finalPrompt missing tool roundtrip semantics: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_VercelPreparePathKeepsFinalAnswerInstruction(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "You are helpful"},
		map[string]any{"role": "user", "content": "请调用工具"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, "After receiving a tool result, you MUST use it to produce the final answer.") {
		t.Fatalf("vercel prepare finalPrompt missing final-answer instruction: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "Only call another tool when the previous result is missing required data or returned an error.") {
		t.Fatalf("vercel prepare finalPrompt missing retry guard instruction: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "[TOOL_RESULT_HISTORY]") {
		t.Fatalf("vercel prepare finalPrompt missing history marker instruction: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_UsesDSMLToolcallInstruction(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "run tool"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "shell_command",
				"description": "run command",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, "<｜DSML｜tool_calls>") {
		t.Fatalf("expected DSML wrapper instruction, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, "|DSML|") {
		t.Fatalf("expected prompt contract to use fullwidth DSML delimiters only, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, "output ONLY the raw JSON object") {
		t.Fatalf("did not expect old raw-JSON contract after DSML migration, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, "NEVER use XML/markup call formats") {
		t.Fatalf("did not expect XML/DSML ban after migration, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_RequiresExactDeclaredToolName(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "run tool"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "shell_command",
				"description": "run command",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, "Use the exact tool name from the provided schema.") {
		t.Fatalf("expected exact tool-name guidance, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "if the tool name is shell_command, do not output shell") {
		t.Fatalf("expected alias warning for shell_command, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_IncludesDeclaredStringParameterSchemaForCodexTools(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "run tool"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "functions.shell_command",
				"description": "run command",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
					"required": []any{"command"},
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, `"command":{"type":"string"}`) {
		t.Fatalf("expected tool schema with command:string in prompt, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "Use the exact tool name from the provided schema.") {
		t.Fatalf("expected exact schema-name instruction, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_RequiresCompleteDSMLClosersAndNonEmptyRequiredParams(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "write file to D:\\git_codes\\ds2api\\s1.md"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Write",
				"description": "write file",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{"type": "string"},
						"content":   map[string]any{"type": "string"},
					},
					"required": []any{"file_path", "content"},
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, "You MUST output the complete closing tags </｜DSML｜invoke> and </｜DSML｜tool_calls>.") {
		t.Fatalf("expected complete-closing-tag instruction, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "Never emit a partial or truncated closing tag such as </｜DSML｜.") {
		t.Fatalf("expected partial-closing-tag prohibition, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, "Fill every required parameter with a non-empty value before calling a tool.") {
		t.Fatalf("expected non-empty required-parameter instruction, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_IncludesStaticWriteFewShotExample(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "write file"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Write",
				"description": "write file",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, "CORRECT EXAMPLES") {
		t.Fatalf("expected correct examples block, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜invoke name="Write">`) {
		t.Fatalf("expected Write example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="file_path"><![CDATA[notes.txt]]></｜DSML｜parameter>`) {
		t.Fatalf("expected Write file_path example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="content"><![CDATA[Hello world]]></｜DSML｜parameter>`) {
		t.Fatalf("expected Write content example, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, `<｜DSML｜invoke name="write_to_file">`) {
		t.Fatalf("did not expect write_to_file example when only Write is declared, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_IncludesStaticWriteToFileFewShotExample(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "write file"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "write_to_file",
				"description": "write file",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, `<｜DSML｜invoke name="write_to_file">`) {
		t.Fatalf("expected write_to_file example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="path"><![CDATA[notes.txt]]></｜DSML｜parameter>`) {
		t.Fatalf("expected write_to_file path example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="content"><![CDATA[Hello world]]></｜DSML｜parameter>`) {
		t.Fatalf("expected write_to_file content example, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, `<｜DSML｜parameter name="file_path"><![CDATA[notes.txt]]></｜DSML｜parameter>`) {
		t.Fatalf("did not expect Write-style file_path example for write_to_file, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_IncludesStaticMultiEditFewShotExample(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "edit file"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "MultiEdit",
				"description": "edit file multiple times",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, `<｜DSML｜invoke name="MultiEdit">`) {
		t.Fatalf("expected MultiEdit example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="edits"><item><old_string><![CDATA[foo]]></old_string><new_string><![CDATA[bar]]></new_string></item></｜DSML｜parameter>`) {
		t.Fatalf("expected MultiEdit edits item-array example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="file_path"><![CDATA[README.md]]></｜DSML｜parameter>`) {
		t.Fatalf("expected MultiEdit file_path example, got: %q", finalPrompt)
	}
}

func TestBuildOpenAIFinalPrompt_IncludesStaticEditFewShotExample(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "edit file"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "Edit",
				"description": "edit file",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}

	finalPrompt, _ := buildOpenAIFinalPrompt(messages, tools, "")
	if !strings.Contains(finalPrompt, `<｜DSML｜invoke name="Edit">`) {
		t.Fatalf("expected Edit example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="file_path"><![CDATA[README.md]]></｜DSML｜parameter>`) {
		t.Fatalf("expected Edit file_path example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="old_string"><![CDATA[foo]]></｜DSML｜parameter>`) {
		t.Fatalf("expected Edit old_string example, got: %q", finalPrompt)
	}
	if !strings.Contains(finalPrompt, `<｜DSML｜parameter name="new_string"><![CDATA[bar]]></｜DSML｜parameter>`) {
		t.Fatalf("expected Edit new_string example, got: %q", finalPrompt)
	}
	if strings.Contains(finalPrompt, `<｜DSML｜parameter name="edits">`) {
		t.Fatalf("did not expect MultiEdit-style edits array in Edit example, got: %q", finalPrompt)
	}
}
