package editor

import (
	"unicode/utf8"

	"github.com/anotherhadi/operations/template"

	"github.com/anotherhadi/foam/highlight"

	"github.com/anotherhadi/steep/internal/request"
)

// Highlight colors a request file: its `# steep` block, and its blocks.
var Highlight = highlight.Options{Directive: "steep", Spans: BlockSpans}

// BlockSpans colors the blocks of the line s: wordlists and operations. A
// block going on below is closed for the parse, so that its head is colored
// all the same.
func BlockSpans(s string) []highlight.Span {
	var nodes []template.Node
	for closing := ""; len(closing) <= 8; closing += "}" {
		var err error
		if nodes, err = request.Engine.Parse(s + closing); err == nil {
			break
		}
	}
	at := func(i int) int { return utf8.RuneCountInString(s[:min(i, len(s))]) }
	var out []highlight.Span
	add := func(a, b int, r highlight.Role) { out = append(out, highlight.Span{Start: at(a), End: at(b), Role: r}) }
	var walk func([]template.Node)
	walk = func(nodes []template.Node) {
		for _, n := range nodes {
			b, ok := n.(*template.Block)
			if !ok {
				continue
			}
			if request.IsWordlist(b.Op) {
				add(b.Pos.Start, b.Pos.End, highlight.Block)
				continue
			}
			name := b.Head.Start + len(b.Op.ID)
			add(b.Head.Start, name, highlight.OpName)
			add(name, b.Head.End, highlight.OpArgs)
			// the "|" to the next operation, or the "{"
			add(b.Head.End, b.Head.End+1, highlight.OpBrace)
			add(b.Pos.End-1, b.Pos.End, highlight.OpBrace)
			walk(b.Children)
		}
	}
	walk(nodes)
	return out
}
