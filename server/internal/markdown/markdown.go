// Package markdown preserves authoritative Markdown and produces a closed,
// non-HTML display tree. Never pass a goldmark HTML renderer to a browser.
package markdown

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	htmlwriter "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

const Format = "nexus-markdown-v1"
const ViewFormat = "nexus-markdown-view-v1"
const MaxBytes = 262144
const MaxNodes = 20000
const MaxDepth = 32

type Node struct {
	Kind     string `json:"kind"`
	Text     string `json:"text,omitempty"`
	URL      string `json:"url,omitempty"`
	Level    int    `json:"level,omitempty"`
	Start    int    `json:"start,omitempty"`
	Ordered  bool   `json:"ordered,omitempty"`
	Children []Node `json:"children,omitempty"`
}
type View struct {
	Format string `json:"format"`
	Nodes  []Node `json:"nodes"`
}

// Diagnostic contains a fixed code and byte offset, never user source text.
type Diagnostic struct {
	Code   string `json:"code"`
	Offset int    `json:"offset"`
}

func (d *Diagnostic) Error() string { return fmt.Sprintf("Markdown %s at byte %d", d.Code, d.Offset) }
func (d *Diagnostic) Unwrap() error { return authz.ErrInvalid }

func Normalize(body, format string) (string, error) {
	if format != Format {
		return "", &Diagnostic{Code: "unsupported_format"}
	}
	if !utf8.ValidString(body) || strings.ContainsRune(body, 0) || len(body) > MaxBytes {
		return "", &Diagnostic{Code: "invalid_source"}
	}
	return strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), nil
}

func ValidURL(value string) bool {
	if strings.ContainsAny(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return false
	}
	// Percent-encoded control characters and backslashes are rejected as well.
	decoded, err := url.PathUnescape(value)
	return err == nil && !strings.ContainsRune(decoded, '\\') && strings.IndexFunc(decoded, unicode.IsControl) < 0
}

// decodeText uses goldmark's CommonMark escaping rules for text only, then
// removes the HTML escaping. The intermediate bytes never leave this function.
func decodeText(value []byte) string {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	htmlwriter.NewWriter().Write(w, value)
	_ = w.Flush() // bytes.Buffer cannot fail.
	return html.UnescapeString(b.String())
}

