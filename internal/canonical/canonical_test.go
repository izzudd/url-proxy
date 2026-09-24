package canonical

import (
	"testing"
)

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "protocol and host uppercase",
			input:    "HTTPS://EXAMPLE.COM/File.pdf",
			expected: "https://example.com/File.pdf",
		},
		{
			name:     "default https port stripped",
			input:    "https://example.com:443/data.zip",
			expected: "https://example.com/data.zip",
		},
		{
			name:     "default http port stripped",
			input:    "http://example.com:80/data.zip",
			expected: "http://example.com/data.zip",
		},
		{
			name:     "custom port kept",
			input:    "http://example.com:8080/data.zip",
			expected: "http://example.com:8080/data.zip",
		},
		{
			name:     "fragment removed",
			input:    "https://example.com/file.pdf#section1",
			expected: "https://example.com/file.pdf",
		},
		{
			name:     "redundant slashes in path cleaned",
			input:    "https://example.com//path///sub//file.zip",
			expected: "https://example.com/path/sub/file.zip",
		},
		{
			name:     "query params sorted and tracking stripped",
			input:    "https://example.com/file.iso?utm_source=twitter&b=2&fbclid=xyz&a=1&gclid=123",
			expected: "https://example.com/file.iso?a=1&b=2",
		},
		{
			name:    "empty url",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing scheme",
			input:   "example.com/file.pdf",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeURL(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CanonicalizeURL() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.expected {
				t.Errorf("CanonicalizeURL() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestGenerateID_Deterministic(t *testing.T) {
	url1 := "https://example.com/data.pdf?b=2&a=1&utm_medium=email"
	url2 := "HTTPS://EXAMPLE.COM:443/data.pdf?utm_source=google&a=1&b=2#page=1"

	c1, err := CanonicalizeURL(url1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c2, err := CanonicalizeURL(url2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c1 != c2 {
		t.Fatalf("canonical URLs do not match: %q vs %q", c1, c2)
	}

	id1 := GenerateID(c1)
	id2 := GenerateID(c2)

	if id1 == "" {
		t.Fatal("generated ID is empty")
	}

	if id1 != id2 {
		t.Fatalf("expected identical IDs for equivalent URLs, got %q vs %q", id1, id2)
	}
}

func BenchmarkCanonicalizeURL(b *testing.B) {
	sampleURL := "https://EXAMPLE.com:443/files//archive.zip?utm_source=twitter&b=2&a=1#section"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = CanonicalizeURL(sampleURL)
	}
}

func BenchmarkGenerateID(b *testing.B) {
	canonical := "https://example.com/files/archive.zip?a=1&b=2"
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GenerateID(canonical)
	}
}
