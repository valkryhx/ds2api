package openai

import (
	"strings"
	"testing"
)

func TestProcessToolSieveBareInvokeInlineProseDoesNotStall(t *testing.T) {
	var state toolStreamSieveState
	chunk := "Use `<invoke name=\"read_file\">` as plain documentation text."
	events := processToolSieveChunk(&state, chunk, []string{"read_file"})

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected inline invoke prose to remain text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected inline invoke prose to stream immediately, got %q", textContent.String())
	}
	if state.capturing {
		t.Fatal("expected inline invoke prose not to leave stream capture open")
	}
}

func TestProcessToolSieveBareInvokeExampleReleasesWhenNotRepairable(t *testing.T) {
	var state toolStreamSieveState
	chunks := []string{
		`Example: <invoke name="read_file"><parameter name="path">README.md</parameter>`,
		"</invoke> then continue.",
	}
	var events []toolStreamEvent
	for _, c := range chunks {
		events = append(events, processToolSieveChunk(&state, c, []string{"read_file"})...)
	}

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected non-repairable bare invoke to remain text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != strings.Join(chunks, "") {
		t.Fatalf("expected non-repairable bare invoke to pass through, got %q", textContent.String())
	}
	if state.capturing {
		t.Fatal("expected non-repairable bare invoke not to leave stream capture open")
	}
}
