// Package curl turns a curl command into a raw request and back.
package curl

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/sectui/steep/internal/request"
)

// Flags that describe the connection rather than the request: their value is
// skipped (false: a switch, listed to make that plain).
var takesValue = map[string]bool{
	"-x": true, "--proxy": true, "-o": true, "--output": true, "-m": true, "--max-time": true,
	"--connect-timeout": true, "-w": true, "--write-out": true, "--resolve": true, "-E": true,
	"--cert": true, "--key": true, "--cacert": true, "--capath": true, "-K": true, "--config": true,
	"--retry": true, "--retry-delay": true, "--retry-max-time": true, "--limit-rate": true,
	"-c": true, "--cookie-jar": true, "--max-redirs": true, "-U": true, "--proxy-user": true,
	"--interface": true, "--local-port": true, "--dns-servers": true, "-T": true, "--upload-file": true,
	"--connect-to": true, "--unix-socket": true, "-D": true, "--dump-header": true, "--trace": true,
	"--trace-ascii": true, "--ciphers": true, "--tls-max": true, "-r": true, "--range": true,
	"--proto": true, "--proto-redir": true, "--proto-default": true, "-y": true, "--speed-time": true,
	"-Y": true, "--speed-limit": true, "-z": true, "--time-cond": true, "-C": true, "--continue-at": true,
	"--max-filesize": true, "--noproxy": true, "--cert-type": true, "--key-type": true, "--pass": true,
	"--engine": true, "--dns-interface": true, "--dns-ipv4-addr": true, "--dns-ipv6-addr": true,
	"--doh-url": true, "--proxy-header": true, "--proxy-cacert": true, "--proxy-cert": true,
	"--proxy-key": true, "--proxy-insecure": false, "--preproxy": true, "--socks5": true,
	"--socks5-hostname": true, "--socks4": true, "--socks4a": true, "--expect100-timeout": true,
	"--happy-eyeballs-timeout-ms": true, "--keepalive-time": true, "--netrc-file": true,
	"--output-dir": true, "--create-file-mode": true, "--stderr": true, "--trace-config": true,
	"--variable": true, "--crlfile": true, "--pinnedpubkey": true,
	"--hostpubmd5": true, "--hostpubsha256": true, "--krb": true, "--service-name": true,
	"--sasl-authzid": true, "--login-options": true, "--mail-from": true, "--mail-rcpt": true,
	"--ftp-port": true, "-P": true, "--quote": true, "-Q": true, "--telnet-option": true, "-t": true,
	"--tftp-blksize": true, "--aws-sigv4": true, "--retry-all-errors": false, "--parallel-max": true,
	"--rate": true, "--ech": true, "--ip-tos": true, "--vlan-priority": true, "--mptcp": false,
}

// Parse reads a curl command. Flags about the connection (-k, -x,
// --max-time) are ignored.
func Parse(cmd string) (request.HTTP, error) {
	h, _, err := parse(cmd)
	return h, err
}

func Scheme(cmd string) string {
	_, scheme, _ := parse(cmd)
	return scheme
}

// Import turns a curl command into a request's text: its raw HTTP, under a
// `# steep proto: http` directive when the URL is http.
func Import(cmd string) (string, error) {
	args, err := command(cmd)
	if err != nil {
		return "", err
	}
	return ImportArgs(args)
}

// ImportArgs is Import for the arguments of curl, as a shell split them.
func ImportArgs(args []string) (string, error) {
	h, scheme, err := parseArgs(args)
	if err != nil {
		return "", err
	}
	if scheme == "http" {
		return "# steep proto: http\n" + h.String(), nil
	}
	return h.String(), nil
}

func command(cmd string) ([]string, error) {
	args, err := split(cmd)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 || args[0] != "curl" {
		return nil, errors.New("not a curl command")
	}
	return args[1:], nil
}

func parse(cmd string) (request.HTTP, string, error) {
	args, err := command(cmd)
	if err != nil {
		return request.HTTP{}, "", err
	}
	return parseArgs(args)
}

