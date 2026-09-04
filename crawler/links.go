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

func parseDocument(body io.Reader) *html.Node {
	doc, err := html.Parse(body)
	if err != nil {
		return nil
	}

	return doc
}

// pageLink — найденная на странице ссылка; followed отмечает переход по <a>.
type pageLink struct {
	url      string
	followed bool
}

func extractLinks(base *url.URL, doc *html.Node) []pageLink {
	links := make([]pageLink, 0)
	positions := make(map[string]int)

	for node := range doc.Descendants() {
		link, ok := nodeLink(base, node)
		if !ok {
			continue
		}

		if position, duplicate := positions[link.url]; duplicate {
			links[position].followed = links[position].followed || link.followed

			continue
		}

		positions[link.url] = len(links)
		links = append(links, link)
	}

	return links
}

func nodeLink(base *url.URL, node *html.Node) (pageLink, bool) {
	if node.Type != html.ElementNode {
		return pageLink{}, false
	}

	name, ok := linkAttributes[node.Data]
	if !ok {
		return pageLink{}, false
	}

	for _, attr := range node.Attr {
		if attr.Key != name {
			continue
		}

		resolved, ok := resolveLink(base, attr.Val)
		if !ok {
			return pageLink{}, false
		}

		return pageLink{url: resolved, followed: node.Data == "a"}, true
	}

	return pageLink{}, false
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
