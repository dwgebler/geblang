package evaluator

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
	"io"
	"strconv"
	"strings"

	yamllib "gopkg.in/yaml.v3"
)

func (e *Evaluator) jsonStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects source and handler", call.Callee.String())
	}
	handler, ok := args[1].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s handler must be an object implementing JsonStreamInterface", call.Callee.String())
	}
	readerValue, err := e.jsonReader(call, []runtime.Value{args[0]})
	if err != nil {
		return nil, err
	}
	reader, ok := readerValue.(runtime.NativeObject)
	if !ok {
		return nil, fmt.Errorf("internal error: json reader returned unexpected type")
	}
	defer e.closeJSONReader(reader.ID)
	count := int64(0)
	for {
		stream, err := e.lookupJSONReader(reader.ID)
		if err != nil {
			return nil, err
		}
		ok, err := e.jsonReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		event := stream.pending
		stream.pending = nil
		count++
		stop, err := e.dispatchJSONStreamEvent(handler, event)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}
	return runtime.NewInt64(count), nil
}

func (e *Evaluator) csvStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects source and handler", call.Callee.String())
	}
	handler, ok := args[1].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s handler must be an object implementing CsvStreamInterface", call.Callee.String())
	}
	readerValue, err := e.csvReader(call, []runtime.Value{args[0]})
	if err != nil {
		return nil, err
	}
	reader, ok := readerValue.(runtime.NativeObject)
	if !ok {
		return nil, fmt.Errorf("internal error: csv reader returned unexpected type")
	}
	defer e.closeCSVReader(reader.ID)
	count := int64(0)
	for {
		stream, err := e.lookupCSVReader(reader.ID)
		if err != nil {
			return nil, err
		}
		ok, err := e.csvReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		event := stream.pending
		stream.pending = nil
		count++
		stop, err := e.dispatchCSVStreamEvent(handler, event)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}
	return runtime.NewInt64(count), nil
}

func (e *Evaluator) yamlStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects source and handler", call.Callee.String())
	}
	handler, ok := args[1].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s handler must be an object implementing YamlStreamInterface", call.Callee.String())
	}
	readerValue, err := e.yamlReader(call, []runtime.Value{args[0]})
	if err != nil {
		return nil, err
	}
	reader, ok := readerValue.(runtime.NativeObject)
	if !ok {
		return nil, fmt.Errorf("internal error: yaml reader returned unexpected type")
	}
	defer e.closeYAMLReader(reader.ID)
	count := int64(0)
	for {
		stream, err := e.lookupYAMLReader(reader.ID)
		if err != nil {
			return nil, err
		}
		ok, err := e.yamlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		event := stream.pending
		stream.pending = nil
		count++
		stop, err := e.dispatchYAMLStreamEvent(handler, event)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}
	return runtime.NewInt64(count), nil
}

func (e *Evaluator) yamlReader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	source, err := e.streamSourceReader(call, args[0])
	if err != nil {
		return nil, err
	}
	decoder := yamllib.NewDecoder(source.Reader)
	e.yamlMu.Lock()
	defer e.yamlMu.Unlock()
	e.nextYAMLID++
	id := e.nextYAMLID
	ensureMap(&e.yamlReaders)[id] = &yamlStreamReader{decoder: decoder}
	return runtime.NativeObject{Kind: "YamlReader", ID: id}, nil
}

func (e *Evaluator) closeYAMLReader(id int64) {
	e.yamlMu.Lock()
	delete(e.yamlReaders, id)
	e.yamlMu.Unlock()
}

func (e *Evaluator) yamlReaderMethod(reader runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	if reader.Kind != "YamlReader" {
		return nil, fmt.Errorf("%s has no method %s", reader.TypeName(), name)
	}
	if name == "close" {
		if len(args) != 0 {
			return nil, fmt.Errorf("YamlReader.close expects no arguments")
		}
		e.closeYAMLReader(reader.ID)
		return runtime.Null{}, nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("YamlReader.%s expects no arguments", name)
	}
	stream, err := e.lookupYAMLReader(reader.ID)
	if err != nil {
		return nil, err
	}
	switch name {
	case "hasNext":
		ok, err := e.yamlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: ok}, nil
	case "next":
		ok, err := e.yamlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			return runtime.Null{}, nil
		}
		event := stream.pending
		stream.pending = nil
		return event, nil
	default:
		return nil, fmt.Errorf("YamlReader has no method %s", name)
	}
}

