package customerrorsrewrite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// BenchmarkServeHTTP benchmarks the basic ServeHTTP operation
func BenchmarkServeHTTP(b *testing.B) {
	// Create a simple target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("fallback content"))
	}))
	defer targetServer.Close()

	// Create backend that returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("forbidden"))
	})

	config := &Config{
		TargetService: targetServer.URL,
		MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
		ReplaceRule:   "/s/files/$1/theme/$2$3",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "benchmark")
	if err != nil {
		b.Fatalf("failed to create middleware: %v", err)
	}

	req := httptest.NewRequest("GET", "/data/shops/123/themes/456/style.css", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
	}
}

// BenchmarkServeHTTPNoRedirect benchmarks when no redirect occurs
func BenchmarkServeHTTPNoRedirect(b *testing.B) {
	// Create backend that returns 200 (no redirect)
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	config := &Config{
		TargetService: "http://example.com",
		MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
		ReplaceRule:   "/s/files/$1/theme/$2$3",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "benchmark")
	if err != nil {
		b.Fatalf("failed to create middleware: %v", err)
	}

	req := httptest.NewRequest("GET", "/data/shops/123/themes/456/style.css", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
	}
}

// BenchmarkRegexMatching benchmarks the regex pattern matching
func BenchmarkRegexMatching(b *testing.B) {
	config := &Config{
		TargetService: "http://example.com",
		MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
		ReplaceRule:   "/s/files/$1/theme/$2$3",
		ResponseCode:  403,
	}

	regex := regexp.MustCompile(config.MatchPattern)
	testPath := "/data/shops/123/themes/456/assets/images/logo.png"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if regex.MatchString(testPath) {
			_ = regex.ReplaceAllString(testPath, config.ReplaceRule)
		}
	}
}

// BenchmarkLargeRequestBody benchmarks handling of large request bodies
func BenchmarkLargeRequestBody(b *testing.B) {
	// Create target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer targetServer.Close()

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(403)
	})

	config := &Config{
		TargetService: targetServer.URL,
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/$1",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "benchmark")
	if err != nil {
		b.Fatalf("failed to create middleware: %v", err)
	}

	// Create 1MB request body
	largeBody := strings.Repeat("x", 1024*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/upload", strings.NewReader(largeBody))
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)
	}
}

// BenchmarkConcurrentRequests benchmarks concurrent request handling
func BenchmarkConcurrentRequests(b *testing.B) {
	// Create target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer targetServer.Close()

	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	})

	config := &Config{
		TargetService: targetServer.URL,
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/$1",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "benchmark")
	if err != nil {
		b.Fatalf("failed to create middleware: %v", err)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)
		}
	})
}

// BenchmarkDifferentURLPatterns benchmarks various URL pattern complexities
func BenchmarkDifferentURLPatterns(b *testing.B) {
	patterns := []struct {
		name    string
		pattern string
		rule    string
		path    string
	}{
		{
			name:    "simple",
			pattern: "/(.*)",
			rule:    "/fallback/$1",
			path:    "/test",
		},
		{
			name:    "moderate",
			pattern: "/api/v([0-9]+)/(.*)",
			rule:    "/legacy/v$1/$2",
			path:    "/api/v2/users/123",
		},
		{
			name:    "complex",
			pattern: "/data/shops/([0-9]+)/themes/([0-9]+)/assets/([^/]+)/(.*)",
			rule:    "/cdn/$1/$2/$3/$4",
			path:    "/data/shops/123/themes/456/assets/images/logo.png",
		},
	}

	for _, p := range patterns {
		b.Run(p.name, func(b *testing.B) {
			backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
			})

			config := &Config{
				TargetService: "http://example.com",
				MatchPattern:  p.pattern,
				ReplaceRule:   p.rule,
				ResponseCode:  403,
			}

			middleware, err := New(context.Background(), backend, config, "benchmark")
			if err != nil {
				b.Fatalf("failed to create middleware: %v", err)
			}

			req := httptest.NewRequest("GET", p.path, nil)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				middleware.ServeHTTP(rec, req)
			}
		})
	}
}