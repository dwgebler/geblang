package native

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"geblang/internal/runtime"
)

// Canonical method lists for dir/catalog guards.
var (
	DataFrameMethods = []string{
		"shape", "columns", "dtypes", "rows", "head", "tail", "describe",
		"col", "select", "filter", "filterFn", "sort", "unique",
		"withColumn", "rename", "drop", "dropNulls", "fillNull",
		"groupBy", "join", "pivot", "toCsv", "toJson", "toDicts",
	}
	DFSeriesMethods = []string{
		"name", "dtype", "length", "values", "toList", "isNull",
		"sum", "mean", "min", "max",
	}
	DFExprMethods = []string{
		"gt", "lt", "gte", "lte", "eq", "ne", "and_", "or_", "not",
		"add", "sub", "mul", "div", "isNull",
	}
	DFGroupByMethods = []string{"agg"}
)

func registerDataFrame(r *Registry) {
	r.Register("dataframe", "fromDict", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.fromDict expects a dict of column-name to value-list")
		}
		dict, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("dataframe.fromDict expects a dict")
		}
		frame := &runtime.DataFrame{}
		var buildErr error
		rows := -1
		dict.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			name, ok := entry.Key.(runtime.String)
			if !ok {
				buildErr = fmt.Errorf("dataframe.fromDict column names must be strings")
				return false
			}
			list, ok := entry.Value.(*runtime.List)
			if !ok {
				buildErr = fmt.Errorf("dataframe.fromDict column %q must be a list", name.Value)
				return false
			}
			if rows >= 0 && len(list.Elements) != rows {
				buildErr = fmt.Errorf("dataframe.fromDict columns must be equal length")
				return false
			}
			rows = len(list.Elements)
			col, err := dfBuildColumn(name.Value, list.Elements)
			if err != nil {
				buildErr = err
				return false
			}
			frame.Cols = append(frame.Cols, col)
			return true
		})
		if buildErr != nil {
			return nil, buildErr
		}
		return frame, nil
	})
	r.Register("dataframe", "fromRecords", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.fromRecords expects a list of dicts")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("dataframe.fromRecords expects a list of dicts")
		}
		return DataFrameFromRecordsList(list)
	})
}

// DataFrameFromRecordsList builds a frame from row dicts (key union,
// nulls for absences); shared by the registry and the SQL loader.
func DataFrameFromRecordsList(list *runtime.List) (runtime.Value, error) {
	var order []string
	seen := map[string]bool{}
	dicts := make([]runtime.Dict, len(list.Elements))
	for i, el := range list.Elements {
		d, ok := el.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("dataframe.fromRecords entries must be dicts")
		}
		dicts[i] = d
		var keys []string
		d.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			if s, ok := entry.Key.(runtime.String); ok {
				keys = append(keys, s.Value)
			}
			return true
		})
		sort.Strings(keys)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
		}
	}
	frame := &runtime.DataFrame{}
	for _, name := range order {
		vals := make([]runtime.Value, len(dicts))
		for i, d := range dicts {
			entry, ok := d.GetEntry(DictKey(runtime.String{Value: name}))
			if !ok {
				vals[i] = runtime.Null{}
			} else {
				vals[i] = entry.Value
			}
		}
		col, err := dfBuildColumn(name, vals)
		if err != nil {
			return nil, err
		}
		frame.Cols = append(frame.Cols, col)
	}
	return frame, nil
}

