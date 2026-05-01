package openai

import (
	"strings"

	"ds2api/internal/util"
)

type toolStreamSieveState struct {
	pending                 strings.Builder
	capture                 strings.Builder
	capturing               bool
	codeFenceStack         []int
	codeFencePendingTicks  int
	codeFencePendingTildes int
	codeFenceNotLineStart  bool
	pendingToolRaw          string
	pendingToolCalls        []util.ParsedToolCall
	disableDeltas           bool
	toolNameSent            bool
	toolName                string
	toolArgsStart           int
	toolArgsSent            int
	toolArgsString          bool
	toolArgsDone            bool
}

type toolStreamEvent struct {
	Content        string
	ToolCalls      []util.ParsedToolCall
	ToolCallDeltas []toolCallDelta
}

type toolCallDelta struct {
	Index     int
	Name      string
	Arguments string
}

func (s *toolStreamSieveState) resetIncrementalToolState() {
	s.disableDeltas = false
	s.toolNameSent = false
	s.toolName = ""
	s.toolArgsStart = -1
	s.toolArgsSent = -1
	s.toolArgsString = false
	s.toolArgsDone = false
}

func (s *toolStreamSieveState) noteText(content string) {
	if !hasMeaningfulText(content) {
		return
	}
	updateCodeFenceState(s, content)
}

func hasMeaningfulText(text string) bool {
	return strings.TrimSpace(text) != ""
}

func looksLikeToolExampleContext(text string) bool {
	return insideCodeFence(text)
}

func insideCodeFenceWithState(state *toolStreamSieveState, text string) bool {
	if state == nil {
		return insideCodeFence(text)
	}
	simulated := simulateCodeFenceState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFencePendingTildes,
		!state.codeFenceNotLineStart,
		text,
	)
	return len(simulated.stack) > 0
}

func insideCodeFence(text string) bool {
	if text == "" {
		return false
	}
	return len(simulateCodeFenceState(nil, 0, 0, true, text).stack) > 0
}

func updateCodeFenceState(state *toolStreamSieveState, text string) {
	if state == nil || !hasMeaningfulText(text) {
		return
	}
	next := simulateCodeFenceState(
		state.codeFenceStack,
		state.codeFencePendingTicks,
		state.codeFencePendingTildes,
		!state.codeFenceNotLineStart,
		text,
	)
	state.codeFenceStack = next.stack
	state.codeFencePendingTicks = next.pendingTicks
	state.codeFencePendingTildes = next.pendingTildes
	state.codeFenceNotLineStart = !next.lineStart
}

type codeFenceSimulation struct {
	stack         []int
	pendingTicks  int
	pendingTildes int
	lineStart     bool
}

func simulateCodeFenceState(stack []int, pendingTicks, pendingTildes int, lineStart bool, text string) codeFenceSimulation {
	chunk := text
	nextStack := append([]int(nil), stack...)
	ticks := pendingTicks
	tildes := pendingTildes
	atLineStart := lineStart

	flushPending := func() {
		if ticks > 0 {
			if atLineStart && ticks >= 3 {
				applyFenceMarker(&nextStack, ticks)
			}
			atLineStart = false
			ticks = 0
		}
		if tildes > 0 {
			if atLineStart && tildes >= 3 {
				applyFenceMarker(&nextStack, -tildes)
			}
			atLineStart = false
			tildes = 0
		}
	}

	for i := 0; i < len(chunk); i++ {
		ch := chunk[i]
		if ch == '`' {
			if tildes > 0 {
				flushPending()
			}
			ticks++
			continue
		}
		if ch == '~' {
			if ticks > 0 {
				flushPending()
			}
			tildes++
			continue
		}
		flushPending()
		switch ch {
		case '\n', '\r':
			atLineStart = true
		case ' ', '\t':
			if atLineStart {
				continue
			}
			atLineStart = false
		default:
			atLineStart = false
		}
	}

	return codeFenceSimulation{
		stack:         nextStack,
		pendingTicks:  ticks,
		pendingTildes: tildes,
		lineStart:     atLineStart,
	}
}

func applyFenceMarker(stack *[]int, marker int) {
	if stack == nil || marker == 0 {
		return
	}
	if len(*stack) == 0 {
		*stack = append(*stack, marker)
		return
	}
	top := (*stack)[len(*stack)-1]
	sameType := (top > 0 && marker > 0) || (top < 0 && marker < 0)
	if !sameType {
		*stack = append(*stack, marker)
		return
	}
	absMarker := marker
	absTop := top
	if absMarker < 0 {
		absMarker = -absMarker
	}
	if absTop < 0 {
		absTop = -absTop
	}
	if absMarker >= absTop {
		*stack = (*stack)[:len(*stack)-1]
		return
	}
	*stack = append(*stack, marker)
}
