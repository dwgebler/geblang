package native

import (
	"encoding/csv"
	"fmt"
	"strings"

	"geblang/internal/runtime"
)

func registerCSV(r *Registry) {
	r.Register("csv", "parse", func(args []runtime.Value) (runtime.Value, error) {
		text, opts, err := csvParseArgs(args, "csv.parse")
		if err != nil {
			return nil, err
		}
		rows, err := readCSVAll(text, opts)
		if err != nil {
			return nil, err
		}
		elements := make([]runtime.Value, len(rows))
		for i, row := range rows {
			cells := make([]runtime.Value, len(row))
			for j, cell := range row {
				cells[j] = runtime.String{Value: cell}
			}
			elements[i] = &runtime.List{Elements: cells}
		}
		return &runtime.List{Elements: elements}, nil
	})
	r.Register("csv", "parseDict", func(args []runtime.Value) (runtime.Value, error) {
		text, opts, err := csvParseArgs(args, "csv.parseDict")
		if err != nil {
			return nil, err
		}
		rows, err := readCSVAll(text, opts)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return &runtime.List{Elements: nil}, nil
		}
		headers := rows[0]
		elements := make([]runtime.Value, 0, len(rows)-1)
		for _, row := range rows[1:] {
			entries := make(map[string]runtime.DictEntry, len(headers))
			for j, h := range headers {
				key := runtime.String{Value: h}
				var val string
				if j < len(row) {
					val = row[j]
				}
				entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: val}}
			}
			elements = append(elements, runtime.DictFromEntries(entries, nil))
		}
		return &runtime.List{Elements: elements}, nil
	})
	r.Register("csv", "stringify", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("csv.stringify expects (rows) or (rows, options)")
		}
		opts, err := csvOptionsFrom(args, 1)
		if err != nil {
			return nil, err
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("csv.stringify expects a list of rows")
		}
		if len(list.Elements) == 0 {
			return runtime.String{Value: ""}, nil
		}
		var buf strings.Builder
		w := csv.NewWriter(&buf)
		if opts.delimiter != 0 {
			w.Comma = opts.delimiter
		}
		switch list.Elements[0].(type) {
		case runtime.Dict:
			if err := csvStringifyDicts(w, list.Elements); err != nil {
				return nil, err
			}
		case *runtime.List:
			if err := csvStringifyRows(w, list.Elements); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("csv.stringify rows must be lists or dicts")
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return nil, fmt.Errorf("csv.stringify: %v", err)
		}
		return runtime.String{Value: buf.String()}, nil
	})
}

type csvOptions struct {
	delimiter rune
	trimSpace bool
}

func csvOptionsFrom(args []runtime.Value, index int) (csvOptions, error) {
	opts := csvOptions{}
	if index >= len(args) {
		return opts, nil
	}
	dict, ok := args[index].(runtime.Dict)
	if !ok {
		return opts, fmt.Errorf("options must be a dict")
	}
	for _, dk := range dict.EntryKeys() {
		entry, _ := dict.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok {
			return opts, fmt.Errorf("option keys must be strings")
		}
		switch key.Value {
		case "delimiter":
			s, ok := entry.Value.(runtime.String)
			if !ok || len(s.Value) == 0 {
				return opts, fmt.Errorf("delimiter must be a non-empty string")
			}
			opts.delimiter = []rune(s.Value)[0]
		case "trimSpace":
			b, ok := entry.Value.(runtime.Bool)
			if !ok {
				return opts, fmt.Errorf("trimSpace must be bool")
			}
			opts.trimSpace = b.Value
		}
	}
	return opts, nil
}

func csvParseArgs(args []runtime.Value, label string) (string, csvOptions, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", csvOptions{}, fmt.Errorf("%s expects (text) or (text, options)", label)
	}
	text, ok := args[0].(runtime.String)
	if !ok {
		return "", csvOptions{}, fmt.Errorf("%s expects text as first argument", label)
	}
	opts, err := csvOptionsFrom(args, 1)
	if err != nil {
		return "", csvOptions{}, fmt.Errorf("%s: %v", label, err)
	}
	return text.Value, opts, nil
}

func csvCellString(v runtime.Value) string {
	if s, ok := v.(runtime.String); ok {
		return s.Value
	}
	return v.Inspect()
}

func csvStringifyRows(w *csv.Writer, elements []runtime.Value) error {
	for _, row := range elements {
		rowList, ok := row.(*runtime.List)
		if !ok {
			return fmt.Errorf("csv.stringify rows must be lists")
		}
		cells := make([]string, len(rowList.Elements))
		for j, cell := range rowList.Elements {
			cells[j] = csvCellString(cell)
		}
		if err := w.Write(cells); err != nil {
			return fmt.Errorf("csv.stringify: %v", err)
		}
	}
	return nil
}

func csvStringifyDicts(w *csv.Writer, elements []runtime.Value) error {
	firstDict := elements[0].(runtime.Dict)
	dks := firstDict.EntryKeys()
	headers := make([]string, len(dks))
	for i, dk := range dks {
		entry, _ := firstDict.GetEntry(dk)
		ks, ok := entry.Key.(runtime.String)
		if !ok {
			return fmt.Errorf("csv.stringify: dict keys must be strings")
		}
		headers[i] = ks.Value
	}
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("csv.stringify: %v", err)
	}
	for _, elem := range elements {
		d, ok := elem.(runtime.Dict)
		if !ok {
			return fmt.Errorf("csv.stringify: mixed row types not supported")
		}
		cells := make([]string, len(dks))
		for i, dk := range dks {
			entry, present := d.GetEntry(dk)
			if present {
				cells[i] = csvCellString(entry.Value)
			}
		}
		if err := w.Write(cells); err != nil {
			return fmt.Errorf("csv.stringify: %v", err)
		}
	}
	return nil
}

func readCSVAll(text string, opts csvOptions) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	if opts.delimiter != 0 {
		reader.Comma = opts.delimiter
	}
	reader.TrimLeadingSpace = opts.trimSpace
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv parse: %v", err)
	}
	return rows, nil
}
