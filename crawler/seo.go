package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

func extractSEO(doc *html.Node) SEO {
	var seo SEO

	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Namespace != "" {
			continue
		}

		switch node.Data {
		case "title":
			if !seo.HasTitle {
				seo.HasTitle = true
				seo.Title = cleanText(nodeText(node))
			}
		case "meta":
			if description, ok := metaDescription(node); ok && !seo.HasDescription {
				seo.HasDescription = true
				seo.Description = cleanText(description)
			}
		case "h1":
			seo.HasH1 = true
		}
	}

	return seo
}

func metaDescription(node *html.Node) (string, bool) {
	var (
		name    string
		content string
	)

	for _, attr := range node.Attr {
		switch strings.ToLower(attr.Key) {
		case "name":
			name = attr.Val
		case "content":
			content = attr.Val
		}
	}

	return content, strings.EqualFold(strings.TrimSpace(name), "description")
}

func nodeText(node *html.Node) string {
	var text strings.Builder

	for child := range node.Descendants() {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}

	return text.String()
}

// cleanText схлопывает пробелы и переносы строк в одну строку.
func cleanText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