func (e *Evaluator) lookupYAMLReader(id int64) (*yamlStreamReader, error) {
	e.yamlMu.Lock()
	reader, ok := e.yamlReaders[id]
	e.yamlMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.lookupYAMLReader(id)
	}
	if !ok {
		return nil, fmt.Errorf("YamlReader is closed")
	}
	return reader, nil
}

func (e *Evaluator) yamlReaderHasNext(reader *yamlStreamReader) (bool, error) {
	if reader.pending != nil {
		return true, nil
	}
	if reader.done {
		return false, nil
	}
	event := nextYAMLEvent(reader)
	if event == nil {
		reader.done = true
		return false, nil
	}
	reader.pending = event
	return true, nil
}

func nextYAMLEvent(reader *yamlStreamReader) runtime.Value {
	for len(reader.queue) == 0 {
		var node yamllib.Node
		if err := reader.decoder.Decode(&node); err == io.EOF {
			return nil
		} else if err != nil {
			reader.done = true
			return yamlEvent("error", native.ParseErrorValue(yamlParseError(err)))
		}
		enqueueYAMLEvents(reader, &node)
	}
	event := reader.queue[0]
	reader.queue[0] = nil
	reader.queue = reader.queue[1:]
	return event
}

func enqueueYAMLEvents(reader *yamlStreamReader, node *yamllib.Node) {
	enqueueYAMLEventsVisited(reader, node, map[*yamllib.Node]bool{})
}

func enqueueYAMLEventsVisited(reader *yamlStreamReader, node *yamllib.Node, visited map[*yamllib.Node]bool) {
	if reader.done {
		return
	}
	if node == nil {
		reader.queue = append(reader.queue, yamlEvent("value", runtime.Null{}))
		return
	}
	if node.Kind == yamllib.DocumentNode {
		if len(node.Content) == 0 {
			return
		}
		enqueueYAMLEventsVisited(reader, node.Content[0], visited)
		return
	}
	switch node.Kind {
	case yamllib.MappingNode:
		reader.queue = append(reader.queue, yamlEvent("startMap", runtime.Null{}))
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := yamlKeyText(node.Content[i])
			reader.queue = append(reader.queue, yamlEvent("key", runtime.String{Value: key}))
			enqueueYAMLEventsVisited(reader, node.Content[i+1], visited)
		}
		reader.queue = append(reader.queue, yamlEvent("endMap", runtime.Null{}))
	case yamllib.SequenceNode:
		reader.queue = append(reader.queue, yamlEvent("startList", runtime.Null{}))
		for _, item := range node.Content {
			enqueueYAMLEventsVisited(reader, item, visited)
		}
		reader.queue = append(reader.queue, yamlEvent("endList", runtime.Null{}))
	case yamllib.ScalarNode:
		reader.queue = append(reader.queue, yamlEvent("value", yamlScalarValue(node)))
	case yamllib.AliasNode:
		if visited[node.Alias] {
			parseErr := native.NewParseError("yaml: alias cycle detected", "", -1)
			reader.queue = append(reader.queue, yamlEvent("error", native.ParseErrorValue(parseErr)))
			reader.done = true
			return
		}
		visited[node.Alias] = true
		enqueueYAMLEventsVisited(reader, node.Alias, visited)
	default:
		reader.queue = append(reader.queue, yamlEvent("value", runtime.Null{}))
	}
}

func yamlKeyText(node *yamllib.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yamllib.ScalarNode {
		return node.Value
	}
	value := yamlScalarValue(node)
	return value.Inspect()
}

func yamlScalarValue(node *yamllib.Node) runtime.Value {
	switch node.Tag {
	case "!!null":
		return runtime.Null{}
	case "!!bool":
		return runtime.Bool{Value: strings.EqualFold(node.Value, "true")}
	case "!!int":
		value, err := runtime.NewIntLiteral(strings.ReplaceAll(node.Value, "_", ""))
		if err == nil {
			return value
		}
		// Malformed !!int literal (e.g. non-numeric YAML tag): fall through to string.
	case "!!float":
		value, err := strconv.ParseFloat(strings.ReplaceAll(node.Value, "_", ""), 64)
		if err == nil {
			return runtime.Float{Value: value}
		}
		// Malformed !!float literal: fall through to string.
	}
	return runtime.String{Value: node.Value}
}

