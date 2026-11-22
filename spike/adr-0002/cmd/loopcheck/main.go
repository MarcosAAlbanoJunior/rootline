// Command loopcheck reads the Collector's OTLP JSON file, prints the span and
// attribute inventory per route, and applies the ADR-0002 rule to every DB span:
// walk rootline.app_stack innermost-first and pick the first frame whose line is
// inside a for/range body (go/ast). Files are resolved relative to -root.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type anyValue struct {
	StringValue *string `json:"stringValue"`
	IntValue    *string `json:"intValue"`
	BoolValue   *bool   `json:"boolValue"`
	ArrayValue  *struct {
		Values []anyValue `json:"values"`
	} `json:"arrayValue"`
}

func (v anyValue) String() string {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.IntValue != nil:
		return *v.IntValue
	case v.BoolValue != nil:
		return strconv.FormatBool(*v.BoolValue)
	case v.ArrayValue != nil:
		parts := make([]string, len(v.ArrayValue.Values))
		for i, x := range v.ArrayValue.Values {
			parts[i] = x.String()
		}
		return "[" + strings.Join(parts, " | ") + "]"
	}
	return "?"
}

type kv struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type span struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId"`
	Name         string `json:"name"`
	Kind         int    `json:"kind"`
	Attributes   []kv   `json:"attributes"`
	scope        string
	resource     []kv
}

func (s span) attr(k string) (anyValue, bool) {
	for _, a := range s.Attributes {
		if a.Key == k {
			return a.Value, true
		}
	}
	return anyValue{}, false
}

func main() {
	file := flag.String("traces", "out/traces.jsonl", "OTLP JSON lines file")
	root := flag.String("root", ".", "module root for resolving code paths")
	flag.Parse()

	spans := load(*file)
	byTrace := map[string][]span{}
	for _, s := range spans {
		byTrace[s.TraceID] = append(byTrace[s.TraceID], s)
	}

	// group traces by route of their server span
	routes := map[string][]string{}
	for id, ss := range byTrace {
		for _, s := range ss {
			if s.ParentSpanID == "" {
				r, ok := s.attr("http.route")
				name := s.Name
				if ok {
					name = r.String()
				}
				routes[name] = append(routes[name], id)
			}
		}
	}
	names := make([]string, 0, len(routes))
	for r := range routes {
		names = append(names, r)
	}
	sort.Strings(names)

	idx := &index{root: *root, files: map[string]*fileInfo{}}
	for _, route := range names {
		ids := routes[route]
		sort.Strings(ids)
		fmt.Printf("\n## route %q (%d traces)\n", route, len(ids))
		sample := byTrace[ids[0]]
		fmt.Println("\n### sample trace inventory")
		inventory(sample)

		// apply the rule across all traces of the route
		type key struct{ query, loc string }
		counts := map[key]int{}
		perTrace := map[string]int{}
		for _, id := range ids {
			for _, s := range byTrace[id] {
				q, ok := s.attr("db.query.text")
				if !ok {
					continue
				}
				perTrace[id]++
				loc := "no app stack (hook off?)"
				if st, ok := s.attr("rootline.app_stack"); ok && st.ArrayValue != nil {
					loc = idx.firstLoopFrame(st.ArrayValue.Values)
				}
				counts[key{oneLine(q.String()), loc}]++
			}
		}
		fmt.Println("\n### ADR-0002 rule over all traces of the route")
		keys := make([]key, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
		for _, k := range keys {
			fmt.Printf("- %5d × `%s`\n  → %s\n", counts[k], k.query, k.loc)
		}
		min, max := 1<<30, 0
		for _, n := range perTrace {
			min, max = minInt(min, n), maxInt(max, n)
		}
		fmt.Printf("\nDB spans per trace: min=%d max=%d\n", min, max)
	}
}

func inventory(ss []span) {
	sort.Slice(ss, func(i, j int) bool { return ss[i].ParentSpanID < ss[j].ParentSpanID })
	seen := map[string]bool{}
	for _, s := range ss {
		sig := s.scope + "|" + s.Name
		if seen[sig] {
			continue
		}
		seen[sig] = true
		parent := "root"
		if s.ParentSpanID != "" {
			parent = "child"
		}
		fmt.Printf("- span %q kind=%d (%s) scope=%s\n", s.Name, s.Kind, parent, s.scope)
		for _, a := range s.Attributes {
			fmt.Printf("    %s = %s\n", a.Key, trunc(oneLine(a.Value.String()), 400))
		}
	}
}

func load(path string) []span {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	var out []span
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var doc struct {
			ResourceSpans []struct {
				Resource struct {
					Attributes []kv `json:"attributes"`
				} `json:"resource"`
				ScopeSpans []struct {
					Scope struct {
						Name    string `json:"name"`
						Version string `json:"version"`
					} `json:"scope"`
					Spans []span `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.Unmarshal(sc.Bytes(), &doc); err != nil {
			fmt.Fprintln(os.Stderr, "bad line:", err)
			continue
		}
		for _, rs := range doc.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					s.scope = ss.Scope.Name + "@" + ss.Scope.Version
					s.resource = rs.Resource.Attributes
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// --- index: go/ast loop lookup ---

type fileInfo struct {
	fset  *token.FileSet
	loops [][2]int // body line ranges of for/range statements
	err   error
}

type index struct {
	root  string
	files map[string]*fileInfo
}

func (ix *index) file(rel string) *fileInfo {
	if fi, ok := ix.files[rel]; ok {
		return fi
	}
	fi := &fileInfo{fset: token.NewFileSet()}
	ix.files[rel] = fi
	f, err := parser.ParseFile(fi.fset, filepath.Join(ix.root, rel), nil, 0)
	if err != nil {
		fi.err = err
		return fi
	}
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch s := n.(type) {
		case *ast.ForStmt:
			body = s.Body
		case *ast.RangeStmt:
			body = s.Body
		}
		if body != nil {
			fi.loops = append(fi.loops, [2]int{fi.fset.Position(body.Lbrace).Line, fi.fset.Position(body.Rbrace).Line})
		}
		return true
	})
	return fi
}

func (ix *index) firstLoopFrame(stack []anyValue) string {
	for depth, fr := range stack {
		s := fr.String() // "func file:line"
		sp := strings.LastIndexByte(s, ' ')
		colon := strings.LastIndexByte(s, ':')
		if sp < 0 || colon < sp {
			continue
		}
		file, line := s[sp+1:colon], atoi(s[colon+1:])
		fi := ix.file(file)
		if fi.err != nil {
			return "index error: " + fi.err.Error()
		}
		for _, l := range fi.loops {
			if line > l[0] && line < l[1] {
				return fmt.Sprintf("frame %d/%d `%s` (inside loop at lines %d-%d)", depth, len(stack), s, l[0], l[1])
			}
		}
	}
	return fmt.Sprintf("no frame inside a loop (stack depth %d, innermost `%s`)", len(stack), stack[0].String())
}

func atoi(s string) int     { n, _ := strconv.Atoi(s); return n }
func minInt(a, b int) int   { return min(a, b) }
func maxInt(a, b int) int   { return max(a, b) }
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
