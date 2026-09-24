package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"link-proxy/internal/database"
)

func TestProxyEndToEnd(t *testing.T) {
	// 1. Create a mock upstream server delivering mock file content
	mockData := "0123456789abcdefghijklmnopqrstuvwxyz"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="test-file.txt"`)
		w.Header().Set("Accept-Ranges", "bytes")

		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
			var start, end int
			fmt.Sscanf(strings.TrimPrefix(rangeHeader, "bytes="), "%d-%d", &start, &end)
			if end == 0 || end >= len(mockData) {
				end = len(mockData) - 1
			}
			part := mockData[start : end+1]
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(mockData)))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(part)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(part))
			return
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(mockData)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockData))
	}))
	defer upstream.Close()

	// 2. Setup SQLite DB
	dbPath := "test_proxy.db"
	defer os.Remove(dbPath)

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("database.Open failed: %v", err)
	}
	defer db.Close()

	proxySvc := NewProxyService(db, "", "http://localhost:8080", 2*1024*1024)

	// In test, httptest uses 127.0.0.1, so we temporarily override SSRF check or test probe directly
	// Let's test registering URL directly into DB and streaming it
	rec := &database.FileRecord{
		ID:          "test1234",
		OriginalURL: upstream.URL + "/data.txt",
		Filename:    "test-file.txt",
		ContentType: "text/plain",
		FileSize:    int64(len(mockData)),
	}
	if err := db.UpsertFile(context.Background(), rec); err != nil {
		t.Fatalf("UpsertFile failed: %v", err)
	}

	// 3. Test Full Stream
	req := httptest.NewRequest(http.MethodGet, "/p/test1234", nil)
	rr := httptest.NewRecorder()
	proxySvc.ServeStream(rr, req, "test1234")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != mockData {
		t.Fatalf("expected body %q, got %q", mockData, rr.Body.String())
	}
	if rr.Header().Get("Content-Type") != "text/plain" {
		t.Fatalf("expected Content-Type text/plain, got %q", rr.Header().Get("Content-Type"))
	}

	// 4. Test Range Request
	rangeReq := httptest.NewRequest(http.MethodGet, "/p/test1234", nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rangeRR := httptest.NewRecorder()
	proxySvc.ServeStream(rangeRR, rangeReq, "test1234")

	if rangeRR.Code != http.StatusPartialContent {
		t.Fatalf("expected status 206 Partial Content, got %d", rangeRR.Code)
	}
	if rangeRR.Body.String() != "0123456789" {
		t.Fatalf("expected range body '0123456789', got %q", rangeRR.Body.String())
	}

	// Allow async access logging to settle
	time.Sleep(100 * time.Millisecond)

	stats, err := db.GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalAccessCount < 2 {
		t.Fatalf("expected at least 2 accesses, got %d", stats.TotalAccessCount)
	}
}

type noopResponseWriter struct {
	headers http.Header
}

func (n *noopResponseWriter) Header() http.Header         { return n.headers }
func (n *noopResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noopResponseWriter) WriteHeader(statusCode int)  {}

func BenchmarkServeStream(b *testing.B) {
	// Create mock payload 1MB
	payload := make([]byte, 1024*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer upstream.Close()

	dbPath := "bench_proxy.db"
	defer os.Remove(dbPath)
	db, err := database.Open(dbPath)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	defer db.Close()

	proxySvc := NewProxyService(db, "", "http://localhost:8080", 2*1024*1024)
	rec := &database.FileRecord{
		ID:          "stream123",
		OriginalURL: upstream.URL + "/large.bin",
		Filename:    "large.bin",
		ContentType: "application/octet-stream",
		FileSize:    int64(len(payload)),
	}
	_ = db.UpsertFile(context.Background(), rec)

	req := httptest.NewRequest(http.MethodGet, "/p/stream123", nil)
	w := &noopResponseWriter{headers: make(http.Header)}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		proxySvc.ServeStream(w, req, "stream123")
	}
}