func yamlParseError(err error) native.ParseError {
	parseErr := native.NewParseError(err.Error(), "", -1)
	if yamlErr, ok := err.(*yamllib.TypeError); ok && len(yamlErr.Errors) > 0 {
		parseErr.Message = strings.Join(yamlErr.Errors, "; ")
	}
	return parseErr
}

func yamlEvent(kind string, value runtime.Value) runtime.Value {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.String{Value: "type"}):  {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: kind}},
		dictKey(runtime.String{Value: "value"}): {Key: runtime.String{Value: "value"}, Value: value},
	}, nil)
}

func (e *Evaluator) dispatchYAMLStreamEvent(handler *runtime.Instance, event runtime.Value) (bool, error) {
	eventDict, ok := event.(runtime.Dict)
	if !ok {
		return false, fmt.Errorf("YAML stream event must be dict")
	}
	eventType, ok := dictStringField(eventDict, "type")
	if !ok {
		return false, fmt.Errorf("YAML stream event is missing type")
	}
	value, _ := dictField(eventDict, "value")
	switch eventType {
	case "startMap":
		return false, e.callStreamHandler(handler, "onStartMap", nil)
	case "endMap":
		return false, e.callStreamHandler(handler, "onEndMap", nil)
	case "startList":
		return false, e.callStreamHandler(handler, "onStartList", nil)
	case "endList":
		return false, e.callStreamHandler(handler, "onEndList", nil)
	case "key":
		return false, e.callStreamHandler(handler, "onKey", []runtime.Value{value})
	case "value":
		return false, e.callStreamHandler(handler, "onValue", []runtime.Value{value})
	case "error":
		return true, e.callStreamHandler(handler, "onError", []runtime.Value{value})
	default:
		return false, fmt.Errorf("unknown YAML stream event %q", eventType)
	}
}

func (e *Evaluator) csvReader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	source, err := e.streamSourceReader(call, args[0])
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(source.Reader)
	e.csvMu.Lock()
	defer e.csvMu.Unlock()
	e.nextCSVID++
	id := e.nextCSVID
	ensureMap(&e.csvReaders)[id] = &csvStreamReader{reader: reader}
	return runtime.NativeObject{Kind: "CsvReader", ID: id}, nil
}

func (e *Evaluator) closeCSVReader(id int64) {
	e.csvMu.Lock()
	delete(e.csvReaders, id)
	e.csvMu.Unlock()
}

func (e *Evaluator) csvReaderMethod(reader runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	if reader.Kind != "CsvReader" {
		return nil, fmt.Errorf("%s has no method %s", reader.TypeName(), name)
	}
	if name == "close" {
		if len(args) != 0 {
			return nil, fmt.Errorf("CsvReader.close expects no arguments")
		}
		e.closeCSVReader(reader.ID)
		return runtime.Null{}, nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("CsvReader.%s expects no arguments", name)
	}
	stream, err := e.lookupCSVReader(reader.ID)
	if err != nil {
		return nil, err
	}
	switch name {
	case "hasNext":
		ok, err := e.csvReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: ok}, nil
	case "next":
		ok, err := e.csvReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			return runtime.Null{}, nil
		}
		event := stream.pending
		stream.pending = nil
		return event, nil
	default:
		return nil, fmt.Errorf("CsvReader has no method %s", name)
	}
}

func (e *Evaluator) lookupCSVReader(id int64) (*csvStreamReader, error) {
	e.csvMu.Lock()
	reader, ok := e.csvReaders[id]
	e.csvMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.lookupCSVReader(id)
	}
	if !ok {
		return nil, fmt.Errorf("CsvReader is closed")
	}
	return reader, nil
}

func (e *Evaluator) csvReaderHasNext(reader *csvStreamReader) (bool, error) {
	if reader.pending != nil {
		return true, nil
	}
	if reader.done {
		return false, nil
	}
	event := nextCSVEvent(reader)
	if event == nil {
		reader.done = true
		return false, nil
	}
	reader.pending = event
	return true, nil
}

