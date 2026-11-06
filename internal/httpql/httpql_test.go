package httpql

import (
	"strings"
	"testing"
)

var rec = Record{
	ID: 3, Method: "GET", Host: "api.test", Port: 443, Path: "/admin/index.php", Query: "a=1",
	TLS: true, Code: 403, Len: 1234, Roundtrip: 80, Words: 10, Lines: 2,
	Inputs:  []string{"admin", "x"},
	ReqRaw:  func() string { return "GET /admin/index.php HTTP/1.1\r\nHost: api.test\r\n\r\n" },
	RespRaw: func() string { return "HTTP/1.1 403 Forbidden\r\n\r\nflag{nope}" },
}

func TestMatch(t *testing.T) {
	for q, want := range map[string]bool{
		``:                                       true,
		`resp.code.eq:403`:                       true,
		`resp.code.ne:403`:                       false,
		`resp.code.gte:500 OR resp.len.gt:1000`:  true,
		`resp.code.gte:500 AND resp.len.gt:1000`: false,
		`req.path.cont:"ADMIN"`:                  true,
		`req.path.eq:"/ADMIN/index.php"`:         false,
		`req.ext.eq:".php"`:                      true,
		`req.path.like:"/admin/%.php"`:           true,
		`req.path.nlike:"%.asp"`:                 true,
		`req.host.regex:"^api\\."`:               true,
		`req.tls.eq:true`:                        true,
		`"flag{"`:                                true,
		`"absent"`:                               false,
		`resp.raw.cont:"forbidden" and req.method.eq:GET`:               true,
		`(resp.code.eq:200 OR resp.code.eq:403) AND NOT_A_CLAUSE.eq:1`:  false,
		`fuzz.input.eq:"admin"`:                                         true,
		`fuzz.input.ne:"admin"`:                                         false,
		`fuzz.input.ncont:"zzz"`:                                        true,
		`fuzz.words.lt:11 AND fuzz.lines.eq:2`:                          true,
		`row.id.eq:3`:                                                   true,
		`resp.code.eq:200 OR resp.code.eq:403 AND req.method.eq:"POST"`: false,
	} {
		query, err := Parse(q)
		if err != nil {
			if !strings.Contains(q, "NOT_A_CLAUSE") {
				t.Errorf("Parse(%q): %v", q, err)
			}
			continue
		}
		if got := query.Match(rec); got != want {
			t.Errorf("%q = %v, want %v", q, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	for q, msg := range map[string]string{
		`resp.code.eq:"403"`:    "not an integer",
		`resp.code.cont:4`:      "unknown operator",
		`req.created_at.gt:"x"`: "not available",
		`preset:"x"`:            `no preset "x"`,
		`nonsense`:              "expected namespace",
		`resp.code.eq:403 AND`:  "unexpected end",
		`(resp.code.eq:403`:     `expected ")"`,
		`req.path.cont:"x`:      "unterminated",
		`req.nope.eq:"x"`:       "unknown field",
		`req.path.regex:"("`:    "missing closing",
	} {
		_, err := Parse(q)
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("Parse(%q) = %v, want %q", q, err, msg)
		}
	}
}

func TestNeedsRaw(t *testing.T) {
	q, _ := Parse(`resp.code.eq:200`)
	r, _ := Parse(`resp.code.eq:200 OR "x"`)
	if q.NeedsRaw() || !r.NeedsRaw() {
		t.Fatal("NeedsRaw")
	}
}

func TestPresetsAndMarks(t *testing.T) {
	presets := map[string]string{
		"errors":   "resp.code.gte:500",
		"marked":   "row.marked.eq:true",
		"both":     `preset:"errors" AND preset:marked`,
		"loop":     `preset:"loop"`,
		"broken":   "resp.code.eq:",
		"is empty": " ",
	}
	r := Record{Code: 503, Marked: true}
	for q, want := range map[string]bool{
		`preset:"both"`: true,
		`preset:"errors" AND row.marked.eq:false`: false,
		`row.marked.ne:true OR preset:errors`:     true,
	} {
		p, err := ParsePresets(q, presets)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if got := p.Match(r); got != want {
			t.Errorf("%s = %v", q, got)
		}
	}
	for q, msg := range map[string]string{
		`preset:"loop"`:     "goes in circles",
		`preset:"broken"`:   `preset "broken"`,
		`preset:"is empty"`: "is empty",
		`preset:"nope"`:     `no preset "nope"`,
	} {
		if _, err := ParsePresets(q, presets); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", q, err, msg)
		}
	}
}
