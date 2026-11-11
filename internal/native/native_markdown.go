package native

import (
	"bytes"
	"fmt"
	"geblang/internal/runtime"
	"strings"

	"github.com/yuin/goldmark"
	goldast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	goldparser "github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	goldtext "github.com/yuin/goldmark/text"
)

var gfmMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(goldparser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(goldhtml.WithUnsafe()),
)

func registerMarkdown(r *Registry) {
	r.Register("markdown", "parse", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleStringArg(args, "markdown.parse")
		if err != nil {
			return nil, err
		}
		return markdownParse(text), nil
	})
	r.Register("markdown", "renderHtml", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleStringArg(args, "markdown.renderHtml")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: markdownRenderHTML(text)}, nil
	})
	r.Register("markdown", "stripText", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleStringArg(args, "markdown.stripText")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: markdownStripText(text)}, nil
	})
}

func singleStringArg(args []runtime.Value, label string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s expects one string argument", label)
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s argument must be string", label)
	}
	return text.Value, nil
}

func markdownRenderHTML(src string) string {
	var buf bytes.Buffer
	_ = gfmMarkdown.Convert([]byte(src), &buf)
	return buf.String()
}

func markdownParse(src string) *runtime.List {
	reader := goldtext.NewReader([]byte(src))
	doc := gfmMarkdown.Parser().Parse(reader)
	source := []byte(src)
	var results []runtime.Value
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if block, ok := goldmarkNodeToBlock(n, source); ok {
			results = append(results, block)
		}
	}
	if results == nil {
		results = []runtime.Value{}
	}
	return &runtime.List{Elements: results}
}

func markdownStripText(src string) string {
	reader := goldtext.NewReader([]byte(src))
	doc := gfmMarkdown.Parser().Parse(reader)
	source := []byte(src)
	var parts []string
	_ = goldast.Walk(doc, func(n goldast.Node, entering bool) (goldast.WalkStatus, error) {
		if !entering {
			return goldast.WalkContinue, nil
		}
		if t, ok := n.(*goldast.Text); ok {
			parts = append(parts, string(t.Segment.Value(source)))
		}
		return goldast.WalkContinue, nil
	})
	return strings.Join(parts, "")
}

func putMarkdownDict(entries map[string]runtime.DictEntry, key string, value runtime.Value) {
	keyValue := runtime.String{Value: key}
	entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
}

