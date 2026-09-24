package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"link-proxy/internal/canonical"
	"link-proxy/internal/database"

	"github.com/redis/go-redis/v9"
)

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024) // 64KB buffer
		return &b
	},
}

type ProxyService struct {
	db          *database.DB
	rdb         *redis.Client // optional; nil if disabled/unreachable
	client      *http.Client
	accessCh    chan accessEvent
	baseURL     string
	smallLimit  int64 // bytes; e.g. 2MB
}

type accessEvent struct {
	id          string
	bytesServed int64
}

type RegisterResponse struct {
	ID          string `json:"id"`
	ProxyURL    string `json:"proxy_url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

func NewProxyService(db *database.DB, redisAddr string, baseURL string, smallLimit int64) *ProxyService {
	var rdb *redis.Client
	if redisAddr != "" {
		client := redis.NewClient(&redis.Options{
			Addr:        redisAddr,
			DialTimeout: 1 * time.Second,
		})
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := client.Ping(ctx).Err(); err == nil {
			rdb = client
		}
	}

	p := &ProxyService{
		db:         db,
		rdb:        rdb,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		smallLimit: smallLimit,
		accessCh:   make(chan accessEvent, 2048),
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				return ValidateSSRF(req.URL)
			},
		},
	}

	// Background worker for recording access stats without blocking client stream
	go p.processAccessEvents()
	return p
}

func (p *ProxyService) processAccessEvents() {
	for ev := range p.accessCh {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = p.db.RecordAccess(ctx, ev.id, ev.bytesServed)
		if p.rdb != nil {
			_ = p.rdb.ZIncrBy(ctx, "proxy:stats:access_count", 1, ev.id).Err()
		}
		cancel()
	}
}

// ValidateSSRF protects against internal network reconnaissance.
func ValidateSSRF(targetURL *url.URL) error {
	scheme := strings.ToLower(targetURL.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", scheme)
	}

	hostname := targetURL.Hostname()
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", hostname, err)
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
			return fmt.Errorf("prohibited destination IP: %s (private or loopback)", ip.String())
		}
	}
	return nil
}

// RegisterURL validates, canonicalizes, probes upstream, and persists the mapping.
func (p *ProxyService) RegisterURL(ctx context.Context, rawURL string) (*RegisterResponse, error) {
	canonicalURL, err := canonical.CanonicalizeURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	parsed, err := url.Parse(canonicalURL)
	if err != nil {
		return nil, fmt.Errorf("parse canonical url: %w", err)
	}

	if err := ValidateSSRF(parsed); err != nil {
		return nil, fmt.Errorf("ssrf check failed: %w", err)
	}

	id := canonical.GenerateID(canonicalURL)

	// Check DB first for deduplication
	if existing, err := p.db.GetFile(ctx, id); err == nil && existing != nil {
		return &RegisterResponse{
			ID:          existing.ID,
			ProxyURL:    fmt.Sprintf("%s/p/%s", p.baseURL, existing.ID),
			Filename:    existing.Filename,
			ContentType: existing.ContentType,
			Size:        existing.FileSize,
		}, nil
	}

	// Probe upstream for metadata
	filename, contentType, size, err := p.probeUpstream(ctx, canonicalURL)
	if err != nil {
		return nil, fmt.Errorf("upstream probe failed: %w", err)
	}

	rec := &database.FileRecord{
		ID:          id,
		OriginalURL: canonicalURL,
		Filename:    filename,
		ContentType: contentType,
		FileSize:    size,
	}

	if err := p.db.UpsertFile(ctx, rec); err != nil {
		return nil, fmt.Errorf("database upsert: %w", err)
	}

	// Cache metadata in Redis if enabled
	if p.rdb != nil {
		data, _ := json.Marshal(rec)
		_ = p.rdb.Set(ctx, "proxy:id:"+id, data, 24*time.Hour).Err()
	}

	return &RegisterResponse{
		ID:          id,
		ProxyURL:    fmt.Sprintf("%s/p/%s", p.baseURL, id),
		Filename:    filename,
		ContentType: contentType,
		Size:        size,
	}, nil
}

func (p *ProxyService) probeUpstream(ctx context.Context, targetURL string) (string, string, int64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, targetURL, nil)
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("User-Agent", "LinkProxy/1.0")

	resp, err := p.client.Do(req)
	// Fallback to GET range 0-0 if HEAD fails or method not allowed
	if err != nil || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode >= 400 {
		if resp != nil {
			resp.Body.Close()
		}
		getReq, err := http.NewRequestWithContext(probeCtx, http.MethodGet, targetURL, nil)
		if err != nil {
			return "", "", 0, err
		}
		getReq.Header.Set("User-Agent", "LinkProxy/1.0")
		getReq.Header.Set("Range", "bytes=0-0")
		resp, err = p.client.Do(getReq)
		if err != nil {
			return "", "", 0, err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", 0, fmt.Errorf("upstream responded with status: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Resolve filename
	filename := extractFilename(resp.Header.Get("Content-Disposition"), targetURL)

	// Resolve size
	size := resp.ContentLength
	if size <= 0 {
		// check Content-Range header: bytes 0-0/12345
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			parts := strings.Split(cr, "/")
			if len(parts) == 2 {
				if total, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					size = total
				}
			}
		}
	}
	if size < 0 {
		size = 0
	}

	return filename, contentType, size, nil
}

func extractFilename(disposition, rawURL string) string {
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				return path.Base(fn)
			}
		}
	}

	u, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(u.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}

	return "download.bin"
}

// ServeStream handles GET /p/{id}
func (p *ProxyService) ServeStream(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var rec *database.FileRecord

	// Check Redis metadata cache
	if p.rdb != nil {
		val, err := p.rdb.Get(ctx, "proxy:id:"+id).Bytes()
		if err == nil {
			var cached database.FileRecord
			if json.Unmarshal(val, &cached) == nil {
				rec = &cached
			}
		}
	}

	// Fallback to SQLite
	if rec == nil {
		var err error
		rec, err = p.db.GetFile(ctx, id)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		if p.rdb != nil {
			data, _ := json.Marshal(rec)
			_ = p.rdb.Set(ctx, "proxy:id:"+id, data, 24*time.Hour).Err()
		}
	}

	// Check small file blob cache in Redis
	if p.rdb != nil && rec.FileSize > 0 && rec.FileSize <= p.smallLimit {
		blob, err := p.rdb.Get(ctx, "proxy:blob:"+id).Bytes()
		if err == nil {
			w.Header().Set("Content-Type", rec.ContentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, rec.Filename))
			w.Header().Set("X-Cache", "HIT-REDIS-BLOB")
			w.Write(blob)
			p.accessCh <- accessEvent{id: id, bytesServed: int64(len(blob))}
			return
		}
	}

	// Prepare upstream request with client Range forwarding
	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rec.OriginalURL, nil)
	if err != nil {
		http.Error(w, "Failed to create upstream request", http.StatusInternalServerError)
		return
	}

	upstreamReq.Header.Set("User-Agent", "LinkProxy/1.0")
	if clientRange := r.Header.Get("Range"); clientRange != "" {
		upstreamReq.Header.Set("Range", clientRange)
	}

	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Upstream gateway error: %v", err), http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	// Forward upstream headers
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := upstreamResp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, rec.Filename))
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(upstreamResp.StatusCode)

	bufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufPtr)

	// Stream response
	bytesWritten, _ := io.CopyBuffer(w, upstreamResp.Body, *bufPtr)

	// Small file payload populate
	if p.rdb != nil && upstreamResp.StatusCode == http.StatusOK && bytesWritten > 0 && bytesWritten <= p.smallLimit && r.Header.Get("Range") == "" {
		// ponytail: small payload cache write directly from upstream response isn't needed if already streamed; cache on next fetch or skip
	}

	// Asynchronously record access log
	select {
	case p.accessCh <- accessEvent{id: id, bytesServed: bytesWritten}:
	default:
	}
}
