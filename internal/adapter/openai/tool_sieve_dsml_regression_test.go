package openai

import (
	"strings"
	"testing"
)

func TestProcessToolSieveHoldsDSMLTrailingPipeOpenTag(t *testing.T) {
	var state toolStreamSieveState
	events := processToolSieveChunk(&state, "<|DSML|tool_calls|\n", []string{"Bash"})
	if len(events) != 0 {
		t.Fatalf("expected no emitted events for partial DSML wrapper, got %#v", events)
	}
	if !state.capturing {
		t.Fatal("expected sieve to enter capture mode for DSML trailing-pipe wrapper")
	}
}

func TestFlushToolSieveRecoversCompleteInvokeFromMalformedDSMLClosing(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		"<|DSML|tool_calls>\n",
		"<|DSML|invoke name=\"Bash\">\n",
		"<|DSML|parameter name=\"command\">pwd</|DSML|parameter>\n",
		"</|DSML|invoke>\n",
		"</|DSML|", // broken closing tag from real-world case
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"Bash"})...)

	var text strings.Builder
	var calls []struct {
		Name  string
		Input map[string]any
	}
	for _, evt := range events {
		text.WriteString(evt.Content)
		for _, tc := range evt.ToolCalls {
			calls = append(calls, struct {
				Name  string
				Input map[string]any
			}{Name: tc.Name, Input: tc.Input})
		}
	}

	if len(calls) != 1 {
		t.Fatalf("expected malformed DSML closing to recover one tool call, got %d events=%#v", len(calls), events)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected recovered tool name Bash, got %#v", calls[0])
	}
	if got, _ := calls[0].Input["command"].(string); got != "pwd" {
		t.Fatalf("expected recovered command pwd, got %#v", calls[0].Input["command"])
	}
	if text.Len() != 0 {
		t.Fatalf("expected recovered malformed DSML closing not to leak raw text, got %q", text.String())
	}
	if state.capturing {
		t.Fatal("expected capture to be released after flush")
	}
}

func TestProcessToolSieveSuppressesAllEmptyDSMLToolBlock(t *testing.T) {
	var state toolStreamSieveState
	chunk := strings.Join([]string{
		"<|DSML|tool_calls>",
		"<|DSML|invoke name=\"Bash\">",
		"<|DSML|parameter name=\"command\"></|DSML|parameter>",
		"<|DSML|parameter name=\"description\">   </|DSML|parameter>",
		"</|DSML|invoke>",
		"</|DSML|tool_calls>",
	}, "\n")
	events := processToolSieveChunk(&state, chunk, []string{"Bash"})
	events = append(events, flushToolSieve(&state, []string{"Bash"})...)

	var text strings.Builder
	toolCalls := 0
	for _, evt := range events {
		text.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected all-empty DSML block not to produce tool calls, got %d events=%#v", toolCalls, events)
	}
	if text.Len() != 0 {
		t.Fatalf("expected all-empty DSML block not to leak text, got %q", text.String())
	}
}

func TestFlushToolSieveRecoversCompleteInvokeFromMalformedDSMLWrapper(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		"<|DSML|tool_calls>\n",
		"<|DSML|invoke name=\"Write\">\n",
		"<|DSML|parameter name=\"file_path\"><![CDATA[D:\\git_codes\\ds2api\\s1.md]]></|DSML|parameter>\n",
		"<|DSML|parameter name=\"content\"><![CDATA[test body]]></|DSML|parameter>\n",
		"</|DSML|invoke>\n",
		"</|DSML|", // broken wrapper close from real-world capture
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"Write"})...)
	}
	events = append(events, flushToolSieve(&state, []string{"Write"})...)

	var text strings.Builder
	var calls []struct {
		Name  string
		Input map[string]any
	}
	for _, evt := range events {
		text.WriteString(evt.Content)
		for _, tc := range evt.ToolCalls {
			calls = append(calls, struct {
				Name  string
				Input map[string]any
			}{Name: tc.Name, Input: tc.Input})
		}
	}

	if len(calls) != 1 {
		t.Fatalf("expected malformed wrapper to recover one tool call, got %d events=%#v", len(calls), events)
	}
	if calls[0].Name != "Write" {
		t.Fatalf("expected recovered tool name Write, got %#v", calls[0])
	}
	if got, _ := calls[0].Input["file_path"].(string); got != `D:\git_codes\ds2api\s1.md` {
		t.Fatalf("expected recovered file_path, got %#v", calls[0].Input["file_path"])
	}
	if got, _ := calls[0].Input["content"].(string); got != "test body" {
		t.Fatalf("expected recovered content, got %#v", calls[0].Input["content"])
	}
	if text.Len() != 0 {
		t.Fatalf("expected recovered malformed wrapper not to leak raw text, got %q", text.String())
	}
	if state.capturing {
		t.Fatal("expected capture to be released after flush")
	}
}

func TestConsumeXMLToolCaptureRecoversMalformedWrapperWithoutSuffixLeak(t *testing.T) {
	captured := strings.Join([]string{
		"<|DSML|tool_calls>",
		"<|DSML|invoke name=\"Bash\">",
		"<|DSML|parameter name=\"command\">pwd</|DSML|parameter>",
		"</|DSML|invoke>",
		"</|DSML|",
	}, "\n")

	prefix, calls, suffix, ready := consumeXMLToolCapture(captured, []string{"Bash"})
	if !ready {
		t.Fatal("expected malformed wrapper capture to be recoverable")
	}
	if prefix != "" {
		t.Fatalf("expected empty prefix, got %q", prefix)
	}
	if suffix != "" {
		t.Fatalf("expected empty suffix for malformed wrapper tail, got %q", suffix)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one recovered call, got %#v", calls)
	}
}
