package devcapture

import (
	"io"
	"strings"
	"sync"
	"testing"
)

func TestStorePushKeepsNewestWithinLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 2, maxBodyBytes: 1024}
	for i := 0; i < 3; i++ {
		session := s.Start("test", "http://x", "", map[string]any{"seq": i})
		if session == nil {
			t.Fatal("expected session")
		}
		rc := session.WrapBody(io.NopCloser(strings.NewReader("ok")), 200)
		_, _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	items := s.Snapshot()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].RequestBody, `"seq":2`) {
		t.Fatalf("expected newest first, got %#v", items[0].RequestBody)
	}
	if !strings.Contains(items[1].RequestBody, `"seq":1`) {
		t.Fatalf("expected second newest, got %#v", items[1].RequestBody)
	}
}

func TestWrapBodyTruncatesByLimit(t *testing.T) {
	s := &Store{enabled: true, limit: 5, maxBodyBytes: 4}
	session := s.Start("test", "http://x", "acc1", map[string]any{"x": 1})
	if session == nil {
		t.Fatal("expected session")
	}
	rc := session.WrapBody(io.NopCloser(strings.NewReader("abcdef")), 200)
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	items := s.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ResponseBody != "abcd" {
		t.Fatalf("expected truncated body, got %q", items[0].ResponseBody)
	}
	if !items[0].ResponseTruncated {
		t.Fatal("expected truncated flag true")
	}
	if items[0].AccountID != "acc1" {
		t.Fatalf("expected account id, got %q", items[0].AccountID)
	}
}

func TestRecordCapturesImmediateRequestPayload(t *testing.T) {
	s := &Store{enabled: true, limit: 5, maxBodyBytes: 1024}
	s.Record("claude_inbound", "/v1/messages", "acc1", 0, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "write 123.md"}},
		"tools":    []any{map[string]any{"name": "Write"}},
	}, nil)

	items := s.Snapshot()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Label != "claude_inbound" {
		t.Fatalf("expected claude_inbound label, got %q", items[0].Label)
	}
	if !strings.Contains(items[0].RequestBody, "123.md") {
		t.Fatalf("expected request body to include 123.md, got %q", items[0].RequestBody)
	}
	if !strings.Contains(items[0].RequestBody, "Write") {
		t.Fatalf("expected request body to include tool schema, got %q", items[0].RequestBody)
	}
}

func TestNewFromSettingsUsesConfigValues(t *testing.T) {
	enabled := true
	s := NewFromSettings(Settings{Enabled: &enabled, Limit: 20, MaxBodyBytes: 4096})
	if !s.Enabled() {
		t.Fatal("expected enabled from settings")
	}
	if s.Limit() != 20 {
		t.Fatalf("expected limit 20, got %d", s.Limit())
	}
	if s.MaxBodyBytes() != 4096 {
		t.Fatalf("expected max body bytes 4096, got %d", s.MaxBodyBytes())
	}
}

func TestNewFromSettingsEnvOverridesConfig(t *testing.T) {
	enabled := true
	t.Setenv("DS2API_DEV_PACKET_CAPTURE", "false")
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_LIMIT", "3")
	t.Setenv("DS2API_DEV_PACKET_CAPTURE_MAX_BODY_BYTES", "8192")

	s := NewFromSettings(Settings{Enabled: &enabled, Limit: 20, MaxBodyBytes: 4096})
	if s.Enabled() {
		t.Fatal("expected env to disable capture")
	}
	if s.Limit() != 3 {
		t.Fatalf("expected env limit 3, got %d", s.Limit())
	}
	if s.MaxBodyBytes() != 8192 {
		t.Fatalf("expected env max body bytes 8192, got %d", s.MaxBodyBytes())
	}
}

func TestConfigurePersistsForSubsequentGlobalCalls(t *testing.T) {
	enabled := true
	configured := Configure(Settings{Enabled: &enabled, Limit: 20, MaxBodyBytes: 4096})
	t.Cleanup(func() {
		Configure(Settings{})
	})

	got := Global()
	if got != configured {
		t.Fatalf("expected Global to return configured store")
	}
	if got.Limit() != 20 {
		t.Fatalf("expected configured limit 20, got %d", got.Limit())
	}
}

func TestGlobalAndConfigureAreSafeToCallConcurrently(t *testing.T) {
	enabled := true
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if Global() == nil {
				t.Error("expected global store")
			}
		}()
		go func(limit int) {
			defer wg.Done()
			if Configure(Settings{Enabled: &enabled, Limit: limit, MaxBodyBytes: 4096}) == nil {
				t.Error("expected configured store")
			}
		}(i + 1)
	}
	wg.Wait()
}
