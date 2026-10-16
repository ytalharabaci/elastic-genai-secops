// Package api is the analyst-facing API. It holds no business logic: case reads and reviews
// are forwarded to case-service.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Handler routes:
//
//	GET  /healthz
//	GET  /api/cases[?review_status=pending]  -> case-service GET /cases
//	GET  /api/cases/{id}                     -> case-service GET /cases/{id}
//	POST /api/cases/{id}/review              -> case-service POST /cases/{id}/review
//
// /api requires "Authorization: Bearer <analyst token>"; a review is recorded under the analyst
// the token belongs to (ADR-028).
func Handler(caseService *url.URL, analysts *Analysts, log *slog.Logger) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(caseService)
	forward := func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		r.Host = caseService.Host
		proxy.ServeHTTP(w, r)
	}

	review := func(w http.ResponseWriter, r *http.Request) {
		if err := stampReviewer(r); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		forward(w, r)
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/cases", forward)
	api.HandleFunc("GET /api/cases/{id}", forward)
	api.HandleFunc("POST /api/cases/{id}/review", review)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/api/", requireAnalyst(analysts, log, api))
	return securityHeaders(mux)
}

// securityHeaders sets response headers for a JSON API that browsers must not sniff, frame or
// share cross-origin (found by an OWASP ZAP API scan).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
