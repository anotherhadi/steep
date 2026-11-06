package request

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sectui/operations"
	"github.com/sectui/operations/template"
)

// A request body holds blocks: file{path} wordlists, and the operations of
// github.com/sectui/operations, which may wrap them: to-base64{file{x.txt}}
// encodes each line of x.txt. An operation without a wordlist inside is
// computed once per run, one with some for each request.

// FileOp is the file{} block. steep mounts it as a wordlist instead of running it.
var FileOp = &operations.Operation{
	ID:          "file",
	Name:        "Wordlist",
	Description: "Each line of the file, one request each.",
}

// ListOp and RangeOp are wordlists written in the request.
var ListOp = &operations.Operation{
	ID:          "list",
	Name:        "List",
	Description: `Each comma-separated value, one request each: list{admin,root}. "\," is a comma in a value.`,
}

var RangeOp = &operations.Operation{
	ID:          "range",
	Name:        "Range",
	Description: "Each number from a to b, one request each: range{1-500}. range{001-500} pads with zeros.",
}

// EnvOp is a value of env in the config, or else of the environment.
var EnvOp = &operations.Operation{
	ID:          "env",
	Name:        "Variable",
	Description: "The value of env.NAME in the config, or else of $NAME: env{token}.",
	Run: func(in []byte, _ operations.Args) ([]byte, error) {
		name := strings.TrimSpace(string(in))
		if v, ok := Env[name]; ok {
			return []byte(v), nil
		}
		if v, ok := os.LookupEnv(name); ok {
			return []byte(v), nil
		}
		return nil, fmt.Errorf("env{%s}: not in the config's env nor the environment", name)
	},
}

// Env is env of the config, read by env{}.
var Env map[string]string

// Ops are steep's own blocks, offered with the operations.
var Ops = []*operations.Operation{FileOp, ListOp, RangeOp, EnvOp}

// Engine parses request bodies: the operations, and steep's own blocks.
var Engine = &template.Engine{Lookup: func(id string) (*operations.Operation, bool) {
	for _, op := range Ops {
		if op.ID == id {
			return op, true
		}
	}
	return operations.Lookup(id)
}}

// IsWordlist tells whether op stands for one value of a wordlist per request.
func IsWordlist(op *operations.Operation) bool {
	return op == FileOp || op == ListOp || op == RangeOp
}

// Block is a file{path} in a line, found without parsing the whole text: the
// wordlist picker uses it on the line under the cursor. Start and End are
// byte offsets of the whole block.
type Block struct {
	Start, End int
	Path       string
}

var blockRe = regexp.MustCompile(`file\{((?:[^{}\\\n]|\\.)*)\}`)

