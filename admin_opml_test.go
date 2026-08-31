package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"rss-reader/globals"
	"rss-reader/models"
)

func setupOPMLTest(t *testing.T) (*adminService, *http.Cookie, string, string) {
	t.Helper()
	path := writeAdminTestConfig(t)
	t.Setenv("RSS_READER_CONFIG", path)
	if err := globals.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig() error = %v", err)
	}

	service, err := newAdminService(testAdminPassword)
	if err != nil {
		t.Fatalf("newAdminService() error = %v", err)
	}
	cookie, csrf := loginForTest(t, service)

	configRequest := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	configRequest.Header.Set("Cookie", cookie.String())
	configRecorder := httptest.NewRecorder()
	service.adminConfigHandler(configRecorder, configRequest)
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("config GET status = %d, want 200", configRecorder.Code)
	}
	etag := configRecorder.Header().Get("ETag")
	if etag == "" {
		t.Fatal("config response missing ETag")
	}
	return service, cookie, csrf, etag
}

func opmlPostRequest(t *testing.T, service *adminService, cookie *http.Cookie, csrf, etag, host, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/opml", strings.NewReader(body))
	request.Host = host
	request.Header.Set("Cookie", cookie.String())
	request.Header.Set("Origin", "http://"+host)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("If-Match", etag)
	recorder := httptest.NewRecorder()
	service.adminOPMLHandler(recorder, request)
	return recorder
}

func TestOPMLExportIsEmptyForFreshConfig(t *testing.T) {
	service, cookie, _, _ := setupOPMLTest(t)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/opml", nil)
	request.Header.Set("Cookie", cookie.String())
	recorder := httptest.NewRecorder()
	service.adminOPMLHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200", recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Fatalf("unexpected content type: %s", ct)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `<opml version="2.0"`) {
		t.Fatalf("body is not OPML 2.0: %q", body[:len(body)])
	}
	if strings.Contains(body, "<outline") {
		t.Fatal("fresh config should not contain outlines")
	}
}

func TestOPMLImportAddsAndDeduplicatesFeeds(t *testing.T) {
	service, cookie, csrf, etag := setupOPMLTest(t)
	host := "example.com"

	opml := `<?xml version="1.0"?>
<opml version="2.0">
  <head><title>subs</title></head>
  <body>
    <outline text="Tech">
      <outline type="rss" text="Example" xmlUrl="https://example.com/feed.xml"/>
      <outline type="rss" text="Dup" xmlUrl="https://example.com/feed.xml"/>
      <outline type="rss" text="Bad" xmlUrl="ftp://example.com/feed"/>
    </outline>
    <outline type="rss" text="Direct" xmlUrl="https://example.org/rss"/>
  </body>
</opml>`

	recorder := opmlPostRequest(t, service, cookie, csrf, etag, host, opml)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var result opmlImportResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	if result.Imported != 2 {
		t.Fatalf("imported = %d, want 2", result.Imported)
	}
	if result.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2 (duplicate + non-http scheme)", result.Skipped)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d, want 2", result.Total)
	}

	// 配置文件应持久化新订阅
	data, err := os.ReadFile(models.ConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)
	if !strings.Contains(saved, "https://example.com/feed.xml") || !strings.Contains(saved, "https://example.org/rss") {
		t.Fatal("saved config does not contain imported feeds")
	}

	// 再次导入相同 OPML 应全部跳过
	_, _, _, freshETag := func() (*adminService, *http.Cookie, string, string) {
		configRequest := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
		configRequest.Header.Set("Cookie", cookie.String())
		configRecorder := httptest.NewRecorder()
		service.adminConfigHandler(configRecorder, configRequest)
		return service, cookie, csrf, configRecorder.Header().Get("ETag")
	}()
	recorder = opmlPostRequest(t, service, cookie, csrf, freshETag, host, opml)
	if recorder.Code != http.StatusOK {
		t.Fatalf("re-import status = %d, want 200", recorder.Code)
	}
	var rerun opmlImportResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &rerun); err != nil {
		t.Fatalf("decode re-import result: %v", err)
	}
	if rerun.Imported != 0 || rerun.Skipped != 4 || rerun.Total != 2 {
		t.Fatalf("re-import result = %+v, want imported 0 skipped 4 total 2", rerun)
	}

	// 导出应包含全部订阅
	exportRequest := httptest.NewRequest(http.MethodGet, "/api/admin/opml", nil)
	exportRequest.Header.Set("Cookie", cookie.String())
	exportRecorder := httptest.NewRecorder()
	service.adminOPMLHandler(exportRecorder, exportRequest)
	if exportRecorder.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200", exportRecorder.Code)
	}
	exported := exportRecorder.Body.String()
	for _, want := range []string{"https://example.com/feed.xml", "https://example.org/rss"} {
		if !strings.Contains(exported, want) {
			t.Fatalf("exported OPML missing %s", want)
		}
	}
}

func TestOPMLImportRequiresCSRFRevisionAndAuth(t *testing.T) {
	service, cookie, csrf, etag := setupOPMLTest(t)
	opml := `<?xml version="1.0"?><opml version="2.0"><body><outline type="rss" xmlUrl="https://example.com/f.xml"/></body></opml>`

	unauthorized := httptest.NewRequest(http.MethodPost, "/api/admin/opml", strings.NewReader(opml))
	unauthorizedRecorder := httptest.NewRecorder()
	service.adminOPMLHandler(unauthorizedRecorder, unauthorized)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthorizedRecorder.Code)
	}

	noCSRF := httptest.NewRequest(http.MethodPost, "/api/admin/opml", strings.NewReader(opml))
	noCSRF.Header.Set("Cookie", cookie.String())
	noCSRF.Header.Set("If-Match", etag)
	noCSRFRecorder := httptest.NewRecorder()
	service.adminOPMLHandler(noCSRFRecorder, noCSRF)
	if noCSRFRecorder.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", noCSRFRecorder.Code)
	}

	badRevision := httptest.NewRequest(http.MethodPost, "/api/admin/opml", strings.NewReader(opml))
	badRevision.Host = "example.com"
	badRevision.Header.Set("Cookie", cookie.String())
	badRevision.Header.Set("Origin", "http://example.com")
	badRevision.Header.Set("X-CSRF-Token", csrf)
	badRevision.Header.Set("If-Match", `"stale"`)
	badRevisionRecorder := httptest.NewRecorder()
	service.adminOPMLHandler(badRevisionRecorder, badRevision)
	if badRevisionRecorder.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale revision status = %d, want 412", badRevisionRecorder.Code)
	}

	empty := httptest.NewRequest(http.MethodPost, "/api/admin/opml", strings.NewReader(`<?xml version="1.0"?><opml version="2.0"><body/></opml>`))
	empty.Host = "example.com"
	empty.Header.Set("Cookie", cookie.String())
	empty.Header.Set("Origin", "http://example.com")
	empty.Header.Set("X-CSRF-Token", csrf)
	empty.Header.Set("If-Match", etag)
	emptyRecorder := httptest.NewRecorder()
	service.adminOPMLHandler(emptyRecorder, empty)
	if emptyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("empty opml status = %d, want 400", emptyRecorder.Code)
	}

	_, err := io.ReadAll(emptyRecorder.Body)
	if err != nil {
		t.Fatalf("read error body: %v", err)
	}
}
