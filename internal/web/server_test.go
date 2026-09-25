package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"cborbench/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s, err := New(st)
	if err != nil {
		t.Fatalf("web: %v", err)
	}
	return s.Routes()
}

func TestIndexShowsTitle(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	newTestServer(t).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "定序封签台") {
		t.Fatalf("index missing title")
	}
}

func TestImportAndView(t *testing.T) {
	h := newTestServer(t)
	form := url.Values{"name": {"evidence"}, "hex": {"bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("import status %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/item?id=") {
		t.Fatalf("redirect %q", loc)
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, loc, nil))
	body, _ := io.ReadAll(rec2.Result().Body)
	s := string(body)
	for _, want := range []string{"原字节", "宽松解析树", "规范化候选", "幂等", "<svg"} {
		if !strings.Contains(s, want) {
			t.Fatalf("item page missing %q", want)
		}
	}
}

func TestImportRejectsBadHex(t *testing.T) {
	h := newTestServer(t)
	form := url.Values{"name": {"bad"}, "hex": {"zz"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "导入失败") {
		t.Fatalf("bad hex not rejected: %d", rec.Code)
	}
}