var pathEscaper = strings.NewReplacer(`\`, `\\`, `{`, `\{`, `}`, `\}`)
var pathUnescaper = strings.NewReplacer(`\\`, `\`, `\{`, `{`, `\}`, `}`)

// EscapePath writes path as a file{} block holds it: its braces and
// backslashes escaped.
func EscapePath(path string) string { return pathEscaper.Replace(path) }

func Blocks(s string) []Block {
	var out []Block
	for _, m := range blockRe.FindAllStringSubmatchIndex(s, -1) {
		// as in templates, a block starts a word
		if m[0] > 0 && isIDChar(s[m[0]-1]) {
			continue
		}
		out = append(out, Block{Start: m[0], End: m[1], Path: pathUnescaper.Replace(strings.TrimSpace(s[m[2]:m[3]]))})
	}
	return out
}

func isIDChar(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' }

// Keyword is what the n-th wordlist (from 1) becomes for ffuf.
func Keyword(n int) string { return "§" + strconv.Itoa(n) + "§" }

// Wordlist is a wordlist block once mounted: its keyword, and its file, or
// its lines for list{} and range{}.
type Wordlist struct {
	Keyword string
	Path    string
	Lines   []string
}

type Body struct {
	src   string
	nodes []template.Node
	// keywords of the file{} blocks, numbered in the order they are written
	keywords map[*template.Block]string
	files    []*template.Block
	// what comes before the body in its file, for the positions of errors
	lines, bytes int
}

// shift makes the position of a template error count from the top of the
// body's file.
func (b *Body) shift(err error) error {
	return shiftError(err, b.lines, b.bytes)
}

func shiftError(err error, lines, bytes int) error {
	var terr *template.Error
	if lines == 0 && bytes == 0 || !errors.As(err, &terr) {
		return err
	}
	e := *terr
	e.Line += lines
	e.Pos += bytes
	return &e
}

// ParseBody parses the blocks of a request body. Error positions count
// from the start of s.
func ParseBody(s string) (*Body, error) {
	nodes, err := Engine.Parse(s)
	if err != nil {
		return nil, err
	}
	b := &Body{src: s, nodes: nodes, keywords: map[*template.Block]string{}}
	var walk func([]template.Node) error
	walk = func(nodes []template.Node) error {
		for _, n := range nodes {
			blk, ok := n.(*template.Block)
			if !ok {
				continue
			}
			if !IsWordlist(blk.Op) {
				if err := walk(blk.Children); err != nil {
					return err
				}
				continue
			}
			if err := checkWordlist(s, blk); err != nil {
				return err
			}
			b.files = append(b.files, blk)
			b.keywords[blk] = Keyword(len(b.files))
		}
		return nil
	}
	if err := walk(nodes); err != nil {
		return nil, err
	}
	return b, nil
}

func checkWordlist(src string, blk *template.Block) error {
	path, plain := filePath(blk)
	noun := "text"
	if blk.Op == FileOp {
		noun = "path"
	}
	id := blk.Op.ID
	msg := ""
	switch {
	case !plain:
		msg = id + "{} takes a plain " + noun + ", not a block"
	case path == "":
		msg = id + "{} needs a " + noun
	case strings.ContainsAny(path, "\r\n"):
		msg = id + "{} takes a " + noun + " on one line"
	case blk.Op == FileOp && strings.Contains(path, ":"):
		msg = fmt.Sprintf("file{%s}: a wordlist path cannot hold ':'", path)
	case blk.Op == RangeOp:
		if _, err := rangeLines(path); err != nil {
			msg = err.Error()
		}
	}
	if msg == "" {
		return nil
	}
	return template.ErrorAt(src, blk.Head.Start, msg)
}

// maxRange keeps a range{} to what a run can go through.
const maxRange = 10_000_000

var rangeRe = regexp.MustCompile(`^(\d+)\s*-\s*(\d+)$`)

// rangeLines are the numbers of a range{a-b}, padded to the width of a when
// it starts with a zero.
func rangeLines(s string) ([]string, error) {
	m := rangeRe.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("range{%s}: write range{from-to}, e.g. range{1-500}", s)
	}
	from, err1 := strconv.Atoi(m[1])
	to, err2 := strconv.Atoi(m[2])
	switch {
	case err1 != nil || err2 != nil:
		return nil, fmt.Errorf("range{%s}: number too big", s)
	case from > to:
		return nil, fmt.Errorf("range{%s}: %d is above %d", s, from, to)
	case to-from >= maxRange:
		return nil, fmt.Errorf("range{%s}: more than %d values", s, maxRange)
	}
	format := "%d"
	if len(m[1]) > 1 && m[1][0] == '0' {
		format = "%0" + strconv.Itoa(len(m[1])) + "d"
	}
	out := make([]string, 0, to-from+1)
	for n := from; n <= to; n++ {
		out = append(out, fmt.Sprintf(format, n))
	}
	return out, nil
}

// listLines are the values of a list{}: split at commas, "\," kept as one.
func listLines(s string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ',':
			b.WriteByte(',')
			i++
		case s[i] == ',':
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(s[i])
		}
	}
	return append(out, strings.TrimSpace(b.String()))
}

// filePath is the path written in a file{} block; plain is false when the
// block holds other blocks.
func filePath(blk *template.Block) (path string, plain bool) {
	var b strings.Builder
	for _, c := range blk.Children {
		t, ok := c.(*template.Text)
		if !ok {
			return "", false
		}
		b.WriteString(t.Value)
	}
	return strings.TrimSpace(b.String()), true
}

// Wordlists are the wordlist blocks, in the order they are written.
func (b *Body) Wordlists() []Wordlist {
	out := make([]Wordlist, len(b.files))
	for i, f := range b.files {
		text, _ := filePath(f)
		out[i] = Wordlist{Keyword: b.keywords[f]}
		switch f.Op {
		case ListOp:
			out[i].Lines = listLines(text)
		case RangeOp:
			out[i].Lines, _ = rangeLines(text) // checked by ParseBody
		default:
			out[i].Path = text
		}
	}
	return out
}

// Dynamic is an operation over wordlists, computed for each request: its
// Placeholder stands in the expanded text.
type Dynamic struct {
	Placeholder string
	// Source is the block as written.
	Source   string
	block    *template.Block
	keywords map[*template.Block]string
}

// Eval computes the block from the values of the wordlists, by keyword.
func (d Dynamic) Eval(input map[string][]byte) ([]byte, error) {
	return eval(d.block, input, d.keywords)
}

func eval(blk *template.Block, input map[string][]byte, keywords map[*template.Block]string) ([]byte, error) {
	if IsWordlist(blk.Op) {
		return input[keywords[blk]], nil
	}
	var in []byte
	for _, c := range blk.Children {
		switch c := c.(type) {
		case *template.Text:
			in = append(in, c.Value...)
		case *template.Block:
			v, err := eval(c, input, keywords)
			if err != nil {
				return nil, err
			}
			in = append(in, v...)
		}
	}
	return blk.Op.Apply(in, blk.Args)
}

func (b *Body) hasFile(blk *template.Block) bool {
	for _, f := range b.files {
		if f.Pos.Start >= blk.Content.Start && f.Pos.End <= blk.Content.End {
			return true
		}
	}
	return false
}

// Expanded is a body ready for ffuf: file{} blocks are keywords, the other
// blocks computed, or placeholders when they hold wordlists.
type Expanded struct {
	Text    string
	Dynamic []Dynamic
}

// Expand computes the blocks that do not depend on a wordlist.
func (b *Body) Expand() (Expanded, error) {
	var e Expanded
	var out strings.Builder
	for _, n := range b.nodes {
		switch n := n.(type) {
		case *template.Text:
			out.WriteString(n.Value)
		case *template.Block:
			switch {
			case IsWordlist(n.Op):
				out.WriteString(b.keywords[n])
			case b.hasFile(n):
				// The wordlists' keywords stay in the placeholder: ffuf
				// drops a wordlist whose keyword is not in the request.
				var kws strings.Builder
				for _, f := range b.files {
					if f.Pos.Start >= n.Content.Start && f.Pos.End <= n.Content.End {
						kws.WriteString(b.keywords[f])
					}
				}
				ph := "§op" + strconv.Itoa(len(e.Dynamic)+1) + ":" + kws.String() + "§"
				e.Dynamic = append(e.Dynamic, Dynamic{Placeholder: ph, Source: b.src[n.Pos.Start:n.Pos.End], block: n, keywords: b.keywords})
				out.WriteString(ph)
			default:
				v, err := template.Eval(b.src, []template.Node{n})
				if err != nil {
					return Expanded{}, b.shift(err)
				}
				out.Write(v)
			}
		}
	}
	e.Text = out.String()
	return e, nil
}

// Mask replaces every block by a token without spaces, so that a request
// line can be split; unmask puts the blocks back as they are written.
func (b *Body) Mask() (masked string, unmask func(string) string) {
	var out strings.Builder
	var pairs []string
	for _, n := range b.nodes {
		switch n := n.(type) {
		case *template.Text:
			out.WriteString(b.src[n.Pos.Start:n.Pos.End])
		case *template.Block:
			tok := "§b" + strconv.Itoa(len(pairs)/2+1) + "§"
			pairs = append(pairs, tok, b.src[n.Pos.Start:n.Pos.End])
			out.WriteString(tok)
		}
	}
	r := strings.NewReplacer(pairs...)
	return out.String(), r.Replace
}

// ParseBody parses the file's body; error lines count from the top of the
// file.
func (f File) ParseBody() (*Body, error) {
	lines, bytes := len(f.Header), len(f.String())-len(f.Body)
	b, err := ParseBody(f.Body)
	if err != nil {
		return nil, shiftError(err, lines, bytes)
	}
	b.lines, b.bytes = lines, bytes
	return b, nil
}

// EscapeBlocks makes s plain text in a request file: a backslash before each
// block keeps it as written.
func EscapeBlocks(s string) string {
	for range 10_000 {
		nodes, err := Engine.Parse(s)
		var at []int
		var terr *template.Error
		switch {
		case err == nil:
			for _, n := range nodes {
				if b, ok := n.(*template.Block); ok {
					at = append(at, b.Pos.Start)
				}
			}
		case errors.As(err, &terr):
			if i := headBefore(s, terr.Pos); i >= 0 {
				at = []int{i}
			}
		}
		if len(at) == 0 {
			return s
		}
		for i := len(at) - 1; i >= 0; i-- {
			s = s[:at[i]] + `\` + s[at[i]:]
		}
	}
	return s
}

// headBefore is where the block head closest before pos starts, or -1.
func headBefore(s string, pos int) int {
	for i := min(pos, len(s)-1); i >= 0; i-- {
		if i > 0 && (isIDChar(s[i-1]) || s[i-1] == '\\') {
			continue
		}
		k := i
		for k < len(s) && isIDChar(s[k]) {
			k++
		}
		if _, ok := Engine.Lookup(s[i:k]); ok && k < len(s) && strings.IndexByte("({|", s[k]) >= 0 {
			return i
		}
	}
	return -1
}