func parseArgs(args []string) (request.HTTP, string, error) {
	var err error
	var (
		method, rawURL string
		headers        []request.Header
		data           []string
		get, head      bool
		http2          bool
	)
	next := func(i *int, flag string) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s needs a value", flag)
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		flag, inline, hasInline := a, "", false
		if strings.HasPrefix(a, "--") {
			flag, inline, hasInline = strings.Cut(a, "=")
		} else if len(a) > 2 && a[0] == '-' && strings.Contains("XHdbuAe", a[1:2]) {
			flag, inline, hasInline = a[:2], a[2:], true // -XPOST
		}
		value := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			return next(&i, flag)
		}
		var v string
		switch flag {
		case "-X", "--request":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			method = v
		case "-H", "--header":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			name, val, ok := strings.Cut(v, ":")
			if !ok {
				continue
			}
			headers = append(headers, request.Header{Name: strings.TrimSpace(name), Value: strings.TrimSpace(val)})
		case "-d", "--data", "--data-ascii", "--data-binary":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			if file, ok := strings.CutPrefix(v, "@"); ok {
				b, err := os.ReadFile(file)
				if err != nil {
					return request.HTTP{}, "", fmt.Errorf("%s @%s: %w", flag, file, err)
				}
				v = string(b)
				if flag != "--data-binary" {
					// curl drops the line ends of a file given to -d
					v = strings.NewReplacer("\r", "", "\n", "").Replace(v)
				}
			}
			data = append(data, v)
		case "--data-raw":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			data = append(data, v)
		case "--data-urlencode":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			data = append(data, urlencode(v))
		case "-F", "--form", "--form-string":
			return request.HTTP{}, "", errors.New("multipart forms (-F) are not imported: write the body by hand")
		case "--oauth2-bearer":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			headers = append(headers, request.Header{Name: "Authorization", Value: "Bearer " + v})
		case "--json":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			data = append(data, v)
			headers = append(headers, request.Header{Name: "Content-Type", Value: "application/json"},
				request.Header{Name: "Accept", Value: "application/json"})
		case "-b", "--cookie":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			headers = append(headers, request.Header{Name: "Cookie", Value: v})
		case "-u", "--user":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			headers = append(headers, request.Header{Name: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(v))})
		case "-A", "--user-agent":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			headers = append(headers, request.Header{Name: "User-Agent", Value: v})
		case "-e", "--referer":
			if v, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
			headers = append(headers, request.Header{Name: "Referer", Value: v})
		case "--url":
			if rawURL, err = value(); err != nil {
				return request.HTTP{}, "", err
			}
		case "-G", "--get":
			get = true
		case "--http2", "--http2-prior-knowledge":
			http2 = true
		case "-I", "--head":
			head = true
		default:
			switch {
			case takesValue[flag]:
				if !hasInline {
					i++
				}
			case strings.HasPrefix(a, "-"):
				// a switch without a value: -k, -s, -L, --compressed
			case rawURL == "":
				rawURL = a
			}
		}
	}
	if rawURL == "" {
		return request.HTTP{}, "", errors.New("the curl command has no URL")
	}
	u, err := url.Parse(normalizeURL(rawURL))
	if err != nil {
		return request.HTTP{}, "", err
	}

	body := strings.Join(data, "&")
	if get && body != "" {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += body
		body = ""
	}
	switch {
	case method != "":
	case head:
		method = "HEAD"
	case body != "":
		method = "POST"
	default:
		method = "GET"
	}

	target := u.EscapedPath()
	if target == "" {
		target = "/"
	}
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	h := request.HTTP{Method: method, Target: target, Version: "HTTP/1.1", Body: body}
	if http2 {
		h.Version = "HTTP/2"
	}
	h.Headers = append(h.Headers, request.Header{Name: "Host", Value: u.Host})
	for _, hd := range headers {
		if strings.EqualFold(hd.Name, "host") {
			h.SetHeader("Host", hd.Value)
			continue
		}
		h.Headers = append(h.Headers, hd)
	}
	if body != "" && h.Header("Content-Type") == "" {
		h.Headers = append(h.Headers, request.Header{Name: "Content-Type", Value: "application/x-www-form-urlencoded"})
	}
	h.DelHeader("Content-Length")
	return h, u.Scheme, nil
}

// urlencode is --data-urlencode: "name=content" encodes the content, a
// leading "=" is dropped, and anything else is encoded whole.
func urlencode(v string) string {
	if name, content, ok := strings.Cut(v, "="); ok {
		if name == "" {
			return url.QueryEscape(content)
		}
		return name + "=" + url.QueryEscape(content)
	}
	return url.QueryEscape(v)
}

var schemeRe = regexp.MustCompile(`(?i)^(https?):/*`)

