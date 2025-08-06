package customerrorsrewrite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	if config == nil {
		t.Fatal("CreateConfig returned nil")
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		shouldError bool
	}{
		{
			name: "valid config",
			config: &Config{
				TargetService: "http://example.com",
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
			shouldError: false,
		},
		{
			name: "missing target service",
			config: &Config{
				MatchPattern: "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:  "/s/files/$1/theme/$2$3",
				ResponseCode: 403,
			},
			shouldError: true,
		},
		{
			name: "missing match pattern",
			config: &Config{
				TargetService: "http://example.com",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
			shouldError: false, // Missing match pattern is allowed when replace rule exists
		},
		{
			name: "missing replace rule",
			config: &Config{
				TargetService: "http://example.com",
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ResponseCode:  403,
			},
			shouldError: true,
		},
		{
			name: "zero response code",
			config: &Config{
				TargetService: "http://example.com",
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  0,
			},
			shouldError: true,
		},
		{
			name: "invalid regex pattern",
			config: &Config{
				TargetService: "http://example.com",
				MatchPattern:  "[invalid regex",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			})

			_, err := New(context.Background(), handler, tt.config, "test")
			if tt.shouldError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServeHTTP(t *testing.T) {
	tests := []struct {
		name               string
		requestPath        string
		backendStatus      int
		targetResponse     string
		targetStatus       int
		expectedBody       string
		expectedStatus     int
		config             *Config
	}{
		{
			name:          "response code matches - should redirect",
			requestPath:   "/data/shops/123/themes/456/style.css",
			backendStatus: 403,
			targetResponse: "fallback content",
			targetStatus:   200,
			expectedBody:   "fallback content",
			expectedStatus: 200,
			config: &Config{
				TargetService: "", // will be set to test server URL
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
		},
		{
			name:           "response code doesn't match - should pass through",
			requestPath:    "/data/shops/123/themes/456/style.css",
			backendStatus:  404,
			targetResponse: "fallback content",
			targetStatus:  200,
			expectedBody:   "original content",
			expectedStatus: 404,
			config: &Config{
				TargetService: "", // will be set to test server URL
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
		},
		{
			name:           "URL doesn't match pattern - should pass through even with matching status",
			requestPath:    "/other/path",
			backendStatus:  403,
			targetResponse: "fallback content",
			targetStatus:  200,
			expectedBody:   "original content",
			expectedStatus: 403,
			config: &Config{
				TargetService: "", // will be set to test server URL
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
		},
		{
			name:          "complex URL transformation",
			requestPath:   "/data/shops/999/themes/888/assets/images/logo.png",
			backendStatus: 403,
			targetResponse: "logo image data",
			targetStatus:   200,
			expectedBody:   "logo image data",
			expectedStatus: 200,
			config: &Config{
				TargetService: "", // will be set to test server URL
				MatchPattern:  "/data/shops/([0-9]+)/themes/([0-9]+)(.*)",
				ReplaceRule:   "/s/files/$1/theme/$2$3",
				ResponseCode:  403,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create target server
			targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the transformed path
				if tt.config.ResponseCode == tt.backendStatus {
					expectedPath := "/s/files/123/theme/456/style.css"
					if tt.requestPath == "/data/shops/999/themes/888/assets/images/logo.png" {
						expectedPath = "/s/files/999/theme/888/assets/images/logo.png"
					}
					if tt.requestPath == "/data/shops/123/themes/456/style.css" && r.URL.Path != expectedPath {
						t.Errorf("expected transformed path %s, got %s", expectedPath, r.URL.Path)
					}
				}
				w.WriteHeader(tt.targetStatus)
				w.Write([]byte(tt.targetResponse))
			}))
			defer targetServer.Close()

			// Update config with target server URL
			tt.config.TargetService = targetServer.URL

			// Create backend handler
			backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.backendStatus)
				w.Write([]byte("original content"))
			})

			// Create middleware
			middleware, err := New(context.Background(), backend, tt.config, "test")
			if err != nil {
				t.Fatalf("failed to create middleware: %v", err)
			}

			// Create request
			req := httptest.NewRequest("GET", tt.requestPath, nil)
			rec := httptest.NewRecorder()

			// Execute
			middleware.ServeHTTP(rec, req)

			// Verify response
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			body := rec.Body.String()
			if body != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, body)
			}
		})
	}
}

func TestServeHTTPWithRequestBody(t *testing.T) {
	config := &Config{
		TargetService: "", // will be set to test server URL
		MatchPattern:  "/api/(.*)",
		ReplaceRule:   "/fallback/$1",
		ResponseCode:  403,
	}

	// Create target server that echoes the request body
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(200)
		w.Write([]byte("received: " + string(body)))
	}))
	defer targetServer.Close()

	config.TargetService = targetServer.URL

	// Create backend that returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body to simulate real handler
		io.ReadAll(r.Body)
		w.WriteHeader(403)
		w.Write([]byte("forbidden"))
	})

	// Create middleware
	middleware, err := New(context.Background(), backend, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Create request with body
	requestBody := "test request body"
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(requestBody))
	rec := httptest.NewRecorder()

	// Execute
	middleware.ServeHTTP(rec, req)

	// Verify the target server received the body
	expectedResponse := "received: " + requestBody
	if rec.Body.String() != expectedResponse {
		t.Errorf("expected body %q, got %q", expectedResponse, rec.Body.String())
	}
}

func TestServeHTTPHeaders(t *testing.T) {
	config := &Config{
		TargetService: "", // will be set to test server URL
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/fallback/$1",
		ResponseCode:  403,
	}

	// Create target server that returns custom headers
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers are forwarded
		if r.Header.Get("X-Custom-Header") != "test-value" {
			t.Errorf("expected X-Custom-Header to be forwarded")
		}
		
		// Set response headers
		w.Header().Set("X-Target-Response", "from-target")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer targetServer.Close()

	config.TargetService = targetServer.URL

	// Create backend that returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Response", "from-backend")
		w.WriteHeader(403)
	})

	// Create middleware
	middleware, err := New(context.Background(), backend, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Create request with custom header
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Custom-Header", "test-value")
	rec := httptest.NewRecorder()

	// Execute
	middleware.ServeHTTP(rec, req)

	// Verify response headers from target are present
	if rec.Header().Get("X-Target-Response") != "from-target" {
		t.Errorf("expected X-Target-Response header from target")
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header from target")
	}
	// Backend headers should not be present when redirected
	if rec.Header().Get("X-Backend-Response") != "" {
		t.Errorf("backend headers should not be present when redirected")
	}
}

func TestServeHTTPTargetServerError(t *testing.T) {
	config := &Config{
		TargetService: "http://localhost:99999", // Invalid target
		MatchPattern:  "/(.*)",
		ReplaceRule:   "/$1",
		ResponseCode:  403,
	}

	// Create backend that returns 403
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("forbidden"))
	})

	// Create middleware
	middleware, err := New(context.Background(), backend, config, "test")
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	// Create request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// Execute
	middleware.ServeHTTP(rec, req)

	// Should return 500 when target server fails
	if rec.Code != 500 {
		t.Errorf("expected status 500 for target server error, got %d", rec.Code)
	}
}