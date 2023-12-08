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

	// Process the request through the next handler.
	respRecorder := httptest.NewRecorder()
	c.next.ServeHTTP(respRecorder, req)
	log.Println("Response code:" + strconv.Itoa(c.config.ResponseCode))
	// Check if the actual response code matches the configured responseCode.
	if respRecorder.Code == c.config.ResponseCode {
		log.Println("Response code matched!")
		// If the response code matches, redirect to TargetService.
		log.Println("Original URL:" + req.URL.Path)
		if len(c.config.MatchPattern) > 0 {
			// Rewrite path based on regex rule.
			log.Println("MatchPattern:" + c.config.MatchPattern)
			log.Println("ReplaceRule:" + c.config.ReplaceRule)
			newPath := regexp.MustCompile(c.config.MatchPattern).ReplaceAllString(req.URL.Path, c.config.ReplaceRule)
			req.URL.Path = newPath
			newHost := strings.TrimPrefix(c.config.TargetService, "https://")
			newHost = strings.TrimPrefix(newHost, "http://")
			req.URL.Host = newHost

			targetURL := c.config.TargetService + newPath // use newPath
			log.Println("targetURL:" + targetURL)

			// make new http.NewRequest
			newReq, err := http.NewRequest(req.Method, targetURL, req.Body)
			if err != nil {
				fmt.Printf("Error creating new request: %v\n", err)
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
				fmt.Printf("Error fetching target service: %v\n", err)
			}
		}
	}

	// If an error occurs or the response code doesn't match, serve the recorded response.
	respRecorder.Result().Write(rw)
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
	return &CustomErrorsRewrite{
		name:   name,
		config: config,
		next:   next,
	}, nil
}