func goldmarkNodeToBlock(n goldast.Node, source []byte) (runtime.Dict, bool) {
	entries := map[string]runtime.DictEntry{}
	switch n.Kind() {
	case goldast.KindHeading:
		heading := n.(*goldast.Heading)
		putMarkdownDict(entries, "type", runtime.String{Value: "heading"})
		putMarkdownDict(entries, "level", runtime.NewInt64(int64(heading.Level)))
		putMarkdownDict(entries, "text", runtime.String{Value: goldmarkNodeText(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindParagraph:
		putMarkdownDict(entries, "type", runtime.String{Value: "paragraph"})
		putMarkdownDict(entries, "text", runtime.String{Value: goldmarkNodeText(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindFencedCodeBlock:
		fcb := n.(*goldast.FencedCodeBlock)
		lang := ""
		if l := fcb.Language(source); l != nil {
			lang = string(l)
		}
		putMarkdownDict(entries, "type", runtime.String{Value: "code"})
		putMarkdownDict(entries, "lang", runtime.String{Value: lang})
		putMarkdownDict(entries, "code", runtime.String{Value: goldmarkCodeContent(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindCodeBlock:
		putMarkdownDict(entries, "type", runtime.String{Value: "code"})
		putMarkdownDict(entries, "lang", runtime.String{Value: ""})
		putMarkdownDict(entries, "code", runtime.String{Value: goldmarkCodeContent(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindList:
		return goldmarkListToBlock(n.(*goldast.List), source), true
	case extast.KindTable:
		return goldmarkTableToBlock(n, source), true
	case goldast.KindBlockquote:
		putMarkdownDict(entries, "type", runtime.String{Value: "blockquote"})
		putMarkdownDict(entries, "text", runtime.String{Value: goldmarkNodeText(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindThematicBreak:
		putMarkdownDict(entries, "type", runtime.String{Value: "hr"})
		return runtime.DictFromEntries(entries, nil), true
	case goldast.KindHTMLBlock:
		putMarkdownDict(entries, "type", runtime.String{Value: "html"})
		putMarkdownDict(entries, "html", runtime.String{Value: goldmarkCodeContent(n, source)})
		return runtime.DictFromEntries(entries, nil), true
	}
	return runtime.Dict{}, false
}

func goldmarkNodeText(n goldast.Node, source []byte) string {
	var parts []string
	_ = goldast.Walk(n, func(child goldast.Node, entering bool) (goldast.WalkStatus, error) {
		if !entering {
			return goldast.WalkContinue, nil
		}
		if t, ok := child.(*goldast.Text); ok {
			parts = append(parts, string(t.Segment.Value(source)))
		}
		return goldast.WalkContinue, nil
	})
	return strings.Join(parts, "")
}

func goldmarkCodeContent(n goldast.Node, source []byte) string {
	lines := n.Lines()
	var sb strings.Builder
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		sb.Write(line.Value(source))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func goldmarkListToBlock(list *goldast.List, source []byte) runtime.Dict {
	entries := map[string]runtime.DictEntry{}

	isTaskBlock := func(n goldast.Node) bool {
		return n.Kind() == goldast.KindParagraph || n.Kind() == goldast.KindTextBlock
	}

	isTask := false
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if item.Kind() == goldast.KindListItem {
			for para := item.FirstChild(); para != nil; para = para.NextSibling() {
				if isTaskBlock(para) && para.FirstChild() != nil {
					if para.FirstChild().Kind() == extast.KindTaskCheckBox {
						isTask = true
						break
					}
				}
			}
		}
		if isTask {
			break
		}
	}

	if isTask {
		putMarkdownDict(entries, "type", runtime.String{Value: "task_list"})
		var items []runtime.Value
		for item := list.FirstChild(); item != nil; item = item.NextSibling() {
			if item.Kind() != goldast.KindListItem {
				continue
			}
			checked := false
			for para := item.FirstChild(); para != nil; para = para.NextSibling() {
				if isTaskBlock(para) && para.FirstChild() != nil {
					if cb, ok := para.FirstChild().(*extast.TaskCheckBox); ok {
						checked = cb.IsChecked
					}
				}
			}
			text := strings.TrimSpace(goldmarkNodeText(item, source))
			itemEntries := map[string]runtime.DictEntry{}
			putMarkdownDict(itemEntries, "text", runtime.String{Value: text})
			putMarkdownDict(itemEntries, "checked", runtime.Bool{Value: checked})
			items = append(items, runtime.DictFromEntries(itemEntries, nil))
		}
		if items == nil {
			items = []runtime.Value{}
		}
		putMarkdownDict(entries, "items", &runtime.List{Elements: items})
	} else if list.IsOrdered() {
		putMarkdownDict(entries, "type", runtime.String{Value: "ordered_list"})
		var items []runtime.Value
		for item := list.FirstChild(); item != nil; item = item.NextSibling() {
			if item.Kind() != goldast.KindListItem {
				continue
			}
			items = append(items, runtime.String{Value: strings.TrimSpace(goldmarkNodeText(item, source))})
		}
		if items == nil {
			items = []runtime.Value{}
		}
		putMarkdownDict(entries, "items", &runtime.List{Elements: items})
	} else {
		putMarkdownDict(entries, "type", runtime.String{Value: "list"})
		var items []runtime.Value
		for item := list.FirstChild(); item != nil; item = item.NextSibling() {
			if item.Kind() != goldast.KindListItem {
				continue
			}
			items = append(items, runtime.String{Value: strings.TrimSpace(goldmarkNodeText(item, source))})
		}
		if items == nil {
			items = []runtime.Value{}
		}
		putMarkdownDict(entries, "items", &runtime.List{Elements: items})
	}

	return runtime.DictFromEntries(entries, nil)
}

func goldmarkTableToBlock(n goldast.Node, source []byte) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putMarkdownDict(entries, "type", runtime.String{Value: "table"})

	var headers []runtime.Value
	var rows []runtime.Value

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.Kind() {
		case extast.KindTableHeader:
			for cell := child.FirstChild(); cell != nil; cell = cell.NextSibling() {
				if cell.Kind() == extast.KindTableCell {
					headers = append(headers, runtime.String{Value: strings.TrimSpace(goldmarkNodeText(cell, source))})
				}
			}
		case extast.KindTableRow:
			var cells []runtime.Value
			for cell := child.FirstChild(); cell != nil; cell = cell.NextSibling() {
				if cell.Kind() == extast.KindTableCell {
					cells = append(cells, runtime.String{Value: strings.TrimSpace(goldmarkNodeText(cell, source))})
				}
			}
			if cells == nil {
				cells = []runtime.Value{}
			}
			rows = append(rows, &runtime.List{Elements: cells})
		}
	}

	if headers == nil {
		headers = []runtime.Value{}
	}
	if rows == nil {
		rows = []runtime.Value{}
	}

	putMarkdownDict(entries, "headers", &runtime.List{Elements: headers})
	putMarkdownDict(entries, "rows", &runtime.List{Elements: rows})
	return runtime.DictFromEntries(entries, nil)
}
