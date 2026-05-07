package devcapture

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultLimit        = 5
	defaultMaxBodyBytes = 2 * 1024 * 1024
	defaultOutputDir    = "logs/dev_captures"
	maxLimit            = 50
)

type Settings struct {
	Enabled       *bool
	Limit         int
	MaxBodyBytes  int
	PersistToDisk *bool
	OutputDir     string
}

type Entry struct {
	ID                string `json:"id"`
	CreatedAt         int64  `json:"created_at"`
	Label             string `json:"label"`
	URL               string `json:"url"`
	AccountID         string `json:"account_id,omitempty"`
	StatusCode        int    `json:"status_code"`
	RequestBody       string `json:"request_body"`
	ResponseBody      string `json:"response_body"`
	ResponseTruncated bool   `json:"response_truncated"`
}

type Store struct {
	mu            sync.Mutex
	enabled       bool
	limit         int
	maxBodyBytes  int
	persistToDisk bool
	outputDir     string
	items         []Entry
}

type Session struct {
	store      *Store
	id         string
	createdAt  int64
	label      string
	url        string
	accountID  string
	requestRaw string
}

type captureBody struct {
	rc         io.ReadCloser
	s          *Session
	statusCode int
	buf        strings.Builder
	truncated  bool
	finalized  bool
}

var (
	globalMu   sync.Mutex
	globalInst *Store
)

func Global() *Store {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalInst == nil {
		globalInst = NewFromEnv()
	}
	return globalInst
}

func NewFromEnv() *Store {
	return NewFromSettings(Settings{})
}

func NewFromSettings(settings Settings) *Store {
	enabled := !isVercelRuntime()
	if settings.Enabled != nil {
		enabled = *settings.Enabled
	}
	if raw, ok := os.LookupEnv("DS2API_DEV_PACKET_CAPTURE"); ok {
		enabled = parseBool(raw)
	}
	limit := settings.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if raw := strings.TrimSpace(os.Getenv("DS2API_DEV_PACKET_CAPTURE_LIMIT")); raw != "" {
		limit = parseIntWithDefault(raw, defaultLimit)
	}
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	maxBodyBytes := settings.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	if raw := strings.TrimSpace(os.Getenv("DS2API_DEV_PACKET_CAPTURE_MAX_BODY_BYTES")); raw != "" {
		maxBodyBytes = parseIntWithDefault(raw, defaultMaxBodyBytes)
	}
	if maxBodyBytes < 1024 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	persistToDisk := false
	if settings.PersistToDisk != nil {
		persistToDisk = *settings.PersistToDisk
	}
	if raw, ok := os.LookupEnv("DS2API_DEV_PACKET_CAPTURE_PERSIST_TO_DISK"); ok {
		persistToDisk = parseBool(raw)
	}
	outputDir := strings.TrimSpace(settings.OutputDir)
	if outputDir == "" {
		outputDir = defaultOutputDir
	}
	if raw := strings.TrimSpace(os.Getenv("DS2API_DEV_PACKET_CAPTURE_OUTPUT_DIR")); raw != "" {
		outputDir = raw
	}
	return &Store{
		enabled:       enabled,
		limit:         limit,
		maxBodyBytes:  maxBodyBytes,
		persistToDisk: persistToDisk,
		outputDir:     outputDir,
		items:         make([]Entry, 0, limit),
	}
}

func Configure(settings Settings) *Store {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalInst = NewFromSettings(settings)
	return globalInst
}

func isVercelRuntime() bool {
	return strings.TrimSpace(os.Getenv("VERCEL")) != "" || strings.TrimSpace(os.Getenv("NOW_REGION")) != ""
}

func (s *Store) Enabled() bool {
	if s == nil {
		return false
	}
	return s.enabled
}

func (s *Store) Limit() int {
	if s == nil {
		return defaultLimit
	}
	return s.limit
}

func (s *Store) MaxBodyBytes() int {
	if s == nil {
		return defaultMaxBodyBytes
	}
	return s.maxBodyBytes
}

