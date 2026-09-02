package crawler

import (
	"encoding/json"
	"time"
)

// Статусы страницы в отчёте.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Report — JSON-отчёт обхода.
type Report struct {
	RootURL     string    `json:"root_url"`
	Depth       int       `json:"depth"`
	GeneratedAt time.Time `json:"generated_at"`
	Pages       []Page    `json:"pages"`
}

// Page — одна пройденная страница.
type Page struct {
	URL        string `json:"url"`
	Depth      int    `json:"depth"`
	HTTPStatus int    `json:"http_status"`
	Status     string `json:"status"`
	Error      string `json:"error"`
}

// encode сериализует отчёт в JSON, при indent — в читаемом виде.
func (r Report) encode(indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(r, "", "  ")
	}

	return json.Marshal(r)
}
