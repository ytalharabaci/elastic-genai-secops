package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	aliceToken = "alice-token-0123456789abcdef"
	bobToken   = "bob-token-0123456789abcdefgh"
)

func testHandler(t *testing.T, backend http.HandlerFunc) http.Handler {
	t.Helper()
	srv := httptest.NewServer(backend)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	analysts, err := ParseAnalysts("alice:" + aliceToken + ", bob:" + bobToken)
	if err != nil {
		t.Fatal(err)
	}
	return Handler(u, analysts, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func request(method, path, token, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestForwardsCaseRoutes(t *testing.T) {
	var gotPath, gotMethod string
	h := testHandler(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.RequestURI(), r.Method
		_, _ = io.WriteString(w, `{"ok":true}`)
	})

	cases := []struct{ method, path, wantPath string }{
		{"GET", "/api/cases?review_status=pending", "/cases?review_status=pending"},
		{"GET", "/api/cases/inc-1", "/cases/inc-1"},
		{"POST", "/api/cases/inc-1/review", "/cases/inc-1/review"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request(c.method, c.path, aliceToken, `{"status":"approved"}`))
		if rec.Code != http.StatusOK || gotPath != c.wantPath || gotMethod != c.method {
			t.Errorf("%s %s -> backend %s %s (status %d)", c.method, c.path, gotMethod, gotPath, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("DELETE", "/api/cases/inc-1", aliceToken, ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE should not be routed, got %d", rec.Code)
	}
}

func TestRequiresAnalystToken(t *testing.T) {
	called := false
	h := testHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{}`)
	})
	for name, token := range map[string]string{"none": "", "unknown": "x-0123456789abcdef0123456789", "prefix of a token": aliceToken[:20]} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request("GET", "/api/cases", token, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	r := request("GET", "/api/cases", "", "")
	r.Header.Set("Authorization", "Basic "+aliceToken)
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized || called {
		t.Errorf("non-bearer scheme: status %d, backend called %v", rec.Code, called)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request("GET", "/healthz", "", ""))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz must stay open, got %d", rec.Code)
	}
}

func TestReviewIsRecordedUnderTheAuthenticatedAnalyst(t *testing.T) {
	var got map[string]any
	h := testHandler(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{}`)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("POST", "/api/cases/inc-1/review", bobToken,
		`{"status":"rejected","analyst":"alice","notes":"benign"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got["analyst"] != "bob" || got["status"] != "rejected" || got["notes"] != "benign" {
		t.Errorf("backend got %v, want analyst bob and the other fields unchanged", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request("POST", "/api/cases/inc-1/review", bobToken, `["not", "an", "object"]`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("non-object review: status %d, want 400", rec.Code)
	}
}

func TestParseAnalysts(t *testing.T) {
	a, err := ParseAnalysts(" alice:" + aliceToken + ",,bob.smith@corp:" + bobToken)
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := a.Lookup(bobToken); !ok || name != "bob.smith@corp" {
		t.Errorf("Lookup = %q, %v", name, ok)
	}
	for spec, why := range map[string]string{
		"":                     "empty",
		"alice":                "no token",
		"alice:short":          "short token",
		"al ice:" + aliceToken: "space in name",
		"alice:" + aliceToken + ",alice:" + bobToken: "duplicate",
	} {
		if _, err := ParseAnalysts(spec); err == nil {
			t.Errorf("%s: expected an error", why)
		} else if strings.Contains(err.Error(), aliceToken) {
			t.Errorf("%s: error leaks the token: %v", why, err)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := testHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"cases":[]}`)
	})
	for _, path := range []string{"/healthz", "/api/cases"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request("GET", path, aliceToken, ""))
		for k, want := range map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"Cross-Origin-Resource-Policy": "same-origin",
			"Cache-Control":                "no-store",
		} {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("%s: %s = %q, want %q", path, k, got, want)
			}
		}
	}
}
