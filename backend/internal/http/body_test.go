package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJSONBodyCaps: JSON bodies go through authbase.DecodeJSON; over 1 MiB
// is 413 request_too_large, broken JSON and unknown fields are 400.
func TestJSONBodyCaps(t *testing.T) {
	h := newHarness(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Dev-User", "alice")
		req.Header.Set("X-Dev-Groups", "tasks-creator")
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		body, code string
		status     int
	}{
		{`{"name":"` + strings.Repeat("a", 1<<20) + `"}`, "request_too_large", http.StatusRequestEntityTooLarge},
		{`{"name":`, "bad_request", http.StatusBadRequest},
		{`{"name":"A","key":"AAA","color_of_the_day":"red"}`, "bad_request", http.StatusBadRequest},
		{`{"name":"A","key":"AAA"} {"again":1}`, "bad_request", http.StatusBadRequest},
	} {
		rec := post(tc.body)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("body %.40q: %d %s, want %d %s", tc.body, rec.Code, rec.Body.String(), tc.status, tc.code)
		}
	}
}
