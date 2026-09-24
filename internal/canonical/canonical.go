package canonical

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/jxskiss/base62"
)

// DefaultTrackingPrefixes are query parameter prefixes usually stripped for canonicalization.
var DefaultTrackingPrefixes = []string{
	"utm_",
	"fbclid",
	"gclid",
	"msclkid",
	"mc_eid",
	"_hsenc",
	"_hsmi",
	"igshid",
	"ref",
	"source",
}

// CanonicalizeURL produces a deterministic, standardized representation of a URL.
func CanonicalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("url must include scheme and host")
	}

	// Lowercase scheme and host
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Remove fragment
	u.Fragment = ""

	// Remove default ports
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = u.Hostname()
	}

	// Clean path
	cleanedPath := path.Clean(u.Path)
	if !strings.HasPrefix(cleanedPath, "/") {
		cleanedPath = "/" + cleanedPath
	}
	// Preserve trailing slash only if root
	if cleanedPath != "/" && strings.HasSuffix(u.Path, "/") && !strings.HasSuffix(cleanedPath, "/") {
		// in path.Clean, trailing slashes are removed.
	}
	u.Path = cleanedPath

	// Filter and sort query parameters
	q := u.Query()
	for key := range q {
		lowerKey := strings.ToLower(key)
		for _, prefix := range DefaultTrackingPrefixes {
			if strings.HasPrefix(lowerKey, prefix) {
				q.Del(key)
				break
			}
		}
	}

	if len(q) > 0 {
		// Sort query keys deterministically
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var queryParts []string
		for _, k := range keys {
			vals := q[k]
			sort.Strings(vals)
			for _, v := range vals {
				queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
			}
		}
		u.RawQuery = strings.Join(queryParts, "&")
	} else {
		u.RawQuery = ""
	}

	return u.String(), nil
}

// GenerateID produces a deterministic 8-11 character Base62 ID from a canonical URL.
// It takes the first 8 bytes of the SHA-256 hash (64-bit uint) and encodes in Base62.
func GenerateID(canonicalURL string) string {
	hasher := sha256.New()
	hasher.Write([]byte(canonicalURL))
	sum := hasher.Sum(nil)

	// Take first 8 bytes (uint64)
	num := binary.BigEndian.Uint64(sum[:8])
	return string(base62.FormatUint(num))
}