func registerDataFrameIO(r *Registry) {
	r.Register("dataframe", "fromCsv", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("dataframe.fromCsv expects csv text and an optional options dict")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.fromCsv expects csv text")
		}
		overrides, err := dfTypeOverrides("dataframe.fromCsv", args)
		if err != nil {
			return nil, err
		}
		return dfCsvParse(text.Value, overrides)
	})
	r.Register("dataframe", "fromJson", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.fromJson expects a JSON array-of-records string")
		}
		text, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.fromJson expects a string")
		}
		return dfJsonRecords(text.Value)
	})
	r.Register("dataframe", "col", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.col expects a column name")
		}
		name, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.col expects a string")
		}
		return &runtime.DFExpr{Kind: "col", Col: name.Value}, nil
	})
	r.Register("dataframe", "lit", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.lit expects a value")
		}
		return &runtime.DFExpr{Kind: "lit", Lit: args[0]}, nil
	})
	r.Register("dataframe", "concat", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.concat expects a list of dataframes")
		}
		list, ok := args[0].(*runtime.List)
		if !ok || len(list.Elements) == 0 {
			return nil, fmt.Errorf("dataframe.concat expects a non-empty list of dataframes")
		}
		frames := make([]*runtime.DataFrame, len(list.Elements))
		for i, el := range list.Elements {
			f, ok := el.(*runtime.DataFrame)
			if !ok {
				return nil, fmt.Errorf("dataframe.concat entries must be dataframes")
			}
			frames[i] = f
		}
		return dfConcat(frames)
	})
}

func dfTypeOverrides(label string, args []runtime.Value) (map[string]string, error) {
	overrides := map[string]string{}
	if len(args) < 2 {
		return overrides, nil
	}
	opts, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s options must be a dict", label)
	}
	if v, ok := ndDictValue(opts, "types"); ok {
		types, ok := v.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s opts.types must be a dict", label)
		}
		var convErr error
		types.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
			name, nok := entry.Key.(runtime.String)
			t, tok := entry.Value.(runtime.String)
			if !nok || !tok {
				convErr = fmt.Errorf("%s opts.types entries must map column name to type name", label)
				return false
			}
			overrides[name.Value] = t.Value
			return true
		})
		if convErr != nil {
			return nil, convErr
		}
	}
	return overrides, nil
}

func dfConcat(frames []*runtime.DataFrame) (*runtime.DataFrame, error) {
	first := frames[0]
	for _, f := range frames[1:] {
		if len(f.Cols) != len(first.Cols) {
			return nil, fmt.Errorf("dataframe.concat frames must share columns")
		}
		for i, c := range f.Cols {
			if c.Name != first.Cols[i].Name {
				return nil, fmt.Errorf("dataframe.concat frames must share columns (got %q vs %q)", c.Name, first.Cols[i].Name)
			}
		}
	}
	out := &runtime.DataFrame{}
	for ci, c := range first.Cols {
		var vals []runtime.Value
		for _, f := range frames {
			fc := f.Cols[ci]
			for r := 0; r < fc.Len(); r++ {
				vals = append(vals, dfCellValue(fc, r))
			}
		}
		col, err := dfBuildColumn(c.Name, vals)
		if err != nil {
			return nil, err
		}
		out.Cols = append(out.Cols, col)
	}
	return out, nil
}

