// Package httpql filters hits with Caido's HTTPQL:
//
//	resp.code.eq:403
//	resp.code.gte:500 AND req.path.cont:"admin"
//	resp.len.gt:1000 OR (resp.code.eq:200 AND req.ext.eq:".php")
//	"flag{"                       (req.raw or resp.raw contains it)
//
// On top of Caido's fields, fuzz.input (the values of the run's keywords),
// fuzz.words and fuzz.lines describe what is specific to a fuzzing run.
package httpql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Record is what a query reads. Raw bytes are read only when a query asks
// for them.
type Record struct {
	ID        int
	Method    string
	Host      string
	Port      int
	Path      string
	Query     string
	TLS       bool
	Code      int
	Len       int64 // response body length
	Roundtrip int64 // milliseconds
	Words     int64
	Lines     int64
	Inputs    []string
	Marked    bool

	ReqRaw  func() string
	RespRaw func() string
}

// Ext is the extension of the path's last segment, with its dot.
func (r Record) Ext() string {
	seg := r.Path[strings.LastIndex(r.Path, "/")+1:]
	if i := strings.LastIndex(seg, "."); i >= 0 {
		return seg[i:]
	}
	return ""
}

type kind int

const (
	intKind kind = iota
	strKind
	boolKind
)

type field struct {
	kind kind
	raw  bool // reads raw bytes
	get  func(Record) any
}

var fields = map[string]field{
	"req.method":     {kind: strKind, get: func(r Record) any { return r.Method }},
	"req.host":       {kind: strKind, get: func(r Record) any { return r.Host }},
	"req.port":       {kind: intKind, get: func(r Record) any { return int64(r.Port) }},
	"req.path":       {kind: strKind, get: func(r Record) any { return r.Path }},
	"req.query":      {kind: strKind, get: func(r Record) any { return r.Query }},
	"req.ext":        {kind: strKind, get: func(r Record) any { return r.Ext() }},
	"req.tls":        {kind: boolKind, get: func(r Record) any { return r.TLS }},
	"req.raw":        {kind: strKind, raw: true, get: func(r Record) any { return call(r.ReqRaw) }},
	"req.len":        {kind: intKind, raw: true, get: func(r Record) any { return int64(len(call(r.ReqRaw))) }},
	"resp.code":      {kind: intKind, get: func(r Record) any { return int64(r.Code) }},
	"resp.len":       {kind: intKind, get: func(r Record) any { return r.Len }},
	"resp.roundtrip": {kind: intKind, get: func(r Record) any { return r.Roundtrip }},
	"resp.raw":       {kind: strKind, raw: true, get: func(r Record) any { return call(r.RespRaw) }},
	"row.id":         {kind: intKind, get: func(r Record) any { return int64(r.ID) }},
	"row.marked":     {kind: boolKind, get: func(r Record) any { return r.Marked }},
	"fuzz.input":     {kind: strKind, get: func(r Record) any { return r.Inputs }},
	"fuzz.words":     {kind: intKind, get: func(r Record) any { return r.Words }},
	"fuzz.lines":     {kind: intKind, get: func(r Record) any { return r.Lines }},
}

// Not meaningful for a fuzzing run: refused with a reason.
var refused = map[string]string{
	"req.created_at": "hits have no creation date of their own",
	"source":         "every hit comes from the same run",
}

func call(f func() string) string {
	if f == nil {
		return ""
	}
	return f()
}

var ops = map[kind][]string{
	intKind:  {"eq", "ne", "gt", "gte", "lt", "lte"},
	strKind:  {"eq", "ne", "cont", "ncont", "like", "nlike", "regex", "nregex"},
	boolKind: {"eq", "ne"},
}

type Query struct {
	root node
}

// Match tells whether r passes the query; an empty query passes everything.
func (q *Query) Match(r Record) bool {
	if q == nil || q.root == nil {
		return true
	}
	return q.root.eval(r)
}

// NeedsRaw reports whether the query reads raw requests or responses.
func (q *Query) NeedsRaw() bool { return q != nil && q.root != nil && q.root.raw() }

type node interface {
	eval(Record) bool
	raw() bool
}

type and struct{ l, r node }
type or struct{ l, r node }

func (n and) eval(r Record) bool { return n.l.eval(r) && n.r.eval(r) }
func (n and) raw() bool          { return n.l.raw() || n.r.raw() }
func (n or) eval(r Record) bool  { return n.l.eval(r) || n.r.eval(r) }
func (n or) raw() bool           { return n.l.raw() || n.r.raw() }

type clause struct {
	f    field
	op   string
	ival int64
	sval string
	bval bool
	re   *regexp.Regexp
}

func (c clause) raw() bool { return c.f.raw }

func (c clause) eval(r Record) bool {
	switch v := c.f.get(r).(type) {
	case int64:
		switch c.op {
		case "eq":
			return v == c.ival
		case "ne":
			return v != c.ival
		case "gt":
			return v > c.ival
		case "gte":
			return v >= c.ival
		case "lt":
			return v < c.ival
		case "lte":
			return v <= c.ival
		}
	case bool:
		return (v == c.bval) == (c.op == "eq")
	case string:
		return c.str(v)
	case []string:
		// a negated operator holds for every value, the others for one
		neg := strings.HasPrefix(c.op, "n")
		for _, s := range v {
			if c.str(s) != neg {
				return !neg
			}
		}
		return neg
	}
	return false
}

func (c clause) str(v string) bool {
	switch c.op {
	case "eq":
		return v == c.sval
	case "ne":
		return v != c.sval
	case "cont":
		return strings.Contains(strings.ToLower(v), strings.ToLower(c.sval))
	case "ncont":
		return !strings.Contains(strings.ToLower(v), strings.ToLower(c.sval))
	case "like", "regex":
		return c.re.MatchString(v)
	case "nlike", "nregex":
		return !c.re.MatchString(v)
	}
	return false
}

// likeRegexp turns a SQL LIKE pattern (% any run, _ one character) into a
// case-insensitive regexp.
func likeRegexp(p string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?is)^")
	for _, r := range p {
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func newClause(name, op string, val token) (clause, error) {
	if why, ok := refused[name]; ok {
		return clause{}, fmt.Errorf("%s: not available here, %s", name, why)
	}
	for k, why := range refused {
		if strings.HasPrefix(name, k+".") {
			return clause{}, fmt.Errorf("%s: not available here, %s", k, why)
		}
	}
	f, ok := fields[name]
	if !ok {
		return clause{}, fmt.Errorf("unknown field %q", name)
	}
	valid := false
	for _, o := range ops[f.kind] {
		valid = valid || o == op
	}
	if !valid {
		return clause{}, fmt.Errorf("%s: unknown operator %q (%s)", name, op, strings.Join(ops[f.kind], ", "))
	}
	c := clause{f: f, op: op}
	switch f.kind {
	case intKind:
		n, err := strconv.ParseInt(val.text, 10, 64)
		if err != nil || val.quoted {
			return clause{}, fmt.Errorf("%s.%s: %q is not an integer", name, op, val.text)
		}
		c.ival = n
	case boolKind:
		b, err := strconv.ParseBool(val.text)
		if err != nil {
			return clause{}, fmt.Errorf("%s.%s: %q is not true or false", name, op, val.text)
		}
		c.bval = b
	case strKind:
		c.sval = val.text
		switch op {
		case "like", "nlike":
			c.re = likeRegexp(val.text)
		case "regex", "nregex":
			re, err := regexp.Compile(val.text)
			if err != nil {
				return clause{}, fmt.Errorf("%s.%s: %w", name, op, err)
			}
			c.re = re
		}
	}
	return c, nil
}