func nextCSVEvent(reader *csvStreamReader) runtime.Value {
	record, err := reader.reader.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		reader.done = true
		return csvEvent("error", native.ParseErrorValue(csvParseError(err)))
	}
	reader.row++
	if reader.row == 1 {
		return csvEvent("header", stringListValue(record))
	}
	return csvEvent("row", stringListValue(record))
}

func csvParseError(err error) native.ParseError {
	parseErr := native.NewParseError(err.Error(), "", -1)
	if csvErr, ok := err.(*csv.ParseError); ok {
		parseErr.Line = int64(csvErr.Line)
		parseErr.Column = int64(csvErr.Column)
	}
	return parseErr
}

func csvEvent(kind string, value runtime.Value) runtime.Value {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.String{Value: "type"}):  {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: kind}},
		dictKey(runtime.String{Value: "value"}): {Key: runtime.String{Value: "value"}, Value: value},
	}, nil)
}

func stringListValue(values []string) runtime.Value {
	elements := make([]runtime.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, runtime.String{Value: value})
	}
	return &runtime.List{Elements: elements}
}

func (e *Evaluator) dispatchCSVStreamEvent(handler *runtime.Instance, event runtime.Value) (bool, error) {
	eventDict, ok := event.(runtime.Dict)
	if !ok {
		return false, fmt.Errorf("CSV stream event must be dict")
	}
	eventType, ok := dictStringField(eventDict, "type")
	if !ok {
		return false, fmt.Errorf("CSV stream event is missing type")
	}
	value, _ := dictField(eventDict, "value")
	switch eventType {
	case "header":
		return false, e.callStreamHandler(handler, "onHeader", []runtime.Value{value})
	case "row":
		return false, e.callStreamHandler(handler, "onRow", []runtime.Value{value})
	case "error":
		return true, e.callStreamHandler(handler, "onError", []runtime.Value{value})
	default:
		return false, fmt.Errorf("unknown CSV stream event %q", eventType)
	}
}

func (e *Evaluator) xmlStream(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects source and handler", call.Callee.String())
	}
	handler, ok := args[1].(*runtime.Instance)
	if !ok {
		return nil, fmt.Errorf("%s handler must be an object implementing XmlStreamInterface", call.Callee.String())
	}
	readerValue, err := e.xmlReader(call, []runtime.Value{args[0]})
	if err != nil {
		return nil, err
	}
	reader, ok := readerValue.(runtime.NativeObject)
	if !ok {
		return nil, fmt.Errorf("internal error: xml reader returned unexpected type")
	}
	defer e.closeXMLReader(reader.ID)
	count := int64(0)
	for {
		stream, err := e.lookupXMLReader(reader.ID)
		if err != nil {
			return nil, err
		}
		ok, err := e.xmlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		event := stream.pending
		stream.pending = nil
		count++
		stop, err := e.dispatchXMLStreamEvent(handler, event)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}
	return runtime.NewInt64(count), nil
}

func (e *Evaluator) xmlReader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	source, err := e.streamSourceReader(call, args[0])
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(source.Reader)
	e.xmlMu.Lock()
	defer e.xmlMu.Unlock()
	e.nextXMLID++
	id := e.nextXMLID
	ensureMap(&e.xmlReaders)[id] = &xmlStreamReader{decoder: decoder, source: source.Text}
	return runtime.NativeObject{Kind: "XmlReader", ID: id}, nil
}

func (e *Evaluator) closeXMLReader(id int64) {
	e.xmlMu.Lock()
	delete(e.xmlReaders, id)
	e.xmlMu.Unlock()
}

