package native

import (
	"fmt"
	"geblang/internal/runtime"
	"net/url"
	pathlib "path"
	"strings"
)

func registerURL(r *Registry) {
	r.Register("url", "URL", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("url.URL expects string or dict")
		}
		switch value := args[0].(type) {
		case runtime.String:
			parsed, err := url.Parse(value.Value)
			if err != nil {
				return nil, fmt.Errorf("url.URL: %v", err)
			}
			return runtime.URLValue{Raw: parsed.String()}, nil
		case runtime.Dict:
			text, err := buildURLString(value)
			if err != nil {
				return nil, fmt.Errorf("url.URL: %v", err)
			}
			return runtime.URLValue{Raw: text}, nil
		default:
			return nil, fmt.Errorf("url.URL expects string or dict")
		}
	})
	r.Register("url", "parse", func(args []runtime.Value) (runtime.Value, error) {
		raw, err := singleString(args, "url.parse")
		if err != nil {
			return nil, err
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("url.parse: %v", err)
		}
		return urlPartsDict(parsed), nil
	})
	r.Register("url", "stringify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("url.stringify expects exactly one argument")
		}
		parts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("url.stringify expects dict")
		}
		text, err := buildURLString(parts)
		if err != nil {
			return nil, fmt.Errorf("url.stringify: %v", err)
		}
		return runtime.String{Value: text}, nil
	})
	r.Register("url", "encode", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "url.encode")
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: url.QueryEscape(text)}, nil
	})
	r.Register("url", "decode", func(args []runtime.Value) (runtime.Value, error) {
		text, err := singleString(args, "url.decode")
		if err != nil {
			return nil, err
		}
		decoded, err := url.QueryUnescape(text)
		if err != nil {
			return nil, fmt.Errorf("url.decode: %v", err)
		}
		return runtime.String{Value: decoded}, nil
	})
	r.Register("url", "joinPath", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("url.joinPath expects base and optional path parts")
		}
		base, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("url.joinPath base must be string")
		}
		u, err := url.Parse(base.Value)
		if err != nil {
			return nil, fmt.Errorf("url.joinPath: %v", err)
		}
		if len(args) == 1 {
			return runtime.String{Value: u.String()}, nil
		}
		parts := []string{u.Path}
		for _, arg := range args[1:] {
			part, ok := arg.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("url.joinPath parts must be strings")
			}
			parts = append(parts, part.Value)
		}
		u.Path = pathlib.Join(parts...)
		if strings.HasSuffix(parts[len(parts)-1], "/") && !strings.HasSuffix(u.Path, "/") {
			u.Path += "/"
		}
		return runtime.String{Value: u.String()}, nil
	})
}

func URLMethod(receiver runtime.URLValue, name string, args []runtime.Value) (runtime.Value, error) {
	parsed, err := url.Parse(receiver.Raw)
	if err != nil {
		return nil, fmt.Errorf("url.URL.%s: %v", name, err)
	}
	switch name {
	case "toString":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.toString expects no arguments")
		}
		return runtime.String{Value: parsed.String()}, nil
	case "toDict":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.toDict expects no arguments")
		}
		return urlPartsDict(parsed), nil
	case "scheme":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.scheme expects no arguments")
		}
		return runtime.String{Value: parsed.Scheme}, nil
	case "host":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.host expects no arguments")
		}
		return runtime.String{Value: parsed.Hostname()}, nil
	case "port":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.port expects no arguments")
		}
		return runtime.String{Value: parsed.Port()}, nil
	case "path":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.path expects no arguments")
		}
		return runtime.String{Value: parsed.Path}, nil
	case "query":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.query expects no arguments")
		}
		return queryDict(parsed.Query()), nil
	case "fragment":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.fragment expects no arguments")
		}
		return runtime.String{Value: parsed.Fragment}, nil
	case "withScheme":
		text, err := singleString(args, "url.URL.withScheme")
		if err != nil {
			return nil, err
		}
		parsed.Scheme = text
		return runtime.URLValue{Raw: parsed.String()}, nil
	case "withHost":
		text, err := singleString(args, "url.URL.withHost")
		if err != nil {
			return nil, err
		}
		parsed.Host = text
		return runtime.URLValue{Raw: parsed.String()}, nil
	case "withPath":
		text, err := singleString(args, "url.URL.withPath")
		if err != nil {
			return nil, err
		}
		parsed.Path = text
		return runtime.URLValue{Raw: parsed.String()}, nil
	case "withQuery":
		if len(args) != 1 {
			return nil, fmt.Errorf("url.URL.withQuery expects dict or string")
		}
		switch value := args[0].(type) {
		case runtime.String:
			parsed.RawQuery = strings.TrimPrefix(value.Value, "?")
		case runtime.Dict:
			query, err := queryValues(value)
			if err != nil {
				return nil, fmt.Errorf("url.URL.withQuery: %v", err)
			}
			parsed.RawQuery = query.Encode()
		default:
			return nil, fmt.Errorf("url.URL.withQuery expects dict or string")
		}
		return runtime.URLValue{Raw: parsed.String()}, nil
	case "withFragment":
		text, err := singleString(args, "url.URL.withFragment")
		if err != nil {
			return nil, err
		}
		parsed.Fragment = text
		return runtime.URLValue{Raw: parsed.String()}, nil
	case "resolve":
		if len(args) != 1 {
			return nil, fmt.Errorf("url.URL.resolve expects URL or string")
		}
		ref, err := urlFromValue(args[0])
		if err != nil {
			return nil, fmt.Errorf("url.URL.resolve: %v", err)
		}
		return runtime.URLValue{Raw: parsed.ResolveReference(ref).String()}, nil
	case "normalize":
		if len(args) != 0 {
			return nil, fmt.Errorf("url.URL.normalize expects no arguments")
		}
		parsed.Path = pathlib.Clean(parsed.Path)
		if parsed.Path == "." {
			parsed.Path = ""
		}
		parsed.RawQuery = parsed.Query().Encode()
		return runtime.URLValue{Raw: parsed.String()}, nil
	default:
		return nil, fmt.Errorf("url.URL has no method %s", name)
	}
}