func Parse(body, format string) (view View, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if limit, ok := recovered.(parseLimit); ok {
				view = View{}
				err = &Diagnostic{Code: "parser_limit", Offset: limit.offset}
			} else {
				panic(recovered)
			}
		}
	}()
	normalized, err := Normalize(body, format)
	if err != nil {
		return View{}, err
	}
	source := []byte(normalized)
	budget := 0
	blocks := parser.DefaultBlockParsers()
	for i, entry := range blocks {
		blocks[i] = util.Prioritized(&boundedBlock{BlockParser: entry.Value.(parser.BlockParser), budget: &budget}, entry.Priority)
	}
	inlines := parser.DefaultInlineParsers()
	for i, entry := range inlines {
		inlines[i] = util.Prioritized(&boundedInline{InlineParser: entry.Value.(parser.InlineParser), budget: &budget}, entry.Priority)
	}
	root := parser.NewParser(parser.WithBlockParsers(blocks...), parser.WithInlineParsers(inlines...), parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...)).Parse(text.NewReader(source))
	count := 0
	var convert func(ast.Node, int) ([]Node, error)
	convert = func(n ast.Node, depth int) ([]Node, error) {
		count++
		if count > MaxNodes || depth > MaxDepth {
			return nil, &Diagnostic{Code: "projection_limit", Offset: offset(n)}
		}
		v := Node{}
		leaf := false
		switch n := n.(type) {
		case *ast.Document:
			v.Kind = "root"
		case *ast.Paragraph:
			v.Kind = "paragraph"
		case *ast.TextBlock:
			v.Kind = "group"
		case *ast.Heading:
			v.Kind = "heading"
			v.Level = n.Level
		case *ast.Blockquote:
			v.Kind = "quote"
		case *ast.List:
			v.Kind = "list"
			v.Ordered = n.IsOrdered()
			if v.Ordered {
				v.Start = n.Start
			}
		case *ast.ListItem:
			v.Kind = "item"
		case *ast.ThematicBreak:
			v.Kind = "rule"
			leaf = true
		case *ast.Emphasis:
			v.Kind = "emphasis"
			if n.Level == 2 {
				v.Kind = "strong"
			}
		case *ast.CodeBlock:
			v.Kind = "code_block"
			v.Text = string(n.Lines().Value(source))
			leaf = true
		case *ast.FencedCodeBlock:
			v.Kind = "code_block"
			v.Text = string(n.Lines().Value(source))
			leaf = true
		case *ast.CodeSpan:
			v.Kind = "code"
			leaf = true
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				t, ok := c.(*ast.Text)
				if !ok {
					return nil, &Diagnostic{Code: "unknown_node", Offset: offset(c)}
				}
				value := t.Segment.Value(source)
				v.Text += string(value)
				if t.SoftLineBreak() {
					v.Text += " "
				}
			}
		case *ast.Text:
			v.Kind = "text"
			leaf = true
			v.Text = string(n.Segment.Value(source))
			if !n.IsRaw() {
				v.Text = decodeText(n.Segment.Value(source))
			}
			if n.HardLineBreak() {
				count++
				return []Node{v, {Kind: "break"}}, nil
			}
			if n.SoftLineBreak() {
				v.Text += "\n"
			}
		case *ast.String:
			v.Kind = "text"
			v.Text = decodeText(n.Value)
			leaf = true
		case *ast.Link:
			v.Kind = "link"
			v.URL = decodeText(n.Destination)
			if !ValidURL(v.URL) {
				return nil, &Diagnostic{Code: "unsafe_url", Offset: offset(n)}
			}
		case *ast.AutoLink:
			v.Kind = "link"
			v.URL = string(n.URL(source))
			leaf = true
			if !ValidURL(v.URL) {
				return nil, &Diagnostic{Code: "unsafe_url", Offset: offset(n)}
			}
			v.Children = []Node{{Kind: "text", Text: string(n.Label(source))}}
			count++
		case *ast.Image:
			return nil, &Diagnostic{Code: "image_forbidden", Offset: offset(n)}
		case *ast.HTMLBlock, *ast.RawHTML:
			return nil, &Diagnostic{Code: "html_forbidden", Offset: offset(n)}
		default:
			return nil, &Diagnostic{Code: "unknown_node", Offset: offset(n)}
		}
		if !leaf {
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				children, err := convert(c, depth+1)
				if err != nil {
					return nil, err
				}
				v.Children = append(v.Children, children...)
			}
		}
		if count > MaxNodes {
			return nil, &Diagnostic{Code: "projection_limit", Offset: offset(n)}
		}
		if v.Kind == "root" {
			if v.Children == nil {
				v.Children = []Node{}
			}
			return v.Children, nil
		}
		return []Node{v}, nil
	}
	nodes, err := convert(root, 0)
	if err != nil {
		return View{}, err
	}
	return View{Format: ViewFormat, Nodes: nodes}, nil
}

// Goldmark does not expose an error-returning cancellation hook. Only our
// private limit signal is recovered; all unexpected parser panics propagate.
type parseLimit struct{ offset int }

func consume(budget *int, reader text.Reader) {
	*budget++
	if *budget > MaxNodes {
		_, segment := reader.Position()
		panic(parseLimit{segment.Start})
	}
}

type boundedBlock struct {
	parser.BlockParser
	budget *int
}

func (b *boundedBlock) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	consume(b.budget, reader)
	if len(pc.OpenedBlocks()) >= MaxDepth {
		_, segment := reader.Position()
		panic(parseLimit{segment.Start})
	}
	return b.BlockParser.Open(parent, reader, pc)
}

type boundedInline struct {
	parser.InlineParser
	budget *int
}

func (b *boundedInline) Parse(parent ast.Node, reader text.Reader, pc parser.Context) ast.Node {
	consume(b.budget, reader)
	return b.InlineParser.Parse(parent, reader, pc)
}
func (b *boundedInline) CloseBlock(parent ast.Node, reader text.Reader, pc parser.Context) {
	if closer, ok := b.InlineParser.(parser.CloseBlocker); ok {
		closer.CloseBlock(parent, reader, pc)
	}
}

func offset(n ast.Node) int {
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start
	}
	if t, ok := n.(*ast.RawHTML); ok && t.Segments.Len() > 0 {
		return t.Segments.At(0).Start
	}
	if n.Type() == ast.TypeBlock && n.Lines().Len() > 0 {
		return n.Lines().At(0).Start
	}
	if n.FirstChild() != nil {
		return offset(n.FirstChild())
	}
	if n.Parent() != nil && n.Parent().Type() == ast.TypeBlock && n.Parent().Lines().Len() > 0 {
		return n.Parent().Lines().At(0).Start
	}
	return 0
}