func (e *Evaluator) xmlReaderMethod(reader runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	if reader.Kind != "XmlReader" {
		return nil, fmt.Errorf("%s has no method %s", reader.TypeName(), name)
	}
	if name == "close" {
		if len(args) != 0 {
			return nil, fmt.Errorf("XmlReader.close expects no arguments")
		}
		e.closeXMLReader(reader.ID)
		return runtime.Null{}, nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("XmlReader.%s expects no arguments", name)
	}
	stream, err := e.lookupXMLReader(reader.ID)
	if err != nil {
		return nil, err
	}
	switch name {
	case "hasNext":
		ok, err := e.xmlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: ok}, nil
	case "next":
		ok, err := e.xmlReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			return runtime.Null{}, nil
		}
		event := stream.pending
		stream.pending = nil
		return event, nil
	default:
		return nil, fmt.Errorf("XmlReader has no method %s", name)
	}
}

func (e *Evaluator) dispatchXMLStreamEvent(handler *runtime.Instance, event runtime.Value) (bool, error) {
	eventDict, ok := event.(runtime.Dict)
	if !ok {
		return false, fmt.Errorf("XML stream event must be dict")
	}
	eventType, ok := dictStringField(eventDict, "type")
	if !ok {
		return false, fmt.Errorf("XML stream event is missing type")
	}
	value, _ := dictField(eventDict, "value")
	switch eventType {
	case "startElement":
		element, ok := value.(runtime.Dict)
		if !ok {
			return false, fmt.Errorf("XML startElement value must be dict")
		}
		name, ok := dictField(element, "name")
		if !ok {
			return false, fmt.Errorf("XML startElement value is missing name")
		}
		attributes, ok := dictField(element, "attributes")
		if !ok {
			return false, fmt.Errorf("XML startElement value is missing attributes")
		}
		return false, e.callStreamHandler(handler, "onStartElement", []runtime.Value{name, attributes})
	case "endElement":
		return false, e.callStreamHandler(handler, "onEndElement", []runtime.Value{value})
	case "text":
		return false, e.callStreamHandler(handler, "onText", []runtime.Value{value})
	case "comment":
		return false, e.callStreamHandler(handler, "onComment", []runtime.Value{value})
	case "error":
		return true, e.callStreamHandler(handler, "onError", []runtime.Value{value})
	default:
		return false, fmt.Errorf("unknown XML stream event %q", eventType)
	}
}

func (e *Evaluator) lookupXMLReader(id int64) (*xmlStreamReader, error) {
	e.xmlMu.Lock()
	reader, ok := e.xmlReaders[id]
	e.xmlMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.lookupXMLReader(id)
	}
	if !ok {
		return nil, fmt.Errorf("XmlReader is closed")
	}
	return reader, nil
}

func (e *Evaluator) xmlReaderHasNext(reader *xmlStreamReader) (bool, error) {
	if reader.pending != nil {
		return true, nil
	}
	if reader.done {
		return false, nil
	}
	event := nextXMLEvent(reader)
	if event == nil {
		reader.done = true
		return false, nil
	}
	reader.pending = event
	return true, nil
}

func nextXMLEvent(reader *xmlStreamReader) runtime.Value {
	for {
		tokenValue, err := reader.decoder.Token()
		if err == io.EOF {
			if reader.roots == 1 && reader.depth == 0 {
				return nil
			}
			reader.done = true
			parseErr := native.NewParseError("XML document must contain exactly one root element", reader.source, reader.decoder.InputOffset())
			return xmlEvent("error", native.ParseErrorValue(parseErr))
		}
		if err != nil {
			reader.done = true
			parseErr := native.NewParseError(err.Error(), reader.source, reader.decoder.InputOffset())
			if syntaxErr, ok := err.(*xml.SyntaxError); ok && syntaxErr.Line > 0 {
				parseErr.Line = int64(syntaxErr.Line)
			}
			return xmlEvent("error", native.ParseErrorValue(parseErr))
		}
		switch tok := tokenValue.(type) {
		case xml.StartElement:
			if reader.depth == 0 {
				reader.roots++
				if reader.roots > 1 {
					reader.done = true
					parseErr := native.NewParseError("XML document must contain exactly one root element", reader.source, reader.decoder.InputOffset())
					return xmlEvent("error", native.ParseErrorValue(parseErr))
				}
			}
			reader.depth++
			return xmlEvent("startElement", xmlStartElementValue(tok))
		case xml.EndElement:
			reader.depth--
			if reader.depth < 0 {
				reader.done = true
				parseErr := native.NewParseError(fmt.Sprintf("unexpected XML end element %s", tok.Name.Local), reader.source, reader.decoder.InputOffset())
				return xmlEvent("error", native.ParseErrorValue(parseErr))
			}
			return xmlEvent("endElement", runtime.String{Value: tok.Name.Local})
		case xml.CharData:
			text := string(tok)
			if reader.depth == 0 {
				if strings.TrimSpace(text) == "" {
					continue
				}
				reader.done = true
				parseErr := native.NewParseError("XML document contains non-whitespace text outside the root element", reader.source, reader.decoder.InputOffset())
				return xmlEvent("error", native.ParseErrorValue(parseErr))
			}
			if text == "" {
				continue
			}
			return xmlEvent("text", runtime.String{Value: text})
		case xml.Comment:
			return xmlEvent("comment", runtime.String{Value: string(tok)})
		}
	}
}