func buildURLString(parts runtime.Dict) (string, error) {
	u := url.URL{
		Scheme:   dictString(parts, "scheme"),
		Path:     dictString(parts, "path"),
		Fragment: dictString(parts, "fragment"),
	}
	host := dictString(parts, "host")
	port := dictString(parts, "port")
	if host != "" && port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	if queryValue, ok := dictLookup(parts, "query"); ok {
		query, err := queryValues(queryValue)
		if err != nil {
			return "", err
		}
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func urlFromValue(value runtime.Value) (*url.URL, error) {
	switch value := value.(type) {
	case runtime.URLValue:
		return url.Parse(value.Raw)
	case runtime.String:
		return url.Parse(value.Value)
	default:
		return nil, fmt.Errorf("value must be url.URL or string")
	}
}

func urlPartsDict(parsed *url.URL) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	putString := func(key string, value string) {
		keyValue := runtime.String{Value: key}
		entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: runtime.String{Value: value}}
	}
	putString("scheme", parsed.Scheme)
	putString("host", parsed.Hostname())
	putString("port", parsed.Port())
	putString("path", parsed.Path)
	keyValue := runtime.String{Value: "query"}
	entries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: queryDict(parsed.Query())}
	putString("fragment", parsed.Fragment)
	return runtime.DictFromEntries(entries, nil)
}

func queryDict(query url.Values) runtime.Dict {
	queryEntries := map[string]runtime.DictEntry{}
	for key, values := range query {
		keyValue := runtime.String{Value: key}
		var value runtime.Value
		if len(values) == 1 {
			value = runtime.String{Value: values[0]}
		} else {
			elements := make([]runtime.Value, len(values))
			for i, item := range values {
				elements[i] = runtime.String{Value: item}
			}
			value = &runtime.List{Elements: elements}
		}
		queryEntries[DictKey(keyValue)] = runtime.DictEntry{Key: keyValue, Value: value}
	}
	return runtime.DictFromEntries(queryEntries, nil)
}

func queryValues(value runtime.Value) (url.Values, error) {
	query := url.Values{}
	dict, ok := value.(runtime.Dict)
	if !ok {
		return query, fmt.Errorf("query must be dict")
	}
	for _, dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok {
			return query, fmt.Errorf("query keys must be strings")
		}
		switch value := entry.Value.(type) {
		case runtime.String:
			query.Add(key.Value, value.Value)
		case *runtime.List:
			for _, item := range value.Elements {
				text, ok := item.(runtime.String)
				if !ok {
					return query, fmt.Errorf("query list values must be strings")
				}
				query.Add(key.Value, text.Value)
			}
		default:
			return query, fmt.Errorf("query values must be strings or lists of strings")
		}
	}
	return query, nil
}
