package crawler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"code/crawler"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func response(status int, body string, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func analyze(t *testing.T, opts crawler.Options) crawler.Report {
	t.Helper()

	data, err := crawler.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report crawler.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}

	return report
}

func TestAnalyzeCollectsRootPage(t *testing.T) {
	client := stubClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, "<html><body>hello</body></html>", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	if report.RootURL != "https://example.com" {
		t.Errorf("RootURL = %q, want %q", report.RootURL, "https://example.com")
	}

	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is empty")
	}

	if len(report.Pages) != 1 {
		t.Fatalf("len(Pages) = %d, want 1", len(report.Pages))
	}

	page := report.Pages[0]
	if page.URL != "https://example.com" || page.Depth != 0 {
		t.Errorf("page = %+v, want url https://example.com at depth 0", page)
	}

	if page.HTTPStatus != http.StatusOK || page.Status != crawler.StatusOK || page.Error != "" {
		t.Errorf("page = %+v, want status ok without error", page)
	}

	if report.Depth != 1 {
		t.Errorf("Depth = %d, want 1", report.Depth)
	}
}

func TestAnalyzeSendsUserAgent(t *testing.T) {
	var got string

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("User-Agent")

		return response(http.StatusOK, "", req), nil
	})

	analyze(t, crawler.Options{URL: "https://example.com", UserAgent: "custom-agent", HTTPClient: client})

	if got != "custom-agent" {
		t.Errorf("User-Agent = %q, want %q", got, "custom-agent")
	}
}

func TestAnalyzeReportsNetworkError(t *testing.T) {
	client := stubClient(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	if len(report.Pages) != 1 {
		t.Fatalf("len(Pages) = %d, want 1", len(report.Pages))
	}

	page := report.Pages[0]
	if page.Status != crawler.StatusError || page.HTTPStatus != 0 || page.Error == "" {
		t.Errorf("page = %+v, want error status without http code", page)
	}
}

func TestAnalyzeReportsBadHTTPStatus(t *testing.T) {
	statuses := []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := stubClient(func(req *http.Request) (*http.Response, error) {
				return response(status, "", req), nil
			})

			report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

			page := report.Pages[0]
			if page.HTTPStatus != status || page.Status != crawler.StatusError || page.Error == "" {
				t.Errorf("page = %+v, want error status with code %d", page, status)
			}
		})
	}
}

func TestAnalyzeReportsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	report := analyze(t, crawler.Options{
		URL:        server.URL,
		Timeout:    50 * time.Millisecond,
		HTTPClient: server.Client(),
	})

	page := report.Pages[0]
	if page.Status != crawler.StatusError || page.HTTPStatus != 0 {
		t.Errorf("page = %+v, want error status without http code", page)
	}

	if !strings.Contains(page.Error, "deadline exceeded") {
		t.Errorf("page.Error = %q, want a deadline error", page.Error)
	}
}

func TestAnalyzeCollectsPageFromTestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer server.Close()

	report := analyze(t, crawler.Options{URL: server.URL, HTTPClient: server.Client()})

	page := report.Pages[0]
	if page.URL != server.URL || page.HTTPStatus != http.StatusOK || page.Status != crawler.StatusOK {
		t.Errorf("page = %+v, want ok status for %s", page, server.URL)
	}
}

func TestAnalyzeRetriesFailedRequests(t *testing.T) {
	var calls int32

	client := stubClient(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)

		return nil, errors.New("temporary failure")
	})

	analyze(t, crawler.Options{URL: "https://example.com", Retries: 2, HTTPClient: client})

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("requests = %d, want 3 (first attempt + 2 retries)", got)
	}
}

func TestAnalyzeRejectsInvalidURL(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"without scheme": "example.com",
		"unknown scheme": "ftp://example.com",
		"scheme only":    "https://",
	}

	for name, rawURL := range cases {
		t.Run(name, func(t *testing.T) {
			client := stubClient(func(_ *http.Request) (*http.Response, error) {
				t.Error("unexpected request for an invalid url")

				return nil, errors.New("must not be called")
			})

			if _, err := crawler.Analyze(context.Background(), crawler.Options{URL: rawURL, HTTPClient: client}); err == nil {
				t.Errorf("Analyze(%q) error = nil, want error", rawURL)
			}
		})
	}
}

func TestAnalyzeStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, "", req), req.Context().Err()
	})

	data, err := crawler.Analyze(ctx, crawler.Options{URL: "https://example.com", HTTPClient: client})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report crawler.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}

	if report.Pages[0].Status != crawler.StatusError {
		t.Errorf("page = %+v, want error status", report.Pages[0])
	}
}