func xmlStartElementValue(element xml.StartElement) runtime.Value {
	attrs := map[string]runtime.DictEntry{}
	for _, attr := range element.Attr {
		key := runtime.String{Value: attr.Name.Local}
		attrs[dictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: attr.Value}}
	}
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.String{Value: "name"}):       {Key: runtime.String{Value: "name"}, Value: runtime.String{Value: element.Name.Local}},
		dictKey(runtime.String{Value: "attributes"}): {Key: runtime.String{Value: "attributes"}, Value: runtime.DictFromEntries(attrs, nil)},
	}, nil)
}

func xmlEvent(kind string, value runtime.Value) runtime.Value {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.String{Value: "type"}):  {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: kind}},
		dictKey(runtime.String{Value: "value"}): {Key: runtime.String{Value: "value"}, Value: value},
	}, nil)
}

func (e *Evaluator) jsonReader(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	source, err := e.streamSourceReader(call, args[0])
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(source.Reader)
	decoder.UseNumber()
	e.jsonMu.Lock()
	defer e.jsonMu.Unlock()
	e.nextJSONID++
	id := e.nextJSONID
	ensureMap(&e.jsonReaders)[id] = &jsonStreamReader{decoder: decoder}
	return runtime.NativeObject{Kind: "JsonReader", ID: id}, nil
}

func (e *Evaluator) closeJSONReader(id int64) {
	e.jsonMu.Lock()
	delete(e.jsonReaders, id)
	e.jsonMu.Unlock()
}

func (e *Evaluator) jsonReaderMethod(reader runtime.NativeObject, name string, args []runtime.Value) (runtime.Value, error) {
	if reader.Kind != "JsonReader" {
		return nil, fmt.Errorf("%s has no method %s", reader.TypeName(), name)
	}
	if name == "close" {
		if len(args) != 0 {
			return nil, fmt.Errorf("JsonReader.close expects no arguments")
		}
		e.closeJSONReader(reader.ID)
		return runtime.Null{}, nil
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("JsonReader.%s expects no arguments", name)
	}
	stream, err := e.lookupJSONReader(reader.ID)
	if err != nil {
		return nil, err
	}
	switch name {
	case "hasNext":
		ok, err := e.jsonReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: ok}, nil
	case "next":
		ok, err := e.jsonReaderHasNext(stream)
		if err != nil {
			return nil, err
		}
		if !ok {
			return runtime.Null{}, nil
		}
		event := stream.pending
		stream.pending = nil
		return event, nil
	default:
		return nil, fmt.Errorf("JsonReader has no method %s", name)
	}
}

func (e *Evaluator) dispatchJSONStreamEvent(handler *runtime.Instance, event runtime.Value) (bool, error) {
	eventDict, ok := event.(runtime.Dict)
	if !ok {
		return false, fmt.Errorf("JSON stream event must be dict")
	}
	eventType, ok := dictStringField(eventDict, "type")
	if !ok {
		return false, fmt.Errorf("JSON stream event is missing type")
	}
	value, _ := dictField(eventDict, "value")
	switch eventType {
	case "startObject":
		return false, e.callStreamHandler(handler, "onStartObject", nil)
	case "endObject":
		return false, e.callStreamHandler(handler, "onEndObject", nil)
	case "startArray":
		return false, e.callStreamHandler(handler, "onStartArray", nil)
	case "endArray":
		return false, e.callStreamHandler(handler, "onEndArray", nil)
	case "key":
		return false, e.callStreamHandler(handler, "onKey", []runtime.Value{value})
	case "value":
		return false, e.callStreamHandler(handler, "onValue", []runtime.Value{value})
	case "error":
		return true, e.callStreamHandler(handler, "onError", []runtime.Value{value})
	default:
		return false, fmt.Errorf("unknown JSON stream event %q", eventType)
	}
}

