// Package config is steep's configuration: its layers, its flags, its types.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	flag "github.com/spf13/pflag"

	"github.com/sectui/foam"
	"github.com/sectui/foam/commands"
	fconfig "github.com/sectui/foam/config"
	"github.com/sectui/foam/theme"

	"github.com/sectui/steep/internal/request"
)

//go:embed default.yaml
var Defaults []byte

type Config struct {
	Theme      theme.Theme       `koanf:",squash"`
	Keys       commands.Bindings `koanf:"keys"`
	Steep      Steep             `koanf:",squash"`
	Request    Request           `koanf:"request"`
	HTTP       HTTP              `koanf:"http"`
	Layout     Layout            `koanf:"layout"`
	View       View              `koanf:"view"`
	Screenshot Screenshot        `koanf:"screenshot"`
}

type Steep struct {
	Collection       string            `koanf:"collection"`
	CreateCollection bool              `koanf:"createCollection"`
	ConfirmAbove     int               `koanf:"confirmAbove" validate:"gte=0"`
	Shortcuts        map[string]string `koanf:"shortcuts"`
	FilePicker       string            `koanf:"filePicker"`
	PickerDir        string            `koanf:"pickerDir"`
	OnRunEnd         string            `koanf:"onRunEnd"`
	Pipes            []Pipe            `koanf:"pipes"`
	Env              map[string]string `koanf:"env"`
	Filters          []Filter          `koanf:"filters"`
}

// Filter is an HTTPQL query kept under a name: ctrl+r, or preset:"name".
type Filter struct {
	Name  string `koanf:"name"`
	Query string `koanf:"query"`
}

type Pipe struct {
	Name    string `koanf:"name"`
	Command string `koanf:"command"`
}

type Request struct {
	Mode            string   `koanf:"mode" validate:"oneof=clusterbomb pitchfork"`
	Proto           string   `koanf:"proto" validate:"oneof=http https"`
	Concurrency     int      `koanf:"concurrency" validate:"gte=1"`
	RateLimit       int      `koanf:"rateLimit" validate:"gte=0"`
	Timeout         int      `koanf:"timeout" validate:"gte=1"`
	Upstream        string   `koanf:"upstream"`
	FollowRedirects bool     `koanf:"followRedirects"`
	Recursion       bool     `koanf:"recursion"`
	RecursionDepth  int      `koanf:"recursionDepth" validate:"gte=0"`
	Autocalibration bool     `koanf:"autocalibration"`
	Match           Matchers `koanf:"match"`
	Filter          Matchers `koanf:"filter"`
}

type Matchers struct {
	Status string `koanf:"status"`
	Size   string `koanf:"size"`
	Words  string `koanf:"words"`
	Lines  string `koanf:"lines"`
	Regex  string `koanf:"regex"`
}

type HTTP struct {
	Headers         []string `koanf:"headers"`
	UserAgent       string   `koanf:"userAgent"`
	UserAgentSuffix string   `koanf:"userAgentSuffix"`
	StripPwnFox     bool     `koanf:"stripPwnFox"`
}

type Layout struct {
	Focus      string `koanf:"focus" validate:"oneof=editor collection"`
	Collection string `koanf:"collection" validate:"oneof=shown hidden"`
}

type Screenshot struct {
	Dir       string  `koanf:"dir"`
	Format    string  `koanf:"format" validate:"oneof=png svg"`
	Font      string  `koanf:"font"`
	FontSize  float64 `koanf:"fontSize" validate:"gt=0"`
	Scale     float64 `koanf:"scale" validate:"gt=0"`
	OnCapture string  `koanf:"onCapture"`
}

type View struct {
	PrettyPrint bool `koanf:"prettyPrint"`
	Wrap        bool `koanf:"wrap"`
}

// Values flattens the request defaults under the names a request's `# steep`
// block uses.
func (r Request) Values() map[string]string {
	b := strconv.FormatBool
	return map[string]string{
		"mode":            r.Mode,
		"proto":           r.Proto,
		"concurrency":     strconv.Itoa(r.Concurrency),
		"rateLimit":       strconv.Itoa(r.RateLimit),
		"timeout":         strconv.Itoa(r.Timeout),
		"upstream":        r.Upstream,
		"followRedirects": b(r.FollowRedirects),
		"recursion":       b(r.Recursion),
		"recursionDepth":  strconv.Itoa(r.RecursionDepth),
		"autocalibration": b(r.Autocalibration),
		"match.status":    r.Match.Status,
		"match.size":      r.Match.Size,
		"match.words":     r.Match.Words,
		"match.lines":     r.Match.Lines,
		"match.regex":     r.Match.Regex,
		"filter.status":   r.Filter.Status,
		"filter.size":     r.Filter.Size,
		"filter.words":    r.Filter.Words,
		"filter.lines":    r.Filter.Lines,
		"filter.regex":    r.Filter.Regex,
	}
}