func TestAnalyzeKeepsDelayBetweenRequests(t *testing.T) {
	client := stubClient(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("retry me")
	})

	start := time.Now()

	analyze(t, crawler.Options{URL: "https://example.com", Retries: 1, Delay: 50 * time.Millisecond, HTTPClient: client})

	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("elapsed = %v, want at least 50ms between two requests", elapsed)
	}
}

func htmlResponse(body string, req *http.Request) *http.Response {
	resp := response(http.StatusOK, body, req)
	resp.Header.Set("Content-Type", "text/html; charset=utf-8")

	return resp
}

func TestAnalyzeReportsOnlyBrokenLinks(t *testing.T) {
	page := `<html><body>
		<a href="/ok.html">ok</a>
		<a href="/ghost.html">ghost</a>
		<a href="https://cdn.example.com/app.js">cdn</a>
	</body></html>`

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://example.com":
			return htmlResponse(page, req), nil
		case "https://example.com/ok.html":
			return response(http.StatusOK, "", req), nil
		case "https://example.com/ghost.html":
			return response(http.StatusNotFound, "", req), nil
		case "https://cdn.example.com/app.js":
			return nil, errors.New("dial tcp: lookup cdn.example.com: no such host")
		}

		t.Errorf("unexpected request to %s", req.URL)

		return nil, errors.New("unexpected request")
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	broken := report.Pages[0].BrokenLinks
	if len(broken) != 2 {
		t.Fatalf("broken links = %+v, want 2 entries", broken)
	}

	if broken[0].URL != "https://example.com/ghost.html" || broken[0].StatusCode != http.StatusNotFound {
		t.Errorf("broken[0] = %+v, want /ghost.html with code 404", broken[0])
	}

	if broken[1].URL != "https://cdn.example.com/app.js" || broken[1].Error == "" {
		t.Errorf("broken[1] = %+v, want cdn link with an error message", broken[1])
	}

	if broken[1].StatusCode != 0 {
		t.Errorf("broken[1].StatusCode = %d, want 0 for a network failure", broken[1].StatusCode)
	}
}

func TestAnalyzeSkipsUnsupportedLinks(t *testing.T) {
	page := `<html><body>
		<a href="mailto:hi@example.com">mail</a>
		<a href="tel:+123456789">phone</a>
		<a href="javascript:void(0)">js</a>
		<a href="#anchor">anchor</a>
		<a href="">empty</a>
		<a href="   ">spaces</a>
		<a href="data:text/plain,hello">data</a>
	</body></html>`

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://example.com" {
			t.Errorf("unexpected request to %s", req.URL)
		}

		return htmlResponse(page, req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	if len(report.Pages[0].BrokenLinks) != 0 {
		t.Errorf("broken links = %+v, want none", report.Pages[0].BrokenLinks)
	}
}

func TestAnalyzeResolvesRelativeLinks(t *testing.T) {
	page := `<html><body><a href="../ghost.html#top">ghost</a></body></html>`

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://example.com/blog/index.html" {
			return htmlResponse(page, req), nil
		}

		return response(http.StatusNotFound, "", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com/blog/index.html", HTTPClient: client})

	broken := report.Pages[0].BrokenLinks
	if len(broken) != 1 || broken[0].URL != "https://example.com/ghost.html" {
		t.Errorf("broken links = %+v, want absolute url https://example.com/ghost.html", broken)
	}
}

func TestAnalyzeDoesNotDependOnCheckMethod(t *testing.T) {
	page := `<html><body><a href="/head-not-allowed">link</a></body></html>`

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://example.com" {
			return htmlResponse(page, req), nil
		}

		if req.Method == http.MethodHead {
			return response(http.StatusMethodNotAllowed, "", req), nil
		}

		return response(http.StatusOK, "", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	if len(report.Pages[0].BrokenLinks) != 0 {
		t.Errorf("broken links = %+v, want none: the link answers 200 on GET", report.Pages[0].BrokenLinks)
	}
}

func TestAnalyzeChecksEveryLinkOnce(t *testing.T) {
	page := `<html><body>
		<a href="/ghost.html">first</a>
		<a href="/ghost.html">second</a>
		<a href="/ghost.html#anchor">third</a>
	</body></html>`

	var checks int32

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://example.com" {
			return htmlResponse(page, req), nil
		}

		atomic.AddInt32(&checks, 1)

		return response(http.StatusNotFound, "", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", Depth: 1, HTTPClient: client})

	if got := atomic.LoadInt32(&checks); got != 1 {
		t.Errorf("link checks = %d, want 1", got)
	}

	if len(report.Pages[0].BrokenLinks) != 1 {
		t.Errorf("broken links = %+v, want a single entry", report.Pages[0].BrokenLinks)
	}
}

