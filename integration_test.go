package customerrorsrewrite

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestIntegrationFullScenario tests a complete scenario with multiple services
func TestIntegrationFullScenario(t *testing.T) {
	// Create a CDN server that serves theme files
	cdnServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/s/files/123/theme/456/style.css":
			w.Header().Set("Content-Type", "text/css")
			w.WriteHeader(200)
			w.Write([]byte("/* CDN CSS content */\nbody { background: blue; }"))
		case "/s/files/123/theme/456/script.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.WriteHeader(200)
			w.Write([]byte("// CDN JS content\nconsole.log('from CDN');"))
		default:
			w.WriteHeader(404)
			w.Write([]byte("CDN: Not Found"))
		}
	}))
	defer cdnServer.Close()

	// Create S3-like server that returns 403 for certain paths
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate S3 behavior - return 403 for theme files
		if strings.Contains(r.URL.Path, "/themes/") {
			w.WriteHeader(403)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`))
			return
		}
		// Return 200 for other files
		w.WriteHeader(200)
		w.Write([]byte("S3 content"))
	}))
	defer s3Server.Close()

	// Configure middleware
	config := &Config{
		TargetService: cdnServer.URL,
		MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
		ReplaceRule:   "/s/files/$1/theme/$2$3",
		ResponseCode:  403,
	}

	// Create middleware wrapping S3 backend
	s3Handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Forward to S3 server
		resp, err := http.Get(s3Server.URL + r.URL.Path)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		defer resp.Body.Close()

		// Copy headers
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		
		// Copy status and body
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	middleware, err := New(context.Background(), s3Handler, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Test cases
	tests := []struct {
		path           string
		expectedStatus int
		expectedBody   string
		description    string
	}{
		{
			path:           "/data/shops/123/themes/456/style.css",
			expectedStatus: 200,
			expectedBody:   "/* CDN CSS content */\nbody { background: blue; }",
			description:    "Theme CSS should redirect to CDN",
		},
		{
			path:           "/data/shops/123/themes/456/script.js",
			expectedStatus: 200,
			expectedBody:   "// CDN JS content\nconsole.log('from CDN');",
			description:    "Theme JS should redirect to CDN",
		},
		{
			path:           "/data/shops/123/products/image.jpg",
			expectedStatus: 200,
			expectedBody:   "S3 content",
			description:    "Non-theme files should be served from S3",
		},
		{
			path:           "/data/shops/123/themes/456/nonexistent.txt",
			expectedStatus: 404,
			expectedBody:   "CDN: Not Found",
			description:    "Non-existent theme files should return CDN 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			body := rec.Body.String()
			if body != tt.expectedBody {
				t.Errorf("expected body:\n%q\ngot:\n%q", tt.expectedBody, body)
			}
		})
	}
}

// TestIntegrationConcurrentRequests tests the middleware under concurrent load
func TestIntegrationConcurrentRequests(t *testing.T) {
	requestCount := 0
	// Create target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(10 * time.Millisecond) // Simulate some processing time
		w.WriteHeader(200)
		w.Write([]byte(fmt.Sprintf("response-%d", requestCount)))
	}))
	defer targetServer.Close()

	// Create backend that always returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	})

	config := &Config{
		TargetService: targetServer.URL,
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/$1",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Run concurrent requests
	concurrency := 10
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			req := httptest.NewRequest("GET", fmt.Sprintf("/test-%d", id), nil)
			rec := httptest.NewRecorder()
			
			middleware.ServeHTTP(rec, req)
			
			if rec.Code != 200 {
				t.Errorf("request %d: expected status 200, got %d", id, rec.Code)
			}
			
			done <- true
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}

	if requestCount != concurrency {
		t.Errorf("expected %d requests to target server, got %d", concurrency, requestCount)
	}
}

// TestIntegrationLargePayload tests handling of large request/response bodies
func TestIntegrationLargePayload(t *testing.T) {
	// Create target server that echoes large payloads
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		
		// Echo back the body with a prefix
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		w.Write([]byte("ECHO: "))
		w.Write(body)
	}))
	defer targetServer.Close()

	// Create backend that returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the body
		io.ReadAll(r.Body)
		w.WriteHeader(403)
	})

	config := &Config{
		TargetService: targetServer.URL,
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/$1",
		ResponseCode:  403,
	}

	middleware, err := New(context.Background(), backend, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Create large payload (1MB)
	largePayload := strings.Repeat("Hello World! ", 100000)
	
	req := httptest.NewRequest("POST", "/upload", strings.NewReader(largePayload))
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	expectedResponse := "ECHO: " + largePayload
	if rec.Body.String() != expectedResponse {
		t.Errorf("response body mismatch, got %d bytes, expected %d bytes", 
			len(rec.Body.String()), len(expectedResponse))
	}
}

// TestIntegrationVariousStatusCodes tests different response code configurations
func TestIntegrationVariousStatusCodes(t *testing.T) {
	// Create target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("fallback response"))
	}))
	defer targetServer.Close()

	testCases := []struct {
		name           string
		configCode     int
		backendCode    int
		shouldRedirect bool
	}{
		{"403 forbidden", 403, 403, true},
		{"404 not found", 404, 404, true},
		{"500 server error", 500, 500, true},
		{"401 unauthorized", 401, 401, true},
		{"503 service unavailable", 503, 503, true},
		{"200 ok - no redirect", 403, 200, false},
		{"different codes - no redirect", 403, 404, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create backend with specific status code
			backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.backendCode)
				w.Write([]byte("backend response"))
			})

			config := &Config{
				TargetService: targetServer.URL,
				MatchPattern:  "/(.*)",
				ReplaceRule:   "/$1",
				ResponseCode:  tc.configCode,
			}

			middleware, err := New(context.Background(), backend, config, "test")
			if err != nil {
				t.Fatalf("failed to create middleware: %v", err)
			}

			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			expectedBody := "backend response"
			expectedStatus := tc.backendCode
			if tc.shouldRedirect {
				expectedBody = "fallback response"
				expectedStatus = 200
			}

			if rec.Code != expectedStatus {
				t.Errorf("expected status %d, got %d", expectedStatus, rec.Code)
			}

			if rec.Body.String() != expectedBody {
				t.Errorf("expected body %q, got %q", expectedBody, rec.Body.String())
			}
		})
	}
}

// TestIntegrationComplexURLPatterns tests various URL pattern transformations
func TestIntegrationComplexURLPatterns(t *testing.T) {
	// Create target server that reports the received path
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("Path received: " + r.URL.Path))
	}))
	defer targetServer.Close()

	testCases := []struct {
		name         string
		matchPattern string
		replaceRule  string
		inputPath    string
		expectedPath string
	}{
		{
			name:         "shop theme transformation",
			matchPattern: "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
			replaceRule:  "/s/files/$1/theme/$2$3",
			inputPath:    "/data/shops/123/themes/456/assets/style.css",
			expectedPath: "/s/files/123/theme/456/assets/style.css",
		},
		{
			name:         "API versioning",
			matchPattern: "/api/v([0-9]+)/(.*)",
			replaceRule:  "/legacy/api/v$1/$2",
			inputPath:    "/api/v1/users/123",
			expectedPath: "/legacy/api/v1/users/123",
		},
		{
			name:         "file extension replacement",
			matchPattern: "(.*)\\.php$",
			replaceRule:  "$1.html",
			inputPath:    "/pages/about.php",
			expectedPath: "/pages/about.html",
		},
		{
			name:         "domain to path",
			matchPattern: "/tenant/([^/]+)/(.*)",
			replaceRule:  "/$1-files/$2",
			inputPath:    "/tenant/customer1/documents/report.pdf",
			expectedPath: "/customer1-files/documents/report.pdf",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
			})

			config := &Config{
				TargetService: targetServer.URL,
				MatchPattern:  tc.matchPattern,
				ReplaceRule:   tc.replaceRule,
				ResponseCode:  403,
			}

			middleware, err := New(context.Background(), backend, config, "test")
			if err != nil {
				t.Fatalf("failed to create middleware: %v", err)
			}

			req := httptest.NewRequest("GET", tc.inputPath, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			expectedBody := "Path received: " + tc.expectedPath
			if rec.Body.String() != expectedBody {
				t.Errorf("expected transformed path %q, got %q", expectedBody, rec.Body.String())
			}
		})
	}
}