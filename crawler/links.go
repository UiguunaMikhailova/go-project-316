package crawler

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

var linkAttributes = map[string]string{
	"a":      "href",
	"link":   "href",
	"script": "src",
	"img":    "src",
	"iframe": "src",
	"source": "src",
}

func extractLinks(base *url.URL, body io.Reader) []string {
	doc, err := html.Parse(body)
	if err != nil {
		return nil
	}

	links := make([]string, 0)
	seen := make(map[string]struct{})

	for node := range doc.Descendants() {
		link, ok := nodeLink(base, node)
		if !ok {
			continue
		}

		if _, duplicate := seen[link]; duplicate {
			continue
		}

		seen[link] = struct{}{}
		links = append(links, link)
	}

	return links
}

func nodeLink(base *url.URL, node *html.Node) (string, bool) {
	if node.Type != html.ElementNode {
		return "", false
	}

	name, ok := linkAttributes[node.Data]
	if !ok {
		return "", false
	}

	for _, attr := range node.Attr {
		if attr.Key == name {
			return resolveLink(base, attr.Val)
		}
	}

	return "", false
}

func resolveLink(base *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", false
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	resolved := base.ResolveReference(ref)
	if resolved.Host == "" {
		return "", false
	}

	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", false
	}

	resolved.Fragment = ""
	resolved.RawFragment = ""

	return resolved.String(), true
}

func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}

	contentType = strings.ToLower(contentType)

	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml+xml")
}