func (s *Store) PersistToDisk() bool {
	if s == nil {
		return false
	}
	return s.persistToDisk
}

func (s *Store) OutputDir() string {
	if s == nil {
		return defaultOutputDir
	}
	return s.outputDir
}

func (s *Store) Snapshot() []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.items))
	copy(out, s.items)
	return out
}

func (s *Store) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = s.items[:0]
}

func (s *Store) Start(label, url, accountID string, requestPayload any) *Session {
	if s == nil || !s.enabled {
		return nil
	}
	return &Session{
		store:      s,
		id:         "cap_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		createdAt:  time.Now().Unix(),
		label:      strings.TrimSpace(label),
		url:        strings.TrimSpace(url),
		accountID:  strings.TrimSpace(accountID),
		requestRaw: marshalPayload(requestPayload),
	}
}

func (s *Store) Record(label, url, accountID string, statusCode int, requestPayload any, responsePayload any) {
	if s == nil || !s.enabled {
		return
	}
	requestBody, _ := truncateString(marshalPayload(requestPayload), s.maxBodyBytes)
	responseBody, responseTruncated := truncateString(marshalPayload(responsePayload), s.maxBodyBytes)
	entry := Entry{
		ID:                "cap_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		CreatedAt:         time.Now().Unix(),
		Label:             strings.TrimSpace(label),
		URL:               strings.TrimSpace(url),
		AccountID:         strings.TrimSpace(accountID),
		StatusCode:        statusCode,
		RequestBody:       requestBody,
		ResponseBody:      responseBody,
		ResponseTruncated: responseTruncated,
	}
	s.push(entry)
}

func (s *Session) WrapBody(rc io.ReadCloser, statusCode int) io.ReadCloser {
	if s == nil || rc == nil {
		return rc
	}
	return &captureBody{
		rc:         rc,
		s:          s,
		statusCode: statusCode,
	}
}

func (c *captureBody) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if n > 0 {
		c.append(string(p[:n]))
	}
	if err == io.EOF {
		c.finalize()
	}
	return n, err
}

func (c *captureBody) Close() error {
	err := c.rc.Close()
	c.finalize()
	return err
}

func (c *captureBody) append(chunk string) {
	if chunk == "" || c.s == nil || c.s.store == nil {
		return
	}
	maxLen := c.s.store.maxBodyBytes
	current := c.buf.Len()
	if current >= maxLen {
		c.truncated = true
		return
	}
	remain := maxLen - current
	if len(chunk) > remain {
		c.buf.WriteString(chunk[:remain])
		c.truncated = true
		return
	}
	c.buf.WriteString(chunk)
}

func (c *captureBody) finalize() {
	if c.finalized || c.s == nil || c.s.store == nil {
		return
	}
	c.finalized = true
	entry := Entry{
		ID:                c.s.id,
		CreatedAt:         c.s.createdAt,
		Label:             c.s.label,
		URL:               c.s.url,
		AccountID:         c.s.accountID,
		StatusCode:        c.statusCode,
		RequestBody:       c.s.requestRaw,
		ResponseBody:      c.buf.String(),
		ResponseTruncated: c.truncated,
	}
	c.s.store.push(entry)
}

func (s *Store) push(entry Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append([]Entry{entry}, s.items...)
	if len(s.items) > s.limit {
		s.items = s.items[:s.limit]
	}
	s.persistLocked(entry)
}

func (s *Store) persistLocked(entry Entry) {
	if !s.persistToDisk {
		return
	}
	label := sanitizeLabel(entry.Label)
	if label == "" {
		label = "captures"
	}
	dir := strings.TrimSpace(s.outputDir)
	if dir == "" {
		dir = defaultOutputDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, label+".jsonl")
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func sanitizeLabel(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func marshalPayload(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func truncateString(v string, maxLen int) (string, bool) {
	if maxLen <= 0 || len(v) <= maxLen {
		return v, false
	}
	return v[:maxLen], true
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseIntWithDefault(raw string, d int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return d
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return d
	}
	return n
}
