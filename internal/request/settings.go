package request

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Setting is a value a request may set in its `# steep` block, over the
// defaults of the config's `request:` section.
type Setting struct {
	Key     string
	Label   string
	Help    string
	Choices []string
}

var bools = []string{"false", "true"}

var Settings = []Setting{
	{Key: "mode", Label: "Mode", Choices: []string{"clusterbomb", "pitchfork"},
		Help: "How wordlists combine: clusterbomb tries every combination, pitchfork takes them side by side, up to the longest."},
	{Key: "proto", Label: "Protocol", Choices: []string{"https", "http"},
		Help: "Protocol of a request whose target has no scheme."},
	{Key: "concurrency", Label: "Concurrency", Help: "Parallel in-flight requests."},
	{Key: "rateLimit", Label: "Rate limit", Help: "Most requests started per second (0: no limit)."},
	{Key: "timeout", Label: "Timeout", Help: "Seconds before a request times out."},
	{Key: "upstream", Label: "Upstream proxy",
		Help: "e.g. http://127.0.0.1:8080 or socks5://127.0.0.1:9050; empty sends directly."},
	{Key: "followRedirects", Label: "Follow redirects", Choices: bools,
		Help: "Follow redirects and report the response they lead to."},
	{Key: "recursion", Label: "Recursion", Choices: bools,
		Help: "Fuzz again under each hit redirecting to a directory. One wordlist, at the end of the path."},
	{Key: "recursionDepth", Label: "Recursion depth",
		Help: "How many levels recursion goes down (0: no limit)."},
	{Key: "autocalibration", Label: "Autocalibration", Choices: bools,
		Help: "Send a few random requests first, and filter the responses that look like theirs."},
	{Key: "match.status", Label: "Match status", Help: `Status codes that count as a hit: "200-299,403", or "all".`},
	{Key: "match.size", Label: "Match size", Help: "Response sizes in bytes that count as a hit."},
	{Key: "match.words", Label: "Match words", Help: "Word counts that count as a hit."},
	{Key: "match.lines", Label: "Match lines", Help: "Line counts that count as a hit."},
	{Key: "match.regex", Label: "Match regex", Help: "A regular expression a hit must match."},
	{Key: "filter.status", Label: "Filter status", Help: `Status codes to discard, e.g. "404,500-599".`},
	{Key: "filter.size", Label: "Filter size", Help: "Response sizes in bytes to discard."},
	{Key: "filter.words", Label: "Filter words", Help: "Word counts to discard."},
	{Key: "filter.lines", Label: "Filter lines", Help: "Line counts to discard."},
	{Key: "filter.regex", Label: "Filter regex", Help: "A regular expression whose matches are discarded."},
}

func setting(key string) (Setting, bool) {
	for _, s := range Settings {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// Normalize writes a valid value the way it is stored: "050" is "50".
func Normalize(key, value string) string {
	if _, ok := numbers[key]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return strconv.Itoa(n)
		}
	}
	return strings.TrimSpace(value)
}

// numbers are the settings that are integers, with their lowest value.
var numbers = map[string]int{"concurrency": 1, "rateLimit": 0, "timeout": 1, "recursionDepth": 0}

var rangesRe = regexp.MustCompile(`^\s*\d+(\s*-\s*\d+)?(\s*,\s*\d+(\s*-\s*\d+)?)*\s*$`)

func Validate(key, value string) error {
	s, ok := setting(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	if len(s.Choices) > 0 {
		for _, c := range s.Choices {
			if c == value {
				return nil
			}
		}
		return fmt.Errorf("%s: %q is not one of %s", key, value, strings.Join(s.Choices, ", "))
	}
	if lowest, ok := numbers[key]; ok {
		if n, err := strconv.Atoi(value); err != nil || n < lowest {
			return fmt.Errorf("%s: %q is not a number ≥ %d", key, value, lowest)
		}
		return nil
	}
	switch {
	case value == "":
		return nil
	case key == "upstream":
		if u, err := url.Parse(value); err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("upstream: %q is not a proxy URL", value)
		}
	case strings.HasSuffix(key, ".regex"):
		if _, err := regexp.Compile(value); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	case key == "match.status" && value == "all":
	default:
		if !rangesRe.MatchString(value) {
			return fmt.Errorf("%s: %q is not a list of numbers and ranges (200,300-399)", key, value)
		}
	}
	return nil
}

// Effective overlays the file's settings on defaults. Invalid values are
// ignored here; Problems reports them.
func (f File) Effective(defaults map[string]string) map[string]string {
	out := make(map[string]string, len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for _, s := range Settings {
		if v, ok := f.Get(s.Key); ok && Validate(s.Key, v) == nil {
			out[s.Key] = v
		}
	}
	return out
}

// Problems lists what is wrong with the request: invalid directives, a
// block left open or with invalid arguments.
func (f File) Problems() []error {
	var errs []error
	for _, l := range f.Header {
		k, v, ok := parseDirective(l)
		switch {
		case !ok:
		case k == "id":
		default:
			if err := Validate(k, v); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if _, err := f.ParseBody(); err != nil {
		errs = append(errs, err)
	}
	return errs
}
