package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
)

// MinTokenLen is the shortest accepted analyst token.
const MinTokenLen = 24

const maxReviewBody = 64 << 10

type analystKey struct{}

// Analysts maps bearer tokens to analyst names (ADR-028). Tokens are compared as SHA-256 digests
// in constant time, so a lookup does not reveal how much of a token matched.
type Analysts struct {
	digests [][sha256.Size]byte
	names   []string
}

// ParseAnalysts reads ANALYST_TOKENS: comma-separated "name:token" pairs.
func ParseAnalysts(spec string) (*Analysts, error) {
	a := &Analysts{}
	seen := map[string]bool{}
	for i, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, token, ok := strings.Cut(entry, ":")
		if !ok || !validName(name) {
			// The entry may be a mistyped token: name its position, never its text
			return nil, fmt.Errorf("ANALYST_TOKENS: entry %d must be name:token with a name of 1-64 letters, digits, . _ - @", i+1)
		}
		if len(token) < MinTokenLen {
			return nil, fmt.Errorf("ANALYST_TOKENS: token of %q is shorter than %d characters", name, MinTokenLen)
		}
		if seen[name] {
			return nil, fmt.Errorf("ANALYST_TOKENS: %q is listed twice", name)
		}
		seen[name] = true
		a.digests = append(a.digests, sha256.Sum256([]byte(token)))
		a.names = append(a.names, name)
	}
	if len(a.names) == 0 {
		return nil, errors.New("ANALYST_TOKENS is empty: set at least one name:token pair (init-env / init-secrets create one)")
	}
	return a, nil
}

// Lookup returns the analyst for a token, checking every entry so the time does not depend on
// which one matched.
func (a *Analysts) Lookup(token string) (string, bool) {
	d := sha256.Sum256([]byte(token))
	found := -1
	for i := range a.digests {
		if subtle.ConstantTimeCompare(d[:], a.digests[i][:]) == 1 {
			found = i
		}
	}
	if found < 0 {
		return "", false
	}
	return a.names[found], true
}

// Analyst returns the authenticated analyst of a request.
func Analyst(ctx context.Context) string {
	name, _ := ctx.Value(analystKey{}).(string)
	return name
}

// requireAnalyst rejects requests without a known bearer token and logs every request with the
// analyst who made it.
func requireAnalyst(analysts *Analysts, log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		name, known := analysts.Lookup(token)
		if !ok || !known {
			log.Warn("rejected request without a valid analyst token", "method", r.Method, "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="esm"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		path := r.URL.Path // the handler rewrites it for case-service; log what the analyst called
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), analystKey{}, name)))
		log.Info("analyst request", "analyst", name, "method", r.Method, "path", path, "status", rec.status)
	})
}

// stampReviewer sets the review's "analyst" to the authenticated analyst, so the name recorded on
// the case cannot be chosen by the caller.
func stampReviewer(r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxReviewBody))
	if err != nil {
		return err
	}
	var review map[string]any
	if err := json.Unmarshal(body, &review); err != nil || review == nil {
		return errors.New("review must be a JSON object")
	}
	review["analyst"] = Analyst(r.Context())
	body, err = json.Marshal(review)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if c > unicode.MaxASCII || !(unicode.IsLetter(c) || unicode.IsDigit(c) || strings.ContainsRune("._-@", c)) {
			return false
		}
	}
	return true
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