// DataFrameMethod is the single DataFrame dispatcher shared by both backends.
// DataFrameMethod dispatches a dataframe method. invoke is the caller backend's callable invoker, passed explicitly (filterFn is its only user) so no process-global goroutine-keyed invoker lookup is needed.
func DataFrameMethod(frame *runtime.DataFrame, name string, args []runtime.Value, invoke CallableInvokerFunc) (runtime.Value, error) {
	switch name {
	case "shape":
		return &runtime.List{Elements: []runtime.Value{
			runtime.SmallInt{Value: int64(frame.Rows())},
			runtime.SmallInt{Value: int64(len(frame.Cols))},
		}}, nil
	case "rows":
		return runtime.SmallInt{Value: int64(frame.Rows())}, nil
	case "columns":
		elems := make([]runtime.Value, len(frame.Cols))
		for i, c := range frame.Cols {
			elems[i] = runtime.String{Value: c.Name}
		}
		return &runtime.List{Elements: elems}, nil
	case "dtypes":
		entries := map[string]runtime.DictEntry{}
		var order []string
		for _, c := range frame.Cols {
			key := runtime.String{Value: c.Name}
			entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: runtime.String{Value: c.Dtype}}
			order = append(order, DictKey(key))
		}
		return runtime.DictFromEntries(entries, &order), nil
	case "head", "tail":
		n := 5
		if len(args) == 1 {
			v, ok := AsInt64(args[0])
			if !ok || v < 0 {
				return nil, fmt.Errorf("dataframe.%s expects a non-negative count", name)
			}
			n = int(v)
		} else if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.%s expects an optional count", name)
		}
		rows := frame.Rows()
		if n > rows {
			n = rows
		}
		idx := make([]int, n)
		for i := range idx {
			if name == "head" {
				idx[i] = i
			} else {
				idx[i] = rows - n + i
			}
		}
		return dfTake(frame, idx), nil
	case "col":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.col expects a column name")
		}
		colName, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.col expects a string")
		}
		c, found := frame.Column(colName.Value)
		if !found {
			return nil, fmt.Errorf("unknown column %q", colName.Value)
		}
		return &runtime.DFSeries{Col: c}, nil
	case "select":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.select expects a list of column names")
		}
		names, err := dfNameList("dataframe.select", args[0])
		if err != nil {
			return nil, err
		}
		out := &runtime.DataFrame{}
		for _, n := range names {
			c, ok := frame.Column(n)
			if !ok {
				return nil, fmt.Errorf("unknown column %q", n)
			}
			out.Cols = append(out.Cols, c)
		}
		return out, nil
	case "filter":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.filter expects an expression")
		}
		expr, ok := args[0].(*runtime.DFExpr)
		if !ok {
			return nil, fmt.Errorf("dataframe.filter expects a dataframe.Expr (build one with dataframe.col)")
		}
		mask, err := dfEvalExpr(frame, expr)
		if err != nil {
			return nil, err
		}
		if mask.Dtype != runtime.DFBool {
			return nil, fmt.Errorf("dataframe.filter expression must produce booleans, got %s", mask.Dtype)
		}
		var idx []int
		for i := 0; i < frame.Rows(); i++ {
			if !mask.IsNull(i) && mask.Bool[i] {
				idx = append(idx, i)
			}
		}
		return dfTake(frame, idx), nil
	case "filterFn":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.filterFn expects a predicate function")
		}
		if invoke == nil {
			return nil, fmt.Errorf("dataframe.filterFn: no callable invoker available")
		}
		var idx []int
		for i := 0; i < frame.Rows(); i++ {
			res, err := invoke(args[0], []runtime.Value{dfRowDict(frame, i)})
			if err != nil {
				return nil, err
			}
			keep, ok := res.(runtime.Bool)
			if !ok {
				return nil, fmt.Errorf("dataframe.filterFn predicate must return a bool, got %s", res.TypeName())
			}
			if keep.Value {
				idx = append(idx, i)
			}
		}
		return dfTake(frame, idx), nil
	case "sort":
		if len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("dataframe.sort expects a column name and optional options")
		}
		colName, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.sort expects a column name")
		}
		c, found := frame.Column(colName.Value)
		if !found {
			return nil, fmt.Errorf("unknown column %q", colName.Value)
		}
		desc := false
		if len(args) == 2 {
			opts, ok := args[1].(runtime.Dict)
			if !ok {
				return nil, fmt.Errorf("dataframe.sort options must be a dict")
			}
			if v, ok := ndDictValue(opts, "desc"); ok {
				b, ok := v.(runtime.Bool)
				if !ok {
					return nil, fmt.Errorf("dataframe.sort opts.desc must be a bool")
				}
				desc = b.Value
			}
		}
		return dfTake(frame, dfSortIndices(c, desc)), nil
	case "unique":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.unique expects a column name")
		}
		colName, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.unique expects a column name")
		}
		c, found := frame.Column(colName.Value)
		if !found {
			return nil, fmt.Errorf("unknown column %q", colName.Value)
		}
		seen := map[string]bool{}
		var idx []int
		for i := 0; i < c.Len(); i++ {
			k := dfGroupKey([]*runtime.DFColumn{c}, i)
			if !seen[k] {
				seen[k] = true
				idx = append(idx, i)
			}
		}
		return dfTake(frame, idx), nil
	case "withColumn":
		if len(args) != 2 {
			return nil, fmt.Errorf("dataframe.withColumn expects a name and an expression")
		}
		colName, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.withColumn expects a string name")
		}
		expr, ok := args[1].(*runtime.DFExpr)
		if !ok {
			return nil, fmt.Errorf("dataframe.withColumn expects a dataframe.Expr")
		}
		col, err := dfEvalExpr(frame, expr)
		if err != nil {
			return nil, err
		}
		named := *col
		named.Name = colName.Value
		out := &runtime.DataFrame{}
		replaced := false
		for _, c := range frame.Cols {
			if c.Name == colName.Value {
				out.Cols = append(out.Cols, &named)
				replaced = true
			} else {
				out.Cols = append(out.Cols, c)
			}
		}
		if !replaced {
			out.Cols = append(out.Cols, &named)
		}
		return out, nil
	case "rename":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.rename expects a dict of old to new names")
		}
		mapping, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("dataframe.rename expects a dict")
		}
		out := &runtime.DataFrame{}
		for _, c := range frame.Cols {
			entry, ok := mapping.GetEntry(DictKey(runtime.String{Value: c.Name}))
			if !ok {
				out.Cols = append(out.Cols, c)
				continue
			}
			newName, ok := entry.Value.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("dataframe.rename values must be strings")
			}
			renamed := *c
			renamed.Name = newName.Value
			out.Cols = append(out.Cols, &renamed)
		}
		return out, nil
	case "drop":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.drop expects a list of column names")
		}
		names, err := dfNameList("dataframe.drop", args[0])
		if err != nil {
			return nil, err
		}
		dropSet := map[string]bool{}
		for _, n := range names {
			dropSet[n] = true
		}
		out := &runtime.DataFrame{}
		for _, c := range frame.Cols {
			if !dropSet[c.Name] {
				out.Cols = append(out.Cols, c)
			}
		}
		return out, nil
	case "dropNulls":
		cols := frame.Cols
		if len(args) == 1 {
			names, err := dfNameList("dataframe.dropNulls", args[0])
			if err != nil {
				return nil, err
			}
			cols = nil
			for _, n := range names {
				c, ok := frame.Column(n)
				if !ok {
					return nil, fmt.Errorf("unknown column %q", n)
				}
				cols = append(cols, c)
			}
		} else if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.dropNulls expects an optional list of column names")
		}
		var idx []int
		for i := 0; i < frame.Rows(); i++ {
			keep := true
			for _, c := range cols {
				if c.IsNull(i) {
					keep = false
					break
				}
			}
			if keep {
				idx = append(idx, i)
			}
		}
		return dfTake(frame, idx), nil
	case "fillNull":
		if len(args) != 2 {
			return nil, fmt.Errorf("dataframe.fillNull expects a column name and a fill value")
		}
		colName, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.fillNull expects a column name")
		}
		out := &runtime.DataFrame{}
		for _, c := range frame.Cols {
			if c.Name != colName.Value {
				out.Cols = append(out.Cols, c)
				continue
			}
			vals := make([]runtime.Value, c.Len())
			for i := range vals {
				if c.IsNull(i) {
					vals[i] = args[1]
				} else {
					vals[i] = dfCellValue(c, i)
				}
			}
			filled, err := dfBuildColumn(c.Name, vals)
			if err != nil {
				return nil, err
			}
			out.Cols = append(out.Cols, filled)
		}
		return out, nil
	case "groupBy":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.groupBy expects a column name or list of names")
		}
		var keys []string
		if s, ok := args[0].(runtime.String); ok {
			keys = []string{s.Value}
		} else {
			names, err := dfNameList("dataframe.groupBy", args[0])
			if err != nil {
				return nil, err
			}
			keys = names
		}
		for _, k := range keys {
			if _, ok := frame.Column(k); !ok {
				return nil, fmt.Errorf("unknown column %q", k)
			}
		}
		return &runtime.DFGroupBy{Frame: frame, Keys: keys}, nil
	case "pivot":
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.pivot expects an options dict")
		}
		opts, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("dataframe.pivot options must be a dict")
		}
		return dfPivot(frame, opts)
	case "join":
		if len(args) != 2 {
			return nil, fmt.Errorf("dataframe.join expects another frame and an options dict")
		}
		other, ok := args[0].(*runtime.DataFrame)
		if !ok {
			return nil, fmt.Errorf("dataframe.join expects a dataframe")
		}
		opts, ok := args[1].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("dataframe.join options must be a dict")
		}
		onVal, ok := ndDictValue(opts, "on")
		if !ok {
			return nil, fmt.Errorf("dataframe.join needs opts.on")
		}
		on, ok := onVal.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("dataframe.join opts.on must be a string")
		}
		how := "inner"
		if v, ok := ndDictValue(opts, "how"); ok {
			h, ok := v.(runtime.String)
			if !ok {
				return nil, fmt.Errorf("dataframe.join opts.how must be a string")
			}
			how = h.Value
		}
		switch how {
		case "inner", "left", "right", "outer":
		default:
			return nil, fmt.Errorf("dataframe.join opts.how must be inner, left, right, or outer")
		}
		return dfJoin(frame, other, on.Value, how)
	case "describe":
		return dfDescribe(frame)
	case "toCsv":
		if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.toCsv expects no arguments (use dataframe.writeCsv for files)")
		}
		text, err := dfCsvText(frame)
		if err != nil {
			return nil, err
		}
		return runtime.String{Value: text}, nil
	case "toJson":
		if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.toJson expects no arguments")
		}
		return dfToJson(frame)
	case "toDicts":
		if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.toDicts expects no arguments")
		}
		elems := make([]runtime.Value, frame.Rows())
		for i := 0; i < frame.Rows(); i++ {
			elems[i] = dfRowDict(frame, i)
		}
		return &runtime.List{Elements: elems}, nil
	default:
		return nil, fmt.Errorf("dataframe.DataFrame has no method %s", name)
	}
}

