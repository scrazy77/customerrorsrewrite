# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Traefik plugin middleware that handles custom error responses by redirecting requests to alternative services when specific HTTP response codes are encountered. The primary use case is serving theme files from a fallback CDN when the primary S3 bucket returns a 403 error.

## Common Development Commands

### Build and Run
```bash
# Start Traefik with the plugin loaded (development mode)
docker-compose up

# Rebuild and restart if code changes are made
docker-compose down && docker-compose up --build
```

### Testing
Currently, there is no formal test suite. To test the plugin:
1. Run `docker-compose up`
2. Access the Traefik dashboard at http://localhost:9090/dashboard/
3. Make requests to test the middleware behavior

### Debugging
The plugin includes extensive logging. View logs with:
```bash
docker-compose logs -f
```

## Architecture Overview

### Core Components

1. **customerrorsrewrite.go**: Main plugin implementation
   - `Config` struct: Holds configuration (TargetService, MatchPattern, ReplaceRule, ResponseCode)
   - `CustomErrorsRewrite` struct: Main middleware handler that intercepts responses
   - Request flow: Intercept response → Check status code → Apply URL transformation → Redirect to fallback service

2. **Plugin Configuration** (via Traefik labels):
   - `responseCode`: HTTP status code that triggers the redirect (e.g., 403)
   - `targetService`: Fallback service URL
   - `matchPattern`: Regex to match URL paths
   - `replaceRule`: Regex replacement for URL transformation

### How the Plugin Works

1. Request comes through Traefik to the primary service (e.g., AWS S3)
2. Plugin intercepts the response using `httptest.ResponseRecorder`
3. If response code matches configured trigger:
   - Applies regex transformation to the URL path
   - Creates new HTTP request to fallback service
   - Returns the fallback response to client
4. Otherwise, returns the original response

### Example URL Transformation
- Original: `/data/shops/123/themes/456/style.css`
- Transformed: `/s/files/123/theme/456/style.css`
- This happens when S3 returns 403, redirecting to CDN fallback

## Important Implementation Details

- Uses only Go standard library (no external dependencies)
- Implements `http.Handler` interface
- Request body is read and buffered for potential reuse
- Response headers from fallback service are copied to client
- Extensive debug logging throughout the code

## Configuration Files

- **docker-compose.yml**: Defines the Traefik service with plugin configuration
- **traefik.yaml**: Traefik static configuration
- **services.yml**: Dynamic file provider configuration for services
- **.env**: Environment variables (PLUGIN_NAME, PLUGIN_MODULE)