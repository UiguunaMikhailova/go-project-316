// Package crawler обходит сайт и собирает JSON-отчёт.
package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// crawler держит состояние обхода: клиент, опции, посещённые адреса.
type crawler struct {
	opts   Options
	client *http.Client

	mu      sync.Mutex
	visited map[string]struct{}
	pages   []Page

	rateMu        sync.Mutex
	nextRequestAt time.Time
}

// Analyze обходит сайт, описанный в opts, и возвращает JSON-отчёт.
func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, err
	}

	c := newCrawler(opts)
	report := c.run(ctx)

	return report.encode(opts.IndentJSON)
}

func newCrawler(opts Options) *crawler {
	return &crawler{
		opts:    opts,
		client:  opts.HTTPClient,
		visited: make(map[string]struct{}),
	}
}

func (c *crawler) run(ctx context.Context) Report {
	c.visit(ctx, c.opts.URL, 0)

	return c.report()
}

// visit загружает адрес, если он ещё не встречался.
func (c *crawler) visit(ctx context.Context, rawURL string, depth int) {
	if !c.markVisited(rawURL) {
		return
	}

	c.addPage(c.fetchPage(ctx, rawURL, depth))
}

// fetchPage выполняет запрос и превращает его результат в запись отчёта.
func (c *crawler) fetchPage(ctx context.Context, rawURL string, depth int) Page {
	page := Page{URL: rawURL, Depth: depth, Status: StatusOK}

	resp, err := c.request(ctx, rawURL)
	if err != nil {
		page.Status = StatusError
		page.Error = err.Error()

		return page
	}

	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)

	page.HTTPStatus = resp.StatusCode
	if resp.StatusCode >= http.StatusBadRequest {
		page.Status = StatusError
		page.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)
	}

	return page
}

// request отправляет GET-запрос, повторяя его opts.Retries раз при неудаче.
func (c *crawler) request(ctx context.Context, rawURL string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if err := c.throttle(ctx); err != nil {
			return nil, err
		}

		resp, err := c.do(ctx, rawURL)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		if ctx.Err() != nil {
			break
		}
	}

	return nil, lastErr
}

// do выполняет одну попытку, ограниченную таймаутом запроса.
func (c *crawler) do(ctx context.Context, rawURL string) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.opts.Timeout)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()

		return nil, err
	}

	req.Header.Set("User-Agent", c.opts.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		cancel()

		return nil, err
	}

	resp.Body = &bodyWithCancel{ReadCloser: resp.Body, cancel: cancel}

	return resp, nil
}

// throttle выдерживает заданную паузу между запросами всего обхода.
func (c *crawler) throttle(ctx context.Context) error {
	if c.opts.Delay <= 0 {
		return ctx.Err()
	}

	c.rateMu.Lock()
	defer c.rateMu.Unlock()

	if wait := time.Until(c.nextRequestAt); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	c.nextRequestAt = time.Now().Add(c.opts.Delay)

	return ctx.Err()
}

// markVisited сообщает, встретился ли адрес впервые.
func (c *crawler) markVisited(rawURL string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.visited[rawURL]; ok {
		return false
	}

	c.visited[rawURL] = struct{}{}

	return true
}

func (c *crawler) addPage(page Page) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pages = append(c.pages, page)
}

func (c *crawler) report() Report {
	c.mu.Lock()
	defer c.mu.Unlock()

	pages := make([]Page, len(c.pages))
	copy(pages, c.pages)

	depth := 0
	for _, page := range pages {
		if page.Depth+1 > depth {
			depth = page.Depth + 1
		}
	}

	return Report{
		RootURL:     c.opts.URL,
		Depth:       depth,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Pages:       pages,
	}
}

// bodyWithCancel освобождает контекст запроса после закрытия тела ответа.
type bodyWithCancel struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *bodyWithCancel) Close() error {
	defer b.cancel()

	return b.ReadCloser.Close()
}