func dfNameList(label string, v runtime.Value) ([]string, error) {
	list, ok := v.(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects a list of column names", label)
	}
	names := make([]string, len(list.Elements))
	for i, el := range list.Elements {
		s, ok := el.(runtime.String)
		if !ok {
			return nil, fmt.Errorf("%s column names must be strings", label)
		}
		names[i] = s.Value
	}
	return names, nil
}

func dfRowDict(frame *runtime.DataFrame, row int) runtime.Dict {
	entries := map[string]runtime.DictEntry{}
	var order []string
	for _, c := range frame.Cols {
		key := runtime.String{Value: c.Name}
		entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: dfCellValue(c, row)}
		order = append(order, DictKey(key))
	}
	return runtime.DictFromEntries(entries, &order)
}

func dfToJson(frame *runtime.DataFrame) (runtime.Value, error) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < frame.Rows(); i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		text, err := EncodeJSONValue(dfRowDict(frame, i))
		if err != nil {
			return nil, err
		}
		sb.WriteString(text)
	}
	sb.WriteByte(']')
	return runtime.String{Value: sb.String()}, nil
}

// dfDescribe summarises numeric columns: count/mean/std/min/max.
func dfDescribe(frame *runtime.DataFrame) (runtime.Value, error) {
	stats := []string{"count", "mean", "std", "min", "max"}
	statCol := &runtime.DFColumn{Name: "stat", Dtype: runtime.DFString, Str: stats}
	out := &runtime.DataFrame{Cols: []*runtime.DFColumn{statCol}}
	for _, c := range frame.Cols {
		if !dfNumeric(c) {
			continue
		}
		rows := make([]int, c.Len())
		for i := range rows {
			rows[i] = i
		}
		vals := make([]runtime.Value, len(stats))
		for si, stat := range stats {
			v, err := dfAggregate(c, rows, stat)
			if err != nil {
				return nil, err
			}
			if f, ok := asFloat64Strict(v); ok {
				vals[si] = runtime.Float{Value: f}
			} else {
				vals[si] = runtime.Float{Value: math.NaN()}
			}
		}
		col, err := dfBuildColumn(c.Name, vals)
		if err != nil {
			return nil, err
		}
		out.Cols = append(out.Cols, col)
	}
	return out, nil
}

