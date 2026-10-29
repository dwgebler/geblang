package evaluator

import (
	"fmt"
	"strconv"

	"geblang/internal/ast"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

type cliColumn struct {
	key, header, align, format string
	value                      runtime.Value
}

func (e *Evaluator) cliTable(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return cliTable(call, args)
	}
	opts, ok := args[1].(runtime.Dict)
	if !ok {
		return cliTable(call, args)
	}
	columnValue, hasColumns := dictField(opts, "columns")
	strict := false
	if value, present := dictField(opts, "strict"); present {
		flag, ok := value.(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("cli.table strict must be bool")
		}
		strict = flag.Value
	}
	rows, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("cli.table rows must be a list")
	}
	if items, ok := columnValue.(*runtime.List); ok && len(items.Elements) == 0 {
		hasColumns = false
	}
	positional := false
	if !hasColumns {
		if !strict {
			return cliTable(call, args)
		}
		tableRows, inferred, err := cliTableRows(rows, nil)
		if err != nil {
			return nil, err
		}
		if len(inferred) == 0 {
			positional = true
			width := 0
			for _, row := range tableRows {
				if len(row) > width {
					width = len(row)
				}
			}
			for i := 0; i < width; i++ {
				inferred = append(inferred, strconv.Itoa(i))
			}
		}
		items := []runtime.Value{}
		for _, key := range inferred {
			items = append(items, runtime.String{Value: key})
		}
		columnValue = &runtime.List{Elements: items}
	}
	items, ok := columnValue.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("cli.table columns must be a list")
	}
	if !strict {
		legacy := true
		for _, item := range items.Elements {
			if _, ok := item.(runtime.String); !ok {
				legacy = false
				break
			}
		}
		if legacy {
			return cliTable(call, args)
		}
	}
	columns := make([]cliColumn, len(items.Elements))
	for i, item := range items.Elements {
		column := cliColumn{align: "left"}
		switch item := item.(type) {
		case runtime.String:
			column.key, column.header = item.Value, item.Value
		case runtime.Dict:
			for _, field := range []struct {
				name   string
				target *string
			}{{"key", &column.key}, {"header", &column.header}, {"align", &column.align}, {"format", &column.format}} {
				if value, present := dictField(item, field.name); present {
					text, ok := value.(runtime.String)
					if !ok {
						return nil, fmt.Errorf("cli.table column %s must be string", field.name)
					}
					*field.target = text.Value
				}
			}
			if value, present := dictField(item, "value"); present {
				if !runtime.IsCallableValue(value) {
					return nil, fmt.Errorf("cli.table column value must be callable")
				}
				if _, hasKey := dictField(item, "key"); hasKey {
					return nil, fmt.Errorf("cli.table column must use key or value, not both")
				}
				column.value = value
			} else if _, hasKey := dictField(item, "key"); !hasKey {
				return nil, fmt.Errorf("cli.table column requires key or value")
			}
			if _, present := dictField(item, "header"); !present {
				column.header = column.key
			}
		default:
			return nil, fmt.Errorf("cli.table columns must contain strings or descriptors")
		}
		if column.align != "left" && column.align != "right" && column.align != "center" {
			return nil, fmt.Errorf("cli.table alignment must be left, right or center")
		}
		columns[i] = column
	}
	headers, aligns := make([]string, len(columns)), make([]string, len(columns))
	for i, column := range columns {
		headers[i], aligns[i] = column.header, column.align
	}
	if positional {
		headers = nil
	}
	if value, present := dictField(opts, "headers"); present {
		list, ok := value.(*runtime.List)
		if !ok || len(list.Elements) != len(columns) {
			return nil, fmt.Errorf("cli.table headers must match columns")
		}
		headers = make([]string, len(columns))
		for i, value := range list.Elements {
			text, ok := value.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("cli.table headers must be strings")
			}
			headers[i] = text.Value
		}
	}
	separator := "  "
	if value, present := dictField(opts, "separator"); present {
		text, ok := value.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("cli.table separator must be string")
		}
		separator = text.Value
	}
	output := make([][]string, 0, len(rows.Elements))
	for index, row := range rows.Elements {
		cells := make([]string, len(columns))
		for i, column := range columns {
			var value runtime.Value
			found := true
			if column.value != nil {
				var err error
				value, err = e.callValue(column.value, []runtime.Value{row})
				if err != nil {
					return nil, err
				}
			} else {
				switch row := row.(type) {
				case runtime.Dict:
					value, found = dictField(row, column.key)
				case *runtime.List:
					found = i < len(row.Elements)
					if found {
						value = row.Elements[i]
					}
				default:
					return nil, fmt.Errorf("cli.table row requires a value accessor for %s", row.TypeName())
				}
			}
			if !found {
				if strict {
					return nil, runtime.ClassifiedError{Class: "ValueError", Message: fmt.Sprintf("cli.table row %d missing column %q", index, column.key)}
				}
				continue
			}
			if column.format != "" {
				text, err := native.FormatValueWithSpec(value, column.format)
				if err != nil {
					return nil, err
				}
				cells[i] = text
			} else {
				cells[i] = value.Inspect()
			}
		}
		output = append(output, cells)
	}
	return runtime.String{Value: renderTableAligned(headers, output, separator, aligns)}, nil
}
