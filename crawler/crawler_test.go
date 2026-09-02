package crawler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
	client := stubClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusNotFound, "", req), nil
	})

	report := analyze(t, crawler.Options{URL: "https://example.com", HTTPClient: client})

	page := report.Pages[0]
	if page.HTTPStatus != http.StatusNotFound || page.Status != crawler.StatusError {
		t.Errorf("page = %+v, want error status with code 404", page)
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
