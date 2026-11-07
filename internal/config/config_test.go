package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	for _, c := range []struct {
		args, own []string
		sub       string
		rest      []string
	}{
		{[]string{"--rate", "5"}, []string{"--rate", "5"}, "", nil},
		{[]string{"curl", "-i", "x"}, []string{}, "curl", []string{"-i", "x"}},
		{[]string{"--collection", "curl", "curl", "x"}, []string{"--collection", "curl"}, "curl", []string{"x"}},
		{[]string{"--rate=5", "curl", "x"}, []string{"--rate=5"}, "curl", []string{"x"}},
		{[]string{"--", "curl"}, []string{"--", "curl"}, "", nil},
	} {
		own, sub, rest := SplitCommand(c.args)
		if !reflect.DeepEqual(own, c.own) || sub != c.sub || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%q: got %q %q %q", c.args, own, sub, rest)
		}
	}
}

func TestValidatePipes(t *testing.T) {
	c := Config{Request: Request{Mode: "clusterbomb", Proto: "https", Concurrency: 1, Timeout: 1, Match: Matchers{Status: "all"}}}
	c.Steep.Pipes = []Pipe{{Name: "ok", Command: "jq ."}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Steep.Pipes = append(c.Steep.Pipes, Pipe{Name: "empty", Command: " "})
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "pipes[1]") {
		t.Fatalf("got %v", err)
	}
}

func TestTopLevelKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("STEEP_CONFIRM_ABOVE", "7")
	c, err := Load([]string{"--collection", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if c.Steep.ConfirmAbove != 7 || c.Steep.Collection == "" || len(c.Steep.Pipes) == 0 {
		t.Fatalf("got %+v", c.Steep)
	}
}

func TestStripPwnFox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load([]string{"--collection", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !c.HTTP.StripPwnFox {
		t.Fatal("not stripped by default")
	}
	t.Setenv("STEEP_HTTP__STRIP_PWN_FOX", "false")
	if c, err = Load([]string{"--collection", t.TempDir()}); err != nil || c.HTTP.StripPwnFox {
		t.Fatalf("env: %v, %v", err, c.HTTP.StripPwnFox)
	}
}

func TestCollectionConfigCommands(t *testing.T) {
	dir := t.TempDir()
	if got := collectionConfigCommands(filepath.Join(dir, "config.yaml")); got != nil {
		t.Fatalf("missing file: %v", got)
	}
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte("request:\n  timeout: 30\n"), 0o600)
	if got := collectionConfigCommands(p); got != nil {
		t.Fatalf("harmless config: %v", got)
	}
	os.WriteFile(p, []byte("onRunEnd: notify-send done\npipes:\n  - name: j\n    command: jq .\nenv:\n  token: t\n"), 0o600)
	want := []string{"onRunEnd", "pipes", "env"}
	if got := collectionConfigCommands(p); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	os.WriteFile(p, []byte("screenshot:\n  onCapture: wl-copy < $SCREENSHOT\n"), 0o600)
	if got := collectionConfigCommands(p); !slices.Equal(got, []string{"screenshot.onCapture"}) {
		t.Fatalf("nested: %v", got)
	}
}
