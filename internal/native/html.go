package native

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"geblang/internal/runtime"
)

// HtmlNodeMethods is the dispatch surface of an html.Node, read by reflect/dir and the catalog guard.
var HtmlNodeMethods = []string{
	"select", "selectFirst", "text", "attr", "attrs", "tag", "html", "children", "parent",
}

func registerHTML(r *Registry) {
	r.Register("html", "parse", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("html.parse expects one string argument")
		}
		src, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("html.parse expects a string, got %s", args[0].TypeName())
		}
		doc, err := html.Parse(strings.NewReader(src.Value))
		if err != nil {
			return nil, fmt.Errorf("html.parse: %w", err)
		}
		return wrapHTMLNode(doc), nil
	})
}

func wrapHTMLNode(n *html.Node) *runtime.HtmlNode {
	return &runtime.HtmlNode{Node: n, Tag: htmlNodeTag(n)}
}

func htmlNodeTag(n *html.Node) string {
	switch n.Type {
	case html.ElementNode:
		return n.Data
	case html.DocumentNode:
		return "#document"
	default:
		return ""
	}
}

func htmlNodeFrom(recv *runtime.HtmlNode) (*html.Node, error) {
	n, ok := recv.Node.(*html.Node)
	if !ok || n == nil {
		return nil, fmt.Errorf("html.Node: invalid node handle")
	}
	return n, nil
}

// HtmlNodeMethod dispatches a method call on an html.Node for both backends.
func HtmlNodeMethod(recv *runtime.HtmlNode, name string, args []runtime.Value) (runtime.Value, error) {
	n, err := htmlNodeFrom(recv)
	if err != nil {
		return nil, err
	}
	switch name {
	case "select":
		return htmlSelect(n, args, false)
	case "selectFirst":
		return htmlSelect(n, args, true)
	case "text":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.text expects no arguments")
		}
		return runtime.String{Value: htmlNodeText(n)}, nil
	case "attr":
		return htmlNodeAttr(n, args)
	case "attrs":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.attrs expects no arguments")
		}
		return htmlNodeAttrs(n), nil
	case "tag":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.tag expects no arguments")
		}
		return runtime.String{Value: htmlNodeTag(n)}, nil
	case "html":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.html expects no arguments")
		}
		return runtime.String{Value: htmlNodeInner(n)}, nil
	case "children":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.children expects no arguments")
		}
		return htmlNodeChildren(n), nil
	case "parent":
		if len(args) != 0 {
			return nil, fmt.Errorf("html.Node.parent expects no arguments")
		}
		if n.Parent == nil {
			return runtime.Null{}, nil
		}
		return wrapHTMLNode(n.Parent), nil
	}
	return nil, fmt.Errorf("html.Node has no method %s", name)
}

func htmlSelect(n *html.Node, args []runtime.Value, first bool) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("html.Node.select expects one selector string")
	}
	selStr, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("html.Node.select expects a string selector, got %s", args[0].TypeName())
	}
	sel, err := cascadia.Compile(selStr.Value)
	if err != nil {
		return nil, fmt.Errorf("html.Node.select: invalid selector %q: %w", selStr.Value, err)
	}
	matches := sel.MatchAll(n)
	if first {
		if len(matches) == 0 {
			return runtime.Null{}, nil
		}
		return wrapHTMLNode(matches[0]), nil
	}
	elements := make([]runtime.Value, len(matches))
	for i, m := range matches {
		elements[i] = wrapHTMLNode(m)
	}
	return &runtime.List{Elements: elements}, nil
}

func htmlNodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for child := c.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func htmlNodeAttr(n *html.Node, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("html.Node.attr expects one name string")
	}
	name, ok := args[0].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("html.Node.attr expects a string name, got %s", args[0].TypeName())
	}
	for _, a := range n.Attr {
		if a.Key == name.Value {
			return runtime.String{Value: a.Val}, nil
		}
	}
	return runtime.Null{}, nil
}

func htmlNodeAttrs(n *html.Node) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	for _, a := range n.Attr {
		key := runtime.String{Value: a.Key}
		entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: a.Val}}
	}
	return runtime.DictFromEntries(entries, nil)
}

func htmlNodeInner(n *html.Node) string {
	var b bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c)
	}
	return b.String()
}

func htmlNodeChildren(n *html.Node) runtime.Value {
	elements := []runtime.Value{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			elements = append(elements, wrapHTMLNode(c))
		}
	}
	return &runtime.List{Elements: elements}
}
