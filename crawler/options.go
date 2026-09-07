package crawler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Значения по умолчанию для Options.
const (
	DefaultDepth       = 10
	DefaultRetries     = 1
	DefaultTimeout     = 15 * time.Second
	DefaultConcurrency = 4
	DefaultUserAgent   = "hexlet-go-crawler/0.1"
)

// Паузы между повторными попытками: первая, а затем удвоение до предела.
const (
	baseRetryBackoff = 100 * time.Millisecond
	maxRetryBackoff  = 2 * time.Second
)

// ErrEmptyURL возвращается, когда URL не задан.
var ErrEmptyURL = errors.New("url is required")

// Options — параметры обхода.
type Options struct {
	URL         string
	Depth       int
	Retries     int
	Delay       time.Duration
	Timeout     time.Duration
	UserAgent   string
	Concurrency int
	IndentJSON  bool
	HTTPClient  *http.Client
}

// normalized проверяет опции и подставляет значения по умолчанию.
func (o Options) normalized() (Options, error) {
	if o.URL == "" {
		return o, ErrEmptyURL
	}

	parsed, err := url.Parse(o.URL)
	if err != nil {
		return o, fmt.Errorf("parse url %q: %w", o.URL, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return o, fmt.Errorf("unsupported url %q: expected http or https", o.URL)
	}

	if parsed.Host == "" {
		return o, fmt.Errorf("unsupported url %q: host is missing", o.URL)
	}

	if o.Depth <= 0 {
		o.Depth = DefaultDepth
	}

	if o.Retries < 0 {
		o.Retries = 0
	}

	if o.Delay < 0 {
		o.Delay = 0
	}

	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}

	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}

	if o.UserAgent == "" {
		o.UserAgent = DefaultUserAgent
	}

	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{}
	}

	return o, nil
}
