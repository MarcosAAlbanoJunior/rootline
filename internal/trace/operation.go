package trace

import "strings"

// Route sources.
const (
	RouteSourceAttribute = "attribute"
	RouteSourceSpanName  = "span_name"
)

// Operation is the entry operation of a trace: an HTTP method and route pattern.
type Operation struct {
	Method      string
	Route       string
	RouteSource string
}

var httpMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "OPTIONS": true, "CONNECT": true, "TRACE": true,
}

// OperationOf derives the entry operation of a server span. The route comes from http.route when
// present; otherwise from a span name that strictly matches "<METHOD> /<pattern>".
func OperationOf(s Span) (Operation, bool) {
	if route, ok := s.String("http.route"); ok && route != "" {
		method, _ := s.String("http.request.method")
		if method == "" {
			method, _ = s.String("http.method")
		}
		if method == "" {
			if m, _, ok := parseSpanName(s.Name); ok {
				method = m
			}
		}
		if !httpMethods[method] {
			return Operation{}, false
		}
		return Operation{Method: method, Route: route, RouteSource: RouteSourceAttribute}, true
	}
	method, route, ok := parseSpanName(s.Name)
	if !ok {
		return Operation{}, false
	}
	return Operation{Method: method, Route: route, RouteSource: RouteSourceSpanName}, true
}

// parseSpanName splits "<METHOD> /<pattern>" strictly: one space, a known upper-case method and a
// route that starts with "/" and has no whitespace.
func parseSpanName(name string) (method, route string, ok bool) {
	method, route, found := strings.Cut(name, " ")
	if !found || !httpMethods[method] {
		return "", "", false
	}
	if !strings.HasPrefix(route, "/") || strings.ContainsAny(route, " \t\n\r") {
		return "", "", false
	}
	return method, route, true
}
