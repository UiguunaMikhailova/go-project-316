// Package crawler обходит сайт и собирает JSON-отчёт.
package crawler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// crawler держит состояние обхода: клиент, опции, посещённые адреса.
type crawler struct {
	opts   Options
	client *http.Client
	root   *url.URL

	mu      sync.Mutex
	visited map[string]struct{}
	pages   []Page

	rateMu        sync.Mutex
	nextRequestAt time.Time

	linksMu sync.Mutex
	links   map[string]linkStatus
}

type linkStatus struct {
	code int
	err  string
}

// task — адрес в очереди обхода и его расстояние от стартовой страницы.
type task struct {
	url   string
	depth int
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
	root, err := url.Parse(opts.URL)
	if err != nil {
		root = &url.URL{}
	}

	return &crawler{
		opts:    opts,
		client:  opts.HTTPClient,
		root:    root,
		visited: make(map[string]struct{}),
		links:   make(map[string]linkStatus),
	}
}

// run обходит сайт в ширину, пока очередь не опустеет или не отменят контекст.
func (c *crawler) run(ctx context.Context) Report {
	queue := c.visit(ctx, task{url: c.opts.URL})

	for len(queue) > 0 && ctx.Err() == nil {
		current := queue[0]
		queue = append(queue[1:], c.visit(ctx, current)...)
	}

	return c.report()
}

// visit загружает адрес и возвращает задачи следующего уровня.
func (c *crawler) visit(ctx context.Context, current task) []task {
	if !c.markVisited(current.url) {
		return nil
	}

	page, links := c.fetchPage(ctx, current.url, current.depth)
	c.addPage(page)

	return c.nextTasks(links, current.depth)
}

// nextTasks оставляет только внутренние ссылки, до которых достаёт глубина обхода.
func (c *crawler) nextTasks(links []pageLink, depth int) []task {
	next := depth + 1
	if next >= c.opts.Depth {
		return nil
	}

	tasks := make([]task, 0)

	for _, link := range links {
		if !link.followed || !c.internal(link.url) || c.broken(link.url) {
			continue
		}

		tasks = append(tasks, task{url: link.url, depth: next})
	}

	return tasks
}

// broken сообщает, что ссылка уже проверена и оказалась недоступной.
func (c *crawler) broken(rawURL string) bool {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()

	status, ok := c.links[rawURL]

	return ok && (status.err != "" || status.code >= http.StatusBadRequest)
}

// internal сообщает, ведёт ли ссылка на домен стартовой страницы.
func (c *crawler) internal(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Host, c.root.Host)
}

// fetchPage выполняет запрос и превращает его результат в запись отчёта.
func (c *crawler) fetchPage(ctx context.Context, rawURL string, depth int) (Page, []pageLink) {
	page := Page{
		URL:          rawURL,
		Depth:        depth,
		Status:       StatusOK,
		BrokenLinks:  make([]BrokenLink, 0),
		DiscoveredAt: time.Now().UTC().Truncate(time.Second),
	}

	resp, err := c.request(ctx, http.MethodGet, rawURL)
	if err != nil {
		page.Status = StatusError
		page.Error = err.Error()
		c.rememberLink(rawURL, linkStatus{err: err.Error()})

		return page, nil
	}

	defer func() { _ = resp.Body.Close() }()

	page.HTTPStatus = resp.StatusCode
	c.rememberLink(rawURL, linkStatus{code: resp.StatusCode})

	if resp.StatusCode >= http.StatusBadRequest {
		page.Status = StatusError
		page.Error = fmt.Sprintf("unexpected status %d", resp.StatusCode)

		return page, nil
	}

	links := c.analyzeBody(ctx, &page, resp)

	return page, links
}

// analyzeBody разбирает HTML один раз: и для SEO-тегов, и для ссылок.
func (c *crawler) analyzeBody(ctx context.Context, page *Page, resp *http.Response) []pageLink {
	if !isHTML(resp.Header.Get("Content-Type")) {
		return nil
	}

	doc := parseDocument(resp.Body)
	if doc == nil {
		return nil
	}

	links := extractLinks(c.baseURL(resp, page.URL), doc)

	page.SEO = extractSEO(doc)
	page.BrokenLinks = c.checkLinks(ctx, links)

	return links
}

func (c *crawler) baseURL(resp *http.Response, rawURL string) *url.URL {
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL
	}

	base, err := url.Parse(rawURL)
	if err != nil {
		return &url.URL{}
	}

	return base
}

func (c *crawler) checkLinks(ctx context.Context, links []pageLink) []BrokenLink {
	broken := make([]BrokenLink, 0)

	for _, link := range links {
		status := c.linkStatus(ctx, link.url)

		switch {
		case status.err != "":
			broken = append(broken, BrokenLink{URL: link.url, Error: status.err})
		case status.code >= http.StatusBadRequest:
			broken = append(broken, BrokenLink{URL: link.url, StatusCode: status.code})
		}
	}

	return broken
}

// linkStatus проверяет каждую ссылку не больше одного раза за обход.
func (c *crawler) linkStatus(ctx context.Context, rawURL string) linkStatus {
	c.linksMu.Lock()
	cached, ok := c.links[rawURL]
	c.linksMu.Unlock()

	if ok {
		return cached
	}

	status := c.probeLink(ctx, rawURL)
	c.rememberLink(rawURL, status)

	return status
}

func (c *crawler) rememberLink(rawURL string, status linkStatus) {
	c.linksMu.Lock()
	defer c.linksMu.Unlock()

	c.links[rawURL] = status
}

// probeLink спрашивает ресурс через HEAD и повторяет через GET, если метод не поддержан.
func (c *crawler) probeLink(ctx context.Context, rawURL string) linkStatus {
	code, err := c.statusCode(ctx, http.MethodHead, rawURL)
	if err == nil && code != http.StatusMethodNotAllowed && code != http.StatusNotImplemented {
		return linkStatus{code: code}
	}

	code, err = c.statusCode(ctx, http.MethodGet, rawURL)
	if err != nil {
		return linkStatus{err: err.Error()}
	}

	return linkStatus{code: code}
}

func (c *crawler) statusCode(ctx context.Context, method, rawURL string) (int, error) {
	resp, err := c.request(ctx, method, rawURL)
	if err != nil {
		return 0, err
	}

	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode, nil
}

// request отправляет запрос, повторяя его opts.Retries раз при неудаче.
func (c *crawler) request(ctx context.Context, method, rawURL string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if err := c.throttle(ctx); err != nil {
			return nil, err
		}

		resp, err := c.do(ctx, method, rawURL)
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
func (c *crawler) do(ctx context.Context, method, rawURL string) (*http.Response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.opts.Timeout)

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, nil)
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