// DFSeriesMethod dispatches Series methods on both backends.
func DFSeriesMethod(s *runtime.DFSeries, name string, args []runtime.Value) (runtime.Value, error) {
	c := s.Col
	switch name {
	case "name":
		return runtime.String{Value: c.Name}, nil
	case "dtype":
		return runtime.String{Value: c.Dtype}, nil
	case "length":
		return runtime.SmallInt{Value: int64(c.Len())}, nil
	case "toList":
		elems := make([]runtime.Value, c.Len())
		for i := range elems {
			elems[i] = dfCellValue(c, i)
		}
		return &runtime.List{Elements: elems}, nil
	case "isNull":
		out := &runtime.DFColumn{Name: c.Name, Dtype: runtime.DFBool, Bool: make([]bool, c.Len())}
		for i := 0; i < c.Len(); i++ {
			out.Bool[i] = c.IsNull(i)
		}
		return &runtime.DFSeries{Col: out}, nil
	case "values":
		if !dfNumeric(c) {
			return nil, fmt.Errorf("dataframe.Series.values needs a numeric column, %q is %s", c.Name, c.Dtype)
		}
		out := ndAllocF64([]int{c.Len()})
		for i := 0; i < c.Len(); i++ {
			if !c.IsNull(i) {
				out.F64[i] = dfCellF64(c, i)
			}
		}
		return out, nil
	case "sum", "mean", "min", "max":
		rows := make([]int, c.Len())
		for i := range rows {
			rows[i] = i
		}
		return dfAggregate(c, rows, name)
	default:
		return nil, fmt.Errorf("dataframe.Series has no method %s", name)
	}
}

