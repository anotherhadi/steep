package request

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type Header struct {
	Name, Value string
}

// HTTP is a raw HTTP/1.x request, read leniently: "\n" or "\r\n", a missing
// version.
type HTTP struct {
	Method  string
	Target  string
	Version string
	Headers []Header
	Body    string
}

// ParseHTTP reads a raw request; leading blank lines are skipped.
func ParseHTTP(text string) (HTTP, error) {
	text = strings.TrimLeft(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	head, body, _ := strings.Cut(text, "\n\n")
	lines := strings.Split(head, "\n")
	parts := strings.Fields(lines[0])
	if len(parts) < 2 {
		return HTTP{}, fmt.Errorf("the request line needs a method and a target")
	}
	h := HTTP{Method: parts[0], Target: parts[1], Version: "HTTP/1.1", Body: body}
	if len(parts) > 2 {
		h.Version = parts[2]
	}
	for _, l := range lines[1:] {
		name, value, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return HTTP{}, fmt.Errorf("header %q has no name", l)
		}
		h.Headers = append(h.Headers, Header{strings.TrimSpace(name), strings.TrimSpace(value)})
	}
	return h, nil
}

// String writes the request with "\n" line ends.
func (h HTTP) String() string {
	var b strings.Builder
	b.WriteString(h.Method + " " + h.Target + " " + h.Version + "\n")
	for _, hd := range h.Headers {
		b.WriteString(hd.Name + ": " + hd.Value + "\n")
	}
	b.WriteString("\n")
	b.WriteString(h.Body)
	return b.String()
}

// Header returns the value of the first header named name, whatever its case.
func (h HTTP) Header(name string) string {
	for _, hd := range h.Headers {
		if strings.EqualFold(hd.Name, name) {
			return hd.Value
		}
	}
	return ""
}

// SetHeader replaces the headers named name (whatever their case) by one, or
// appends it.
func (h *HTTP) SetHeader(name, value string) {
	out := h.Headers[:0:0]
	done := false
	for _, hd := range h.Headers {
		if strings.EqualFold(hd.Name, name) {
			if !done {
				out = append(out, Header{name, value})
				done = true
			}
			continue
		}
		out = append(out, hd)
	}
	if !done {
		out = append(out, Header{name, value})
	}
	h.Headers = out
}

// DelHeader drops the headers named name, whatever their case.
func (h *HTTP) DelHeader(name string) {
	out := h.Headers[:0:0]
	for _, hd := range h.Headers {
		if !strings.EqualFold(hd.Name, name) {
			out = append(out, hd)
		}
	}
	h.Headers = out
}

// IsHTTP2 tells whether the request line asks for HTTP/2: "HTTP/2" or
// "HTTP/2.0", whatever their case.
func (h HTTP) IsHTTP2() bool {
	v := strings.ToUpper(h.Version)
	return v == "HTTP/2" || v == "HTTP/2.0"
}

// URL is where the request goes: its target when absolute, else built from
// proto, the Host header and the target.
func (h HTTP) URL(proto string) string {
	if strings.HasPrefix(h.Target, "http://") || strings.HasPrefix(h.Target, "https://") {
		return h.Target
	}
	return proto + "://" + h.Header("Host") + h.Target
}

var nameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SuggestName proposes a request name from its host and path:
// "api.test/login" for POST https://api.test/login?x=1.
func SuggestName(h HTTP, proto string) string {
	u, err := url.Parse(h.URL(proto))
	if err != nil || u.Host == "" {
		return "request"
	}
	host := nameUnsafe.ReplaceAllString(u.Hostname(), "-")
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return host + "/index"
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = strings.Trim(nameUnsafe.ReplaceAllString(s, "-"), "-")
	}
	return host + "/" + strings.Join(segs, "-")
}

// Method reads the method off the request line of a request file, cheaply.
func Method(text string) string {
	f := Parse(text)
	line := strings.TrimLeft(f.Body, "\r\n")
	line, _, _ = strings.Cut(line, " ")
	if len(line) > 10 || strings.ContainsAny(line, "\n\r\t") {
		return ""
	}
	return strings.ToUpper(line)
}

// Peek reads where a request goes without asking the rest of it to be valid:
// the method and target of its request line and its Host header, malformed
// headers skipped. ok is false when the request line is not one yet.
func Peek(text string) (method, target, host string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return "", "", "", false
	}
	f := strings.Fields(lines[i])
	if len(f) < 2 || len(f) > 2 && !strings.HasPrefix(f[2], "HTTP/") {
		return "", "", "", false
	}
	// "get /" may be words being typed; "get / HTTP/1.1" is a request line
	if !methodRe.MatchString(f[0]) && (len(f) < 3 || !IsMethod(f[0])) {
		return "", "", "", false
	}
	for _, l := range lines[i+1:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		if name, value, found := strings.Cut(l, ":"); found && strings.EqualFold(strings.TrimSpace(name), "host") {
			host = strings.TrimSpace(value)
		}
	}
	return f[0], f[1], host, true
}

var methodRe = regexp.MustCompile(`^[A-Z]+$`)

// IsMethod tells whether s is made of letters only, whatever their case.
func IsMethod(s string) bool { return methodRe.MatchString(strings.ToUpper(s)) }

// PwnFoxHeader is the header PwnFox adds to the requests of its Firefox
// containers, holding the container's color. It is a tag for the tester, not
// something to send: steep drops it before sending.
const PwnFoxHeader = "X-PwnFox-Color"

// PwnFoxColor reads the PwnFox color of a request file, cheaply, without
// asking the headers to be valid: "" when it has none.
func PwnFoxColor(text string) string {
	lines := strings.Split(strings.TrimLeft(Parse(text).Body, "\r\n"), "\n")
	for _, l := range lines[1:] {
		l = strings.TrimSuffix(l, "\r")
		if strings.TrimSpace(l) == "" {
			break
		}
		if name, value, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(name), PwnFoxHeader) {
			return strings.ToLower(strings.TrimSpace(value))
		}
	}
	return ""
}