func (c *Config) Validate() error {
	var errs []error
	for k, v := range c.Request.Values() {
		if err := request.Validate(k, v); err != nil {
			errs = append(errs, fmt.Errorf("request.%w", err))
		}
	}
	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return errors.Join(errs...)
	}
	for i, p := range c.Steep.Pipes {
		if strings.TrimSpace(p.Command) == "" {
			return fmt.Errorf("pipes[%d]: no command", i)
		}
	}
	for i, f := range c.Steep.Filters {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Query) == "" {
			return fmt.Errorf("filters[%d]: needs a name and a query", i)
		}
	}
	for i, h := range c.HTTP.Headers {
		if !validHeader(h) {
			return fmt.Errorf("http.headers[%d]: %q is not \"Name: value\"", i, h)
		}
	}
	return nil
}

func validHeader(h string) bool {
	for i, r := range h {
		if r == ':' {
			return i > 0
		}
	}
	return false
}

// Version is set at build time with -ldflags -X.
var Version string

func getVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

const usage = `steep: a terminal UI for fuzzing HTTP with ffuf

Usage: steep [options]
       steep [options] curl <curl arguments>   import a request: "curl" prefixed by "steep "`

// valueFlags are the flags taking their value from the next argument.
var valueFlags = map[string]bool{
	"--collection": true, "--mode": true, "--rate": true, "--concurrency": true,
	"--upstream": true, "--set": true, "-c": true,
}

// SplitCommand cuts the command line at its subcommand: steep's own
// arguments, the subcommand ("" without one) and its arguments.
func SplitCommand(args []string) (own []string, sub string, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return args, "", nil
		case valueFlags[a]:
			i++
		case a == "curl":
			return args[:i], a, args[i+1:]
		}
	}
	return args, "", nil
}

// Load reads every layer and the command line. It returns fconfig.ErrExit
// when the command line asked for something else than running (--help, --version).
func Load(args []string) (*Config, error) {
	var version bool
	cfg, _, err := fconfig.Load[Config](fconfig.Options{
		Name:  "steep",
		Usage: usage,
		Base:  [][]byte{foam.Defaults, Defaults},
		Files: []string{
			filepath.Join(fconfig.Dir("sectui"), "config.yaml"),
			filepath.Join(fconfig.Dir("steep"), "config.yaml"),
		},
		Extra: func(get func(string) string) []string {
			return []string{filepath.Join(CollectionDir(get("collection")), "config.yaml")}
		},
		Flags: func(fs *flag.FlagSet) {
			fs.String("collection", "", "directory of the collection to open")
			fs.String("mode", "", "default mode: clusterbomb or pitchfork")
			fs.Int("rate", 0, "requests per second (0: no limit)")
			fs.Int("concurrency", 0, "parallel in-flight requests")
			fs.String("upstream", "", "upstream proxy (http:// or socks5://)")
			fs.BoolVarP(&version, "version", "v", false, "show the version")
		},
		FlagKeys: map[string]string{
			"collection":  "collection",
			"mode":        "request.mode",
			"rate":        "request.rateLimit",
			"concurrency": "request.concurrency",
			"upstream":    "request.upstream",
		},
	}, args)
	if version {
		fmt.Println("steep", getVersion())
		return nil, fconfig.ErrExit
	}
	if err != nil {
		return nil, err
	}
	cfg.Steep.Collection = CollectionDir(cfg.Steep.Collection)
	for i, p := range cfg.Steep.Pipes {
		// a YAML block (command: |) ends with a newline
		cfg.Steep.Pipes[i].Command = strings.TrimSpace(p.Command)
		if p.Name == "" {
			cfg.Steep.Pipes[i].Name = strings.Join(strings.Fields(p.Command), " ")
		}
	}
	return cfg, nil
}

// CollectionDir resolves the collection setting: "" is $XDG_DATA_HOME/steep.
func CollectionDir(dir string) string {
	if dir != "" {
		return fconfig.ExpandHome(dir)
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "steep")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "steep"
	}
	return filepath.Join(home, ".local", "share", "steep")
}