// DFExprMethod builds expression trees on both backends.
func DFExprMethod(e *runtime.DFExpr, name string, args []runtime.Value) (runtime.Value, error) {
	operand := func() (*runtime.DFExpr, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("dataframe.Expr.%s expects one operand", name)
		}
		if other, ok := args[0].(*runtime.DFExpr); ok {
			return other, nil
		}
		return &runtime.DFExpr{Kind: "lit", Lit: args[0]}, nil
	}
	switch name {
	case "gt", "lt", "gte", "lte", "eq", "ne", "add", "sub", "mul", "div":
		right, err := operand()
		if err != nil {
			return nil, err
		}
		return &runtime.DFExpr{Kind: "bin", Op: name, Left: e, Right: right}, nil
	case "and_", "or_":
		right, err := operand()
		if err != nil {
			return nil, err
		}
		op := "and"
		if name == "or_" {
			op = "or"
		}
		return &runtime.DFExpr{Kind: "bin", Op: op, Left: e, Right: right}, nil
	case "not":
		if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.Expr.not expects no arguments")
		}
		return &runtime.DFExpr{Kind: "not", Left: e}, nil
	case "isNull":
		if len(args) != 0 {
			return nil, fmt.Errorf("dataframe.Expr.isNull expects no arguments")
		}
		return &runtime.DFExpr{Kind: "isNull", Left: e}, nil
	default:
		return nil, fmt.Errorf("dataframe.Expr has no method %s", name)
	}
}