func (e *Evaluator) callStreamHandler(handler *runtime.Instance, name string, args []runtime.Value) error {
	methods := lookupMethodOverloads(handler.Class, name)
	if len(methods) == 0 {
		return fmt.Errorf("%s does not implement %s", handler.Class.Name, name)
	}
	method, err := selectOverload(handler.Class.Name+"."+name, methods, args)
	if err != nil {
		return err
	}
	_, err = e.applyFunctionWithThis(method, args, handler)
	return err
}

func (e *Evaluator) lookupJSONReader(id int64) (*jsonStreamReader, error) {
	e.jsonMu.Lock()
	reader, ok := e.jsonReaders[id]
	e.jsonMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.lookupJSONReader(id)
	}
	if !ok {
		return nil, fmt.Errorf("JsonReader is closed")
	}
	return reader, nil
}

func (e *Evaluator) jsonReaderHasNext(reader *jsonStreamReader) (bool, error) {
	if reader.pending != nil {
		return true, nil
	}
	if reader.done {
		return false, nil
	}
	event, err := nextJSONEvent(reader)
	if err != nil {
		return false, err
	}
	if event == nil {
		reader.done = true
		return false, nil
	}
	reader.pending = event
	return true, nil
}

func nextJSONEvent(reader *jsonStreamReader) (runtime.Value, error) {
	tokenValue, err := reader.decoder.Token()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		reader.done = true
		parseErr := native.JSONParseError(err, "")
		return jsonEvent("error", native.ParseErrorValue(parseErr)), nil
	}
	switch tok := tokenValue.(type) {
	case json.Delim:
		switch tok {
		case '{':
			reader.stack = append(reader.stack, jsonContext{kind: 'o', expectKey: true})
			return jsonEvent("startObject", runtime.Null{}), nil
		case '}':
			if len(reader.stack) > 0 {
				reader.stack = reader.stack[:len(reader.stack)-1]
			}
			markJSONValueComplete(reader)
			return jsonEvent("endObject", runtime.Null{}), nil
		case '[':
			reader.stack = append(reader.stack, jsonContext{kind: 'a'})
			return jsonEvent("startArray", runtime.Null{}), nil
		case ']':
			if len(reader.stack) > 0 {
				reader.stack = reader.stack[:len(reader.stack)-1]
			}
			markJSONValueComplete(reader)
			return jsonEvent("endArray", runtime.Null{}), nil
		}
	case string:
		if len(reader.stack) > 0 {
			top := &reader.stack[len(reader.stack)-1]
			if top.kind == 'o' && top.expectKey {
				top.expectKey = false
				return jsonEvent("key", runtime.String{Value: tok}), nil
			}
		}
		markJSONValueComplete(reader)
		return jsonEvent("value", runtime.String{Value: tok}), nil
	default:
		value, err := jsonToValue(tok)
		if err != nil {
			reader.done = true
			parseErr := native.NewParseError(err.Error(), "", -1)
			return jsonEvent("error", native.ParseErrorValue(parseErr)), nil
		}
		markJSONValueComplete(reader)
		return jsonEvent("value", value), nil
	}
	return nil, nil
}

func markJSONValueComplete(reader *jsonStreamReader) {
	if len(reader.stack) == 0 {
		return
	}
	top := &reader.stack[len(reader.stack)-1]
	if top.kind == 'o' && !top.expectKey {
		top.expectKey = true
	}
}

func jsonEvent(kind string, value runtime.Value) runtime.Value {
	return runtime.DictFromEntries(map[string]runtime.DictEntry{
		dictKey(runtime.String{Value: "type"}):  {Key: runtime.String{Value: "type"}, Value: runtime.String{Value: kind}},
		dictKey(runtime.String{Value: "value"}): {Key: runtime.String{Value: "value"}, Value: value},
	}, nil)
}