// normalizeURL fixes what curl forgives: "https:/host" or "https:///host"
// is "https://host", and a URL without a scheme is http.
func normalizeURL(u string) string {
	if m := schemeRe.FindStringSubmatch(u); m != nil {
		return strings.ToLower(m[1]) + "://" + u[len(m[0]):]
	}
	return "http://" + u
}

// Format writes h as a curl command, one flag per line. Host is carried by
// the URL and Content-Length is left to curl; Accept-Encoding becomes
// --compressed.
func Format(h request.HTTP, rawURL string) string {
	parts := []string{"curl"}
	if h.Method != "GET" || h.Body != "" {
		parts = append(parts, "-X "+quote(h.Method))
	}
	parts = append(parts, quote(rawURL))
	if h.IsHTTP2() {
		parts = append(parts, "--http2")
	}
	compressed := false
	for _, hd := range h.Headers {
		switch strings.ToLower(hd.Name) {
		case "host", "content-length":
			continue
		case "accept-encoding":
			compressed = true
			continue
		}
		parts = append(parts, "-H "+quote(hd.Name+": "+hd.Value))
	}
	if compressed {
		parts = append(parts, "--compressed")
	}
	if h.Body != "" {
		parts = append(parts, "--data-binary "+quote(h.Body))
	}
	return strings.Join(parts, " \\\n  ")
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// split cuts a command line into words as a POSIX shell would, with $'...'
// quoting too (what browsers' "copy as cURL" writes).
func split(s string) ([]string, error) {
	var (
		out  []string
		cur  strings.Builder
		word bool
	)
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\' && i+1 < len(r) && (r[i+1] == '\n' || r[i+1] == '\r'):
			i++
			if r[i] == '\r' && i+1 < len(r) && r[i+1] == '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if word {
				out = append(out, cur.String())
				cur.Reset()
				word = false
			}
		case c == '\\' && i+1 < len(r):
			i++
			cur.WriteRune(r[i])
			word = true
		case c == '\'':
			j := i + 1
			for j < len(r) && r[j] != '\'' {
				j++
			}
			if j >= len(r) {
				return nil, errors.New("unterminated '")
			}
			cur.WriteString(string(r[i+1 : j]))
			i, word = j, true
		case c == '$' && i+1 < len(r) && r[i+1] == '\'':
			j, err := ansiC(r, i+2, &cur)
			if err != nil {
				return nil, err
			}
			i, word = j, true
		case c == '"':
			j := i + 1
			for ; j < len(r) && r[j] != '"'; j++ {
				if r[j] == '\\' && j+1 < len(r) && strings.ContainsRune("\"\\$`\n", r[j+1]) {
					j++
					if r[j] == '\n' {
						continue
					}
				}
				cur.WriteRune(r[j])
			}
			if j >= len(r) {
				return nil, errors.New(`unterminated "`)
			}
			i, word = j, true
		default:
			cur.WriteRune(c)
			word = true
		}
	}
	if word {
		out = append(out, cur.String())
	}
	return out, nil
}

// ansiC reads a $'...' string from r[i:], up to its closing quote.
func ansiC(r []rune, i int, b *strings.Builder) (int, error) {
	for ; i < len(r); i++ {
		switch r[i] {
		case '\'':
			return i, nil
		case '\\':
			if i+1 >= len(r) {
				return 0, errors.New("unterminated $'")
			}
			i++
			switch r[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 'e', 'E':
				b.WriteByte(0x1b)
			case 'f':
				b.WriteByte('\f')
			case 'v':
				b.WriteByte('\v')
			case '0':
				b.WriteByte(0)
			case 'x':
				if i+2 < len(r) {
					var v int
					if _, err := fmt.Sscanf(string(r[i+1:i+3]), "%02x", &v); err == nil {
						b.WriteByte(byte(v))
						i += 2
						continue
					}
				}
				b.WriteString(`\x`)
			case 'u':
				if i+4 < len(r) {
					var v int
					if _, err := fmt.Sscanf(string(r[i+1:i+5]), "%04x", &v); err == nil {
						b.WriteRune(rune(v))
						i += 4
						continue
					}
				}
				b.WriteString(`\u`)
			default:
				b.WriteRune(r[i])
			}
		default:
			b.WriteRune(r[i])
		}
	}
	return 0, errors.New("unterminated $'")
}

func LooksLikeCurl(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "curl ")
}
