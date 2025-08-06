package customerrorsrewrite

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	TargetService string `json:"targetService" yaml:"targetService" toml:"targetService"`
	MatchPattern  string `json:"matchPattern" yaml:"matchPattern" toml:"matchPattern"`
	ReplaceRule   string `json:"replaceRule" yaml:"replaceRule" toml:"replaceRule"`
	ResponseCode  int    `json:"responseCode" yaml:"responseCode" toml:"responseCode"`
}
type CustomErrorsRewrite struct {
	name   string
	config *Config
	next   http.Handler
}

func CreateConfig() *Config {
	return &Config{
		TargetService: "",
		MatchPattern:  "",
		ReplaceRule:   "",
		ResponseCode:  0,
	}
}

func (c *CustomErrorsRewrite) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Buffer the request body as it can only be read once
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	}

	// Process the request through the next handler.
	respRecorder := httptest.NewRecorder()
	log.Println("Original URL:" + req.URL.Path)
	c.next.ServeHTTP(respRecorder, req)
	log.Println("Response code:" + strconv.Itoa(c.config.ResponseCode))
	// Check if the actual response code matches the configured responseCode.
	if respRecorder.Code == c.config.ResponseCode {
		log.Println("Response code matched!")
		// If the response code matches, redirect to TargetService.
		log.Println("Original URL2:" + req.URL.Path)
		if len(c.config.MatchPattern) > 0 {
			// Check if the URL matches the pattern
			regex := regexp.MustCompile(c.config.MatchPattern)
			if !regex.MatchString(req.URL.Path) {
				// If it doesn't match, serve the original response
				for key, values := range respRecorder.Result().Header {
					for _, value := range values {
						if !strings.EqualFold(key, "content-length") {
							rw.Header().Set(key, value)
						}
					}
				}
				rw.WriteHeader(respRecorder.Result().StatusCode)
				io.Copy(rw, respRecorder.Result().Body)
				return
			}
			// Rewrite path based on regex rule.
			log.Println("MatchPattern:" + c.config.MatchPattern)
			log.Println("ReplaceRule:" + c.config.ReplaceRule)
			newPath := regex.ReplaceAllString(req.URL.Path, c.config.ReplaceRule)
			log.Println("New path:" + newPath)
			req.URL.Path = newPath
			newHost := strings.TrimPrefix(c.config.TargetService, "https://")
			newHost = strings.TrimPrefix(newHost, "http://")
			req.URL.Host = newHost
			log.Println("New Host:" + newHost)
			log.Println("Target Service:" + c.config.TargetService)
			targetURL := c.config.TargetService + newPath // use newPath
			log.Println("targetURL:" + targetURL)

			// make new http.NewRequest with buffered body
			newReq, err := http.NewRequest(req.Method, targetURL, io.NopCloser(strings.NewReader(string(bodyBytes))))
			if err != nil {
				log.Printf("Error creating new request: %v\n", err)
				rw.WriteHeader(500)
				rw.Write([]byte("Internal Server Error"))
				return
			}

			// copy request Header to new request
			newReq.Header = make(http.Header)
			for key, values := range req.Header {
				for _, value := range values {
					newReq.Header.Add(key, value)
				}
			}

			// http.Client do request
			client := &http.Client{}
			resp, err := client.Do(newReq)
			if err == nil {
				defer resp.Body.Close()

				// copy response header value
				for key, values := range resp.Header {
					for _, value := range values {
						rw.Header().Set(key, value)
					}
				}

				// serve request
				rw.WriteHeader(resp.StatusCode)
				io.Copy(rw, resp.Body)
				return
			} else {
				log.Printf("Error fetching target service: %v\n", err)
				rw.WriteHeader(500)
				rw.Write([]byte("Internal Server Error"))
				return
			}
		}
	}
	// copy response header value
	for key, values := range respRecorder.Result().Header {
		for _, value := range values {
			if !strings.EqualFold(key, "content-length") {
				rw.Header().Set(key, value)
			}
		}
	}
	// serve request
	rw.WriteHeader(respRecorder.Result().StatusCode)
	io.Copy(rw, respRecorder.Result().Body)
	return
}

func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {

	if len(config.TargetService) == 0 {
		return nil, fmt.Errorf("TargetService is required")
	}
	if len(config.MatchPattern) > 0 {
		if len(config.ReplaceRule) == 0 {
			return nil, fmt.Errorf("ReplaceRule is required")
		}
	}
	// Validate regex pattern
	if len(config.MatchPattern) > 0 {
		_, err := regexp.Compile(config.MatchPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid MatchPattern regex: %v", err)
		}
	}

	if config.ResponseCode == 0 {
		return nil, fmt.Errorf("ResponseCode must be non-zero")
	}

	return &CustomErrorsRewrite{
		name:   name,
		config: config,
		next:   next,
	}, nil
}
