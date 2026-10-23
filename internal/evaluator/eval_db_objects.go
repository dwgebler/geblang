package evaluator

import (
	"context"
	"database/sql"
	"fmt"
	"geblang/internal/runtime"
)

func (e *Evaluator) dbObjectClasses(env *runtime.Environment) []*runtime.Class {
	rowsClass := &runtime.Class{
		Name:    "Rows",
		Module:  "db",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
		Env:     env,
	}
	rowsClass.Methods["next"] = []runtime.Function{{Name: "next", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.next expects no arguments")
		}
		return e.dbRowsNext(this)
	}}}
	rowsClass.Methods["row"] = []runtime.Function{{Name: "row", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.row expects no arguments")
		}
		h, err := e.dbRowsHandle(this)
		if err != nil {
			return nil, err
		}
		if h.current == nil {
			return runtime.Null{}, nil
		}
		return h.current, nil
	}}}
	rowsClass.Methods["columns"] = []runtime.Function{{Name: "columns", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.columns expects no arguments")
		}
		h, err := e.dbRowsHandle(this)
		if err != nil {
			return nil, err
		}
		columns := make([]runtime.Value, 0, len(h.columns))
		for _, column := range h.columns {
			columns = append(columns, runtime.String{Value: column})
		}
		return &runtime.List{Elements: columns}, nil
	}}}
	rowsClass.Methods["close"] = []runtime.Function{{Name: "close", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.close expects no arguments")
		}
		return e.dbRowsClose(this)
	}}}
	rowsClass.Methods["all"] = []runtime.Function{{Name: "all", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.all expects no arguments")
		}
		return e.dbRowsAll(this)
	}}}
	rowsClass.Methods["length"] = []runtime.Function{{Name: "length", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.length expects no arguments")
		}
		rows, err := e.dbRowsAll(this)
		if err != nil {
			return nil, err
		}
		list := rows.(*runtime.List)
		return runtime.SmallInt{Value: int64(len(list.Elements))}, nil
	}}}
	rowsClass.Methods["isempty"] = []runtime.Function{{Name: "isEmpty", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.isEmpty expects no arguments")
		}
		first, err := e.dbRowsFirst(this)
		if err != nil {
			return nil, err
		}
		_, empty := first.(runtime.Null)
		return runtime.Bool{Value: empty}, nil
	}}}
	rowsClass.Methods["get"] = []runtime.Function{{Name: "get", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Rows.get expects index")
		}
		index, err := rawInt64(args[0], "index")
		if err != nil {
			return nil, err
		}
		return e.dbRowsGet(this, index)
	}}}
	rowsClass.Methods["first"] = []runtime.Function{{Name: "first", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.first expects no arguments")
		}
		return e.dbRowsFirst(this)
	}}}
	rowsClass.Methods["tolist"] = []runtime.Function{{Name: "toList", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Rows.toList expects no arguments")
		}
		return e.dbRowsAll(this)
	}}}
	rowsClass.Methods["__iter"] = []runtime.Function{{Name: "__iter", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return this, nil
	}}}
	rowsClass.Methods["__done"] = []runtime.Function{{Name: "__done", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		h, err := e.dbRowsHandle(this)
		if err != nil {
			return nil, err
		}
		ok, err := dbRowsPrefetch(h)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: !ok}, nil
	}}}
	rowsClass.Methods["__next"] = []runtime.Function{{Name: "__next", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		h, err := e.dbRowsHandle(this)
		if err != nil {
			return nil, err
		}
		ok, err := dbRowsAdvance(h)
		if err != nil {
			return nil, err
		}
		if !ok {
			return runtime.Null{}, nil
		}
		return h.current, nil
	}}}

	transactionClass := &runtime.Class{
		Name:    "Transaction",
		Module:  "db",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
		Env:     env,
	}
	transactionClass.Methods["exec"] = []runtime.Function{{Name: "exec", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbTxExecStandard(syntheticCall("db.Transaction.exec"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	transactionClass.Methods["query"] = []runtime.Function{{Name: "query", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbTxQueryRows(syntheticCall("db.Transaction.query"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	transactionClass.Methods["commit"] = []runtime.Function{{Name: "commit", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Transaction.commit expects no arguments")
		}
		return e.dbCommit(syntheticCall("db.Transaction.commit"), []runtime.Value{this.Fields["handle"]})
	}}}
	transactionClass.Methods["rollback"] = []runtime.Function{{Name: "rollback", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Transaction.rollback expects no arguments")
		}
		return e.dbRollback(syntheticCall("db.Transaction.rollback"), []runtime.Value{this.Fields["handle"]})
	}}}

	statementClass := &runtime.Class{
		Name:    "Statement",
		Module:  "db",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
		Env:     env,
	}
	statementClass.Methods["exec"] = []runtime.Function{{Name: "exec", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbStmtExecStandard(syntheticCall("db.Statement.exec"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	statementClass.Methods["query"] = []runtime.Function{{Name: "query", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbStmtQueryRows(syntheticCall("db.Statement.query"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	statementClass.Methods["close"] = []runtime.Function{{Name: "close", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Statement.close expects no arguments")
		}
		return e.dbStmtClose(syntheticCall("db.Statement.close"), []runtime.Value{this.Fields["handle"]})
	}}}

	connectionClass := &runtime.Class{
		Name:    "Connection",
		Module:  "db",
		Fields:  []runtime.Field{{Name: "handle"}},
		Methods: map[string][]runtime.Function{},
		Constructors: []runtime.Function{{Name: "Connection", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
			handle, err := e.dbOpen(syntheticCall("db.Connection"), args)
			if err != nil {
				return nil, err
			}
			this.Fields["handle"] = handle
			return runtime.Null{}, nil
		}}},
		Env: env,
	}
	connectionClass.Methods["exec"] = []runtime.Function{{Name: "exec", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbExecStandard(syntheticCall("db.Connection.exec"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	connectionClass.Methods["query"] = []runtime.Function{{Name: "query", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbQueryRows(syntheticCall("db.Connection.query"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	connectionClass.Methods["execwithtimeout"] = []runtime.Function{{Name: "execWithTimeout", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbTimedOperation(this.Fields["handle"], args, false)
	}}}
	connectionClass.Methods["querywithtimeout"] = []runtime.Function{{Name: "queryWithTimeout", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbTimedOperation(this.Fields["handle"], args, true)
	}}}

	connectionClass.Methods["begin"] = []runtime.Function{{Name: "begin", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) > 1 {
			return nil, fmt.Errorf("Connection.begin expects optional timeoutMs")
		}
		handle, err := e.dbBegin(syntheticCall("db.Connection.begin"), append([]runtime.Value{this.Fields["handle"]}, args...))
		if err != nil {
			return nil, err
		}
		return &runtime.Instance{Class: transactionClass, Fields: map[string]runtime.Value{"handle": handle}}, nil
	}}}
	connectionClass.Methods["prepare"] = []runtime.Function{{Name: "prepare", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		handle, err := e.dbPrepare(syntheticCall("db.Connection.prepare"), append([]runtime.Value{this.Fields["handle"]}, args...))
		if err != nil {
			return nil, err
		}
		return &runtime.Instance{Class: statementClass, Fields: map[string]runtime.Value{"handle": handle}}, nil
	}}}
	connectionClass.Methods["configure"] = []runtime.Function{{Name: "configure", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbConfigure(syntheticCall("db.Connection.configure"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	connectionClass.Methods["stats"] = []runtime.Function{{Name: "stats", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Connection.stats expects no arguments")
		}
		return e.dbStats(syntheticCall("db.Connection.stats"), []runtime.Value{this.Fields["handle"]})
	}}}
	connectionClass.Methods["optimize"] = []runtime.Function{{Name: "optimize", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Connection.optimize expects no arguments")
		}
		return e.dbOptimize(syntheticCall("db.Connection.optimize"), []runtime.Value{this.Fields["handle"]})
	}}}
	connectionClass.Methods["migrate"] = []runtime.Function{{Name: "migrate", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		return e.dbMigrate(syntheticCall("db.Connection.migrate"), append([]runtime.Value{this.Fields["handle"]}, args...))
	}}}
	connectionClass.Methods["close"] = []runtime.Function{{Name: "close", Native: func(this *runtime.Instance, args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("Connection.close expects no arguments")
		}
		return e.dbClose(syntheticCall("db.Connection.close"), []runtime.Value{this.Fields["handle"]})
	}}}

	return []*runtime.Class{connectionClass, transactionClass, statementClass, rowsClass}
}

func (e *Evaluator) registerDBRows(rows *sql.Rows) (runtime.Value, error) {
	return e.registerDBRowsContext(rows, nil)
}

func (e *Evaluator) registerDBRowsContext(rows *sql.Rows, cancel context.CancelFunc) (runtime.Value, error) {
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	class := e.dbRowsClass
	if class == nil && e.parent != nil {
		class = e.parent.dbRowsClass
	}
	if class == nil {
		_ = rows.Close()
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("Rows class is not initialized")
	}
	e.dbMu.Lock()
	e.nextDBRowsID++
	id := e.nextDBRowsID
	e.dbRows[id] = &dbRowsHandle{rows: rows, cancel: cancel, columns: columns, textCols: sqlTextColumns(rows)}
	e.dbMu.Unlock()
	return &runtime.Instance{Class: class, Fields: map[string]runtime.Value{"handle": runtime.NewInt64(id)}}, nil
}

func (e *Evaluator) dbRowsHandle(instance *runtime.Instance) (*dbRowsHandle, error) {
	id, ok := instance.Fields["handle"].(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return nil, fmt.Errorf("Rows has invalid backing handle")
	}
	handle := id.Value.Int64()
	e.dbMu.Lock()
	rows, ok := e.dbRows[handle]
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.dbRowsHandle(instance)
	}
	if !ok {
		return nil, fmt.Errorf("unknown Rows handle %d", handle)
	}
	return rows, nil
}

func (e *Evaluator) dbRowsClose(instance *runtime.Instance) (runtime.Value, error) {
	rows, err := e.dbRowsHandle(instance)
	if err != nil {
		return nil, err
	}
	if rows.closed {
		return runtime.Null{}, nil
	}
	rows.closed = true
	if rows.cancel != nil {
		defer rows.cancel()
	}
	rows.exhausted = true
	return runtime.Null{}, rows.rows.Close()
}

func (e *Evaluator) dbRowsNext(instance *runtime.Instance) (runtime.Value, error) {
	rows, err := e.dbRowsHandle(instance)
	if err != nil {
		return nil, err
	}
	ok, err := dbRowsAdvance(rows)
	if err != nil {
		return nil, err
	}
	return runtime.Bool{Value: ok}, nil
}

// dbRowsAdvance moves the cursor one row (honouring a prefetched row from
// the iterator protocol) and stores it as current.
func dbRowsAdvance(rows *dbRowsHandle) (bool, error) {
	if rows.prefetched != nil {
		rows.current = rows.prefetched
		rows.prefetched = nil
		if rows.caching {
			rows.cache = append(rows.cache, rows.current)
		}
		return true, nil
	}
	if rows.closed || rows.exhausted {
		rows.current = nil
		return false, nil
	}
	if !rows.rows.Next() {
		if rows.cancel != nil {
			defer rows.cancel()
		}
		rows.current = nil
		rows.exhausted = true
		rows.closed = true
		if err := rows.rows.Err(); err != nil {
			_ = rows.rows.Close()
			return false, err
		}
		if err := rows.rows.Close(); err != nil {
			return false, err
		}
		return false, nil
	}
	row, err := scanSQLRow(rows.rows, rows.columns, rows.textCols)
	if err != nil {
		rows.current = nil
		rows.exhausted = true
		rows.closed = true
		_ = rows.rows.Close()
		if rows.cancel != nil {
			rows.cancel()
		}
		return false, err
	}
	rows.current = row
	if rows.caching {
		rows.cache = append(rows.cache, row)
	}
	return true, nil
}

// dbRowsPrefetch peeks one row ahead for the for-in protocol's __done.
func dbRowsPrefetch(rows *dbRowsHandle) (bool, error) {
	if rows.prefetched != nil {
		return true, nil
	}
	saved := rows.current
	ok, err := dbRowsAdvance(rows)
	if err != nil {
		return false, err
	}
	if ok {
		// Advance cached it already when caching; stash without re-caching.
		rows.prefetched = rows.current
		if rows.caching && len(rows.cache) > 0 {
			rows.cache = rows.cache[:len(rows.cache)-1]
		}
	}
	rows.current = saved
	return ok, nil
}

func (e *Evaluator) dbRowsAll(instance *runtime.Instance) (runtime.Value, error) {
	rows, err := e.dbRowsHandle(instance)
	if err != nil {
		return nil, err
	}
	rows.caching = true
	for !rows.closed && !rows.exhausted {
		next, err := e.dbRowsNext(instance)
		if err != nil {
			return nil, err
		}
		ok, _ := next.(runtime.Bool)
		if !ok.Value {
			break
		}
	}
	out := append([]runtime.Value(nil), rows.cache...)
	return &runtime.List{Elements: out}, nil
}

func (e *Evaluator) dbRowsFirst(instance *runtime.Instance) (runtime.Value, error) {
	return e.dbRowsGet(instance, 0)
}

func (e *Evaluator) dbRowsGet(instance *runtime.Instance, index int64) (runtime.Value, error) {
	if index < 0 {
		return runtime.Null{}, nil
	}
	rows, err := e.dbRowsHandle(instance)
	if err != nil {
		return nil, err
	}
	rows.caching = true
	for int64(len(rows.cache)) <= index && !rows.closed && !rows.exhausted {
		next, err := e.dbRowsNext(instance)
		if err != nil {
			return nil, err
		}
		ok, _ := next.(runtime.Bool)
		if !ok.Value {
			break
		}
	}
	if index >= int64(len(rows.cache)) {
		return runtime.Null{}, nil
	}
	return rows.cache[int(index)], nil
}
