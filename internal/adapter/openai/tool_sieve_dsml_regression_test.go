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

func TestFlushToolSieveIncompleteDSMLClosingFallsBackToText(t *testing.T) {
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
	toolCalls := 0
	for _, evt := range events {
		text.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected malformed DSML block to fallback as text, got %d tool calls events=%#v", toolCalls, events)
	}
	want := strings.Join(chunks, "")
	if text.String() != want {
		t.Fatalf("expected malformed DSML block to pass through unchanged, got %q want %q", text.String(), want)
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
