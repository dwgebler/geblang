package native

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"geblang/internal/runtime"
)

func ValidateXML(text string) bool {
	return ValidateXMLDetailed(text) == nil
}

func ValidateXMLDetailed(text string) *ParseError {
	decoder := xml.NewDecoder(strings.NewReader(text))
	depth := 0
	roots := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			if roots == 1 && depth == 0 {
				return nil
			}
			parseErr := NewParseError("XML document must contain exactly one root element", text, decoder.InputOffset())
			return &parseErr
		}
		if err != nil {
			parseErr := NewParseError(err.Error(), text, decoder.InputOffset())
			if syntaxErr, ok := err.(*xml.SyntaxError); ok && syntaxErr.Line > 0 {
				parseErr.Line = int64(syntaxErr.Line)
			}
			return &parseErr
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					parseErr := NewParseError("XML document must contain exactly one root element", text, decoder.InputOffset())
					return &parseErr
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				parseErr := NewParseError(fmt.Sprintf("unexpected XML end element %s", tok.Name.Local), text, decoder.InputOffset())
				return &parseErr
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(tok)) != "" {
				parseErr := NewParseError("XML document contains non-whitespace text outside the root element", text, decoder.InputOffset())
				return &parseErr
			}
		}
	}
}

type xmlNode struct {
	name       string
	attrs      map[string]string
	children   []*xmlNode
	text       strings.Builder
	childTexts []string
}

func ParseXML(text string) (runtime.Value, *ParseError) {
	decoder := xml.NewDecoder(strings.NewReader(text))
	stack := []*xmlNode{}
	var root *xmlNode
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			if root == nil || len(stack) != 0 {
				parseErr := NewParseError("XML document must contain exactly one root element", text, decoder.InputOffset())
				return nil, &parseErr
			}
			return xmlNodeValue(root), nil
		}
		if err != nil {
			parseErr := NewParseError(err.Error(), text, decoder.InputOffset())
			if syntaxErr, ok := err.(*xml.SyntaxError); ok && syntaxErr.Line > 0 {
				parseErr.Line = int64(syntaxErr.Line)
			}
			return nil, &parseErr
		}
		switch tok := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: tok.Name.Local, attrs: map[string]string{}}
			for _, attr := range tok.Attr {
				node.attrs[attr.Name.Local] = attr.Value
			}
			if len(stack) == 0 {
				if root != nil {
					parseErr := NewParseError("XML document must contain exactly one root element", text, decoder.InputOffset())
					return nil, &parseErr
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				parseErr := NewParseError(fmt.Sprintf("unexpected XML end element %s", tok.Name.Local), text, decoder.InputOffset())
				return nil, &parseErr
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(tok)) != "" {
					parseErr := NewParseError("XML document contains non-whitespace text outside the root element", text, decoder.InputOffset())
					return nil, &parseErr
				}
				continue
			}
			stack[len(stack)-1].text.WriteString(string(tok))
		}
	}
}

func StringifyXML(value runtime.Value) (string, error) {
	var out bytes.Buffer
	if err := writeXMLValue(&out, value); err != nil {
		return "", err
	}
	return out.String(), nil
}

func writeXMLValue(out *bytes.Buffer, value runtime.Value) error {
	dict, ok := value.(runtime.Dict)
	if !ok {
		return fmt.Errorf("xml.stringify expects an element dict")
	}
	name, err := xmlStringField(dict, "name")
	if err != nil {
		return err
	}
	out.WriteByte('<')
	out.WriteString(name)
	if attrsValue, ok := dict.GetEntry(dictKey("attributes")); ok {
		if attrs, ok := attrsValue.Value.(runtime.Dict); ok {
			for _, dk := range attrs.EntryKeys() {
				entry, _ := attrs.GetEntry(dk)
				key, ok := entry.Key.(runtime.String)
				if !ok {
					continue
				}
				value, ok := entry.Value.(runtime.String)
				if !ok {
					continue
				}
				out.WriteByte(' ')
				out.WriteString(key.Value)
				out.WriteString(`="`)
				xml.EscapeText(out, []byte(value.Value))
				out.WriteByte('"')
			}
		}
	}
	out.WriteByte('>')
	if textValue, ok := dict.GetEntry(dictKey("text")); ok {
		if text, ok := textValue.Value.(runtime.String); ok {
			xml.EscapeText(out, []byte(text.Value))
		}
	}
	if childrenValue, ok := dict.GetEntry(dictKey("children")); ok {
		if children, ok := childrenValue.Value.(*runtime.List); ok {
			for _, child := range children.Elements {
				if err := writeXMLValue(out, child); err != nil {
					return err
				}
			}
		}
	}
	out.WriteString("</")
	out.WriteString(name)
	out.WriteByte('>')
	return nil
}

func xmlStringField(dict runtime.Dict, name string) (string, error) {
	entry, ok := dict.GetEntry(dictKey(name))
	if !ok {
		return "", fmt.Errorf("xml element missing %s", name)
	}
	value, ok := entry.Value.(runtime.String)
	if !ok {
		return "", fmt.Errorf("xml element %s must be string", name)
	}
	return value.Value, nil
}

func xmlNodeValue(node *xmlNode) runtime.Value {
	attrs := map[string]runtime.DictEntry{}
	for key, value := range node.attrs {
		keyValue := runtime.String{Value: key}
		attrs[dictKey(key)] = runtime.DictEntry{Key: keyValue, Value: runtime.String{Value: value}}
	}
	children := make([]runtime.Value, 0, len(node.children))
	for _, child := range node.children {
		children = append(children, xmlNodeValue(child))
	}
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey("name"):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: node.name}},
		dictKey("attributes"): {Key: runtime.String{Value: "attributes"}, Value: runtime.DictFromEntries(attrs, nil)},
		dictKey("children"):   {Key: runtime.String{Value: "children"}, Value: &runtime.List{Elements: children}},
		dictKey("text"):       {Key: runtime.String{Value: "text"}, Value: runtime.String{Value: strings.TrimSpace(node.text.String())}},
	}, nil)
}