func TestAnalyzeChecksAssetLinks(t *testing.T) {
	page := `<html><head>
		<link rel="stylesheet" href="/assets/ghost.css">
		<script src="/assets/app.js"></script>
	</head><body><img src="/assets/logo.png"></body></html>`

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://example.com":
			return htmlResponse(page, req), nil
		case "https://example.com/assets/ghost.css":
			return response(http.StatusNotFound, "", req), nil
		}

		return response(http.StatusOK, "", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	broken := report.Pages[0].BrokenLinks
	if len(broken) != 1 || broken[0].URL != "https://example.com/assets/ghost.css" {
		t.Errorf("broken links = %+v, want only the missing stylesheet", broken)
	}
}

func analyzeHTML(t *testing.T, body string) crawler.Page {
	t.Helper()

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		return htmlResponse(body, req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	return report.Pages[0]
}

func TestAnalyzeCollectsSEOTags(t *testing.T) {
	page := analyzeHTML(t, `<html>
		<head>
			<title>Example Test</title>
			<meta name="description" content="Short page description">
		</head>
		<body><h1>Hello</h1></body>
	</html>`)

	want := crawler.SEO{
		HasTitle:       true,
		Title:          "Example Test",
		HasDescription: true,
		Description:    "Short page description",
		HasH1:          true,
	}

	if page.SEO != want {
		t.Errorf("seo = %+v, want %+v", page.SEO, want)
	}
}

func TestAnalyzeReportsMissingSEOTags(t *testing.T) {
	page := analyzeHTML(t, `<html><head></head><body><p>no tags here</p></body></html>`)

	want := crawler.SEO{}
	if page.SEO != want {
		t.Errorf("seo = %+v, want empty flags and strings", page.SEO)
	}
}

func TestAnalyzeDecodesHTMLEntities(t *testing.T) {
	page := analyzeHTML(t, `<html>
		<head>
			<title>Tom &amp; Jerry &mdash; &#39;home&#39;</title>
			<meta name="description" content="Caf&eacute; &lt;test&gt;">
		</head>
		<body><h1>x</h1></body>
	</html>`)

	if page.SEO.Title != "Tom & Jerry — 'home'" {
		t.Errorf("title = %q, want decoded entities", page.SEO.Title)
	}

	if page.SEO.Description != "Café <test>" {
		t.Errorf("description = %q, want decoded entities", page.SEO.Description)
	}
}

func TestAnalyzeCleansSEOWhitespace(t *testing.T) {
	page := analyzeHTML(t, "<html><head><title>\n\t Example   Test \n</title></head><body></body></html>")

	if page.SEO.Title != "Example Test" {
		t.Errorf("title = %q, want %q", page.SEO.Title, "Example Test")
	}
}

func TestAnalyzeKeepsFlagsForEmptySEOTags(t *testing.T) {
	page := analyzeHTML(t, `<html><head><title></title><meta name="description" content=""></head><body><h1></h1></body></html>`)

	want := crawler.SEO{HasTitle: true, HasDescription: true, HasH1: true}
	if page.SEO != want {
		t.Errorf("seo = %+v, want %+v", page.SEO, want)
	}
}

func TestAnalyzeIgnoresSVGTitle(t *testing.T) {
	page := analyzeHTML(t, `<html><head></head><body><svg><title>icon</title></svg></body></html>`)

	if page.SEO.HasTitle || page.SEO.Title != "" {
		t.Errorf("seo = %+v, want no title outside the head", page.SEO)
	}
}

// siteClient отвечает подготовленным HTML на адреса внутри https://example.com.
func siteClient(t *testing.T, pages map[string]string) *http.Client {
	t.Helper()

	return stubClient(func(req *http.Request) (*http.Response, error) {
		body, ok := pages[req.URL.String()]
		if !ok {
			return response(http.StatusNotFound, "", req), nil
		}

		return htmlResponse(body, req), nil
	})
}

func pageURLs(report crawler.Report) []string {
	urls := make([]string, 0, len(report.Pages))
	for _, page := range report.Pages {
		urls = append(urls, page.URL)
	}

	return urls
}

func TestAnalyzeRespectsDepthLimit(t *testing.T) {
	pages := map[string]string{
		"https://example.com":                 `<html><body><a href="/child.html">child</a></body></html>`,
		"https://example.com/child.html":      `<html><body><a href="/grandchild.html">grandchild</a></body></html>`,
		"https://example.com/grandchild.html": `<html><body>the end</body></html>`,
	}

	cases := []struct {
		depth int
		want  []string
	}{
		{depth: 1, want: []string{"https://example.com"}},
		{depth: 2, want: []string{"https://example.com", "https://example.com/child.html"}},
		{depth: 3, want: []string{
			"https://example.com",
			"https://example.com/child.html",
			"https://example.com/grandchild.html",
		}},
	}

	for _, testCase := range cases {
		t.Run(fmt.Sprintf("depth %d", testCase.depth), func(t *testing.T) {
			report := analyze(t, crawler.Options{
				URL:        "https://example.com",
				Depth:      testCase.depth,
				HTTPClient: siteClient(t, pages),
			})

			if got := pageURLs(report); !slices.Equal(got, testCase.want) {
				t.Errorf("pages = %v, want %v", got, testCase.want)
			}

			if report.Depth != testCase.depth {
				t.Errorf("report depth = %d, want %d", report.Depth, testCase.depth)
			}
		})
	}
}

func TestAnalyzeReportsPageDepth(t *testing.T) {
	pages := map[string]string{
		"https://example.com":            `<html><body><a href="/child.html">child</a></body></html>`,
		"https://example.com/child.html": `<html><body>child</body></html>`,
	}

	report := analyze(t, crawler.Options{URL: "https://example.com", Depth: 2, HTTPClient: siteClient(t, pages)})

	if report.Pages[0].Depth != 0 || report.Pages[1].Depth != 1 {
		t.Errorf("depths = %d, %d, want 0, 1", report.Pages[0].Depth, report.Pages[1].Depth)
	}
}

func TestAnalyzeSkipsExternalPages(t *testing.T) {
	pages := map[string]string{
		"https://example.com": `<html><body>
			<a href="/first.html">first</a>
			<a href="/second.html">second</a>
			<a href="https://other.test/page.html">external</a>
		</body></html>`,
		"https://example.com/first.html":  `<html><body>first</body></html>`,
		"https://example.com/second.html": `<html><body>second</body></html>`,
	}

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "other.test" {
			if req.Method != http.MethodHead && req.Method != http.MethodGet {
				t.Errorf("unexpected method %s for an external link", req.Method)
			}

			return response(http.StatusNotFound, "", req), nil
		}

		body, ok := pages[req.URL.String()]
		if !ok {
			return response(http.StatusNotFound, "", req), nil
		}

		return htmlResponse(body, req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", Depth: 3, HTTPClient: client})

	want := []string{
		"https://example.com",
		"https://example.com/first.html",
		"https://example.com/second.html",
	}

	if got := pageURLs(report); !slices.Equal(got, want) {
		t.Errorf("pages = %v, want only internal pages %v", got, want)
	}

	broken := report.Pages[0].BrokenLinks
	if len(broken) != 1 || broken[0].URL != "https://other.test/page.html" {
		t.Errorf("broken links = %+v, want the external link checked", broken)
	}
}

func TestAnalyzeVisitsEveryPageOnce(t *testing.T) {
	pages := map[string]string{
		"https://example.com": `<html><body>
			<a href="/child.html">first</a>
			<a href="/child.html">duplicate</a>
			<a href="/child.html#anchor">with anchor</a>
		</body></html>`,
		"https://example.com/child.html": `<html><body><a href="https://example.com">back home</a></body></html>`,
	}

	report := analyze(t, crawler.Options{URL: "https://example.com", Depth: 5, HTTPClient: siteClient(t, pages)})

	want := []string{"https://example.com", "https://example.com/child.html"}
	if got := pageURLs(report); !slices.Equal(got, want) {
		t.Errorf("pages = %v, want %v", got, want)
	}
}

func TestAnalyzeKeepsReportValidWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pages := map[string]string{
		"https://example.com": `<html><body>
			<a href="/first.html">first</a>
			<a href="/second.html">second</a>
		</body></html>`,
		"https://example.com/first.html":  `<html><body>first</body></html>`,
		"https://example.com/second.html": `<html><body>second</body></html>`,
	}

	client := stubClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://example.com/first.html" {
			cancel()
		}

		body, ok := pages[req.URL.String()]
		if !ok {
			return response(http.StatusNotFound, "", req), nil
		}

		return htmlResponse(body, req), nil
	})

	data, err := crawler.Analyze(ctx, crawler.Options{URL: "https://example.com", Depth: 3, HTTPClient: client})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report crawler.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("report is not valid json: %v", err)
	}

	if report.RootURL != "https://example.com" || len(report.Pages) == 0 {
		t.Errorf("report = %+v, want the pages collected before the cancellation", report)
	}

	if len(report.Pages) > 2 {
		t.Errorf("pages = %v, want the crawl to stop after the cancellation", pageURLs(report))
	}
}
