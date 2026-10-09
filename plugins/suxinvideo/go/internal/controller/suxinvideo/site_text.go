package suxinvideo

import (
	"html"
	"regexp"
	"strings"
)

var htmlTag = regexp.MustCompile(`<[^>]*>`)

func filmDescription(value string) string {
	value = strings.ReplaceAll(value, "<br>", " ")
	value = strings.ReplaceAll(value, "<br/>", " ")
	value = strings.ReplaceAll(value, "<br />", " ")
	value = html.UnescapeString(htmlTag.ReplaceAllString(value, " "))
	return strings.Join(strings.Fields(value), " ")
}
