package sse

import (
	"bufio"
	"context"
	"io"
)

const (
	parsedLineBufferSize = 128
	lineReaderBufferSize = 64 * 1024
)

// StartParsedLinePump scans an upstream DeepSeek SSE body and emits normalized
// line parse results. It centralizes scanner setup + current fragment type
// tracking for all streaming adapters.
func StartParsedLinePump(ctx context.Context, body io.Reader, thinkingEnabled bool, initialType string) (<-chan LineResult, <-chan error) {
	out := make(chan LineResult, parsedLineBufferSize)
	done := make(chan error, 1)
	go func() {
		defer close(out)
		reader := bufio.NewReaderSize(body, lineReaderBufferSize)
		currentType := initialType
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) == 0 && err != nil {
				if err == io.EOF {
					done <- nil
				} else {
					done <- err
				}
				return
			}
			line = append([]byte(nil), line...)
			result := ParseDeepSeekContentLine(line, thinkingEnabled, currentType)
			currentType = result.NextType
			select {
			case out <- result:
			case <-ctx.Done():
				done <- ctx.Err()
				return
			}
			if err != nil {
				if err == io.EOF {
					done <- nil
				} else {
					done <- err
				}
				return
			}
		}
	}()
	return out, done
}
