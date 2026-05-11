package claude

import (
	"testing"

	"ds2api/internal/config"
)

func TestNormalizeClaudeRequest(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-opus-4-6",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"stream": true,
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}
	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if norm.Standard.ResolvedModel == "" {
		t.Fatalf("expected resolved model")
	}
	if !norm.Standard.Stream {
		t.Fatalf("expected stream=true")
	}
	if len(norm.Standard.ToolNames) == 0 {
		t.Fatalf("expected tool names")
	}
	if norm.Standard.FinalPrompt == "" {
		t.Fatalf("expected non-empty final prompt")
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoExistingSystemMessage(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{"role": "system", "content": "baseline rule"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected into final prompt, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "baseline rule") {
		t.Fatalf("expected existing system message preserved, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestInjectsToolsIntoTopLevelSystem(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model":  "claude-sonnet-4-5",
		"system": "top-level system",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{"name": "search", "description": "Search"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "top-level system") {
		t.Fatalf("expected top-level system preserved, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "You have access to these tools") {
		t.Fatalf("expected tool prompt injected, got=%q", norm.Standard.FinalPrompt)
	}
}

func TestNormalizeClaudeRequestIncludesDeferredToolNamesFromMessageContent(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "<available-deferred-tools>\nBash\nRead\nWrite\n</available-deferred-tools>\n你调用bash 看看当前D盘大小",
			},
		},
		"tools": []any{
			map[string]any{"name": "ToolSearch", "description": "Fetch deferred tool schemas"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	got := map[string]bool{}
	for _, name := range norm.Standard.ToolNames {
		got[name] = true
	}
	for _, want := range []string{"ToolSearch", "Bash", "Read", "Write"} {
		if !got[want] {
			t.Fatalf("expected deferred tool name %q in %v", want, norm.Standard.ToolNames)
		}
	}
}

func TestNormalizeClaudeRequestPrefersDirectDeferredToolCalls(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{}`)
	store := config.LoadStore()
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "<available-deferred-tools>\nBash\nRead\nWrite\n</available-deferred-tools>\n把总结写入到 1221.md",
			},
		},
		"tools": []any{
			map[string]any{"name": "ToolSearch", "description": "Fetch deferred tool schemas"},
		},
	}

	norm, err := normalizeClaudeRequest(store, req)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	if !containsStr(norm.Standard.FinalPrompt, "call that tool directly in DSML") {
		t.Fatalf("expected direct deferred-tool instruction, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "Read -> file_path") {
		t.Fatalf("expected Read hint, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "Write -> file_path, content") {
		t.Fatalf("expected Write hint, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "Bash -> command") {
		t.Fatalf("expected Bash hint, got=%q", norm.Standard.FinalPrompt)
	}
	if containsStr(norm.Standard.FinalPrompt, "deferred-only until loaded") {
		t.Fatalf("did not expect deferred-only wording, got=%q", norm.Standard.FinalPrompt)
	}
	if !containsStr(norm.Standard.FinalPrompt, "Never claim that you called, wrote, read, or executed a tool") {
		t.Fatalf("expected anti-hallucination tool-execution instruction, got=%q", norm.Standard.FinalPrompt)
	}
}