// DFGroupByMethod dispatches GroupBy.agg on both backends.
func DFGroupByMethod(g *runtime.DFGroupBy, name string, args []runtime.Value) (runtime.Value, error) {
	if name != "agg" {
		return nil, fmt.Errorf("dataframe.GroupBy has no method %s", name)
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("dataframe.GroupBy.agg expects a spec dict")
	}
	spec, ok := args[0].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("dataframe.GroupBy.agg expects a dict of column to aggregation(s)")
	}
	type aggCol struct {
		col  string
		aggs []string
	}
	var plan []aggCol
	var specErr error
	spec.ForEachEntry(func(key string, entry runtime.DictEntry) bool {
		colName, ok := entry.Key.(runtime.String)
		if !ok {
			specErr = fmt.Errorf("dataframe.GroupBy.agg spec keys must be column names")
			return false
		}
		switch v := entry.Value.(type) {
		case runtime.String:
			plan = append(plan, aggCol{col: colName.Value, aggs: []string{v.Value}})
		case *runtime.List:
			var aggs []string
			for _, el := range v.Elements {
				s, ok := el.(runtime.String)
				if !ok {
					specErr = fmt.Errorf("dataframe.GroupBy.agg aggregation names must be strings")
					return false
				}
				aggs = append(aggs, s.Value)
			}
			plan = append(plan, aggCol{col: colName.Value, aggs: aggs})
		default:
			specErr = fmt.Errorf("dataframe.GroupBy.agg spec values must be a string or list of strings")
			return false
		}
		return true
	})
	if specErr != nil {
		return nil, specErr
	}
	frame := g.Frame
	keyCols := make([]*runtime.DFColumn, len(g.Keys))
	for i, k := range g.Keys {
		keyCols[i], _ = frame.Column(k)
	}
	groupRows := map[string][]int{}
	var groupOrder []string
	for i := 0; i < frame.Rows(); i++ {
		k := dfGroupKey(keyCols, i)
		if _, ok := groupRows[k]; !ok {
			groupOrder = append(groupOrder, k)
		}
		groupRows[k] = append(groupRows[k], i)
	}
	out := &runtime.DataFrame{}
	for ki, keyName := range g.Keys {
		vals := make([]runtime.Value, len(groupOrder))
		for gi, gk := range groupOrder {
			vals[gi] = dfCellValue(keyCols[ki], groupRows[gk][0])
		}
		col, err := dfBuildColumn(keyName, vals)
		if err != nil {
			return nil, err
		}
		out.Cols = append(out.Cols, col)
	}
	for _, p := range plan {
		src, ok := frame.Column(p.col)
		if !ok {
			return nil, fmt.Errorf("unknown column %q", p.col)
		}
		for _, agg := range p.aggs {
			vals := make([]runtime.Value, len(groupOrder))
			for gi, gk := range groupOrder {
				v, err := dfAggregate(src, groupRows[gk], agg)
				if err != nil {
					return nil, err
				}
				vals[gi] = v
			}
			colName := p.col + "_" + agg
			if len(p.aggs) == 1 {
				colName = p.col + "_" + agg
			}
			col, err := dfBuildColumn(colName, vals)
			if err != nil {
				return nil, err
			}
			out.Cols = append(out.Cols, col)
		}
	}
	return out, nil
}

// DataFrameFromCsvText parses CSV text; the evaluator's file loader entry point.
func DataFrameFromCsvText(text string, typeOverrides map[string]string) (*runtime.DataFrame, error) {
	return dfCsvParse(text, typeOverrides)
}

// DataFrameCsvText renders a frame as CSV; the evaluator's file writer entry point.
func DataFrameCsvText(frame *runtime.DataFrame) (string, error) {
	return dfCsvText(frame)
}

// DataFrameTypeOverrides parses the opts.types dict for CSV loaders.
func DataFrameTypeOverrides(label string, args []runtime.Value) (map[string]string, error) {
	return dfTypeOverrides(label, args)
}
