package evaluator

import (
	"database/sql"
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"math/big"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

func (e *Evaluator) dbConnectionObject(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if err := e.ensureBuiltinTypes(); err != nil {
		return nil, err
	}
	return e.instantiateClass(e.dbConnectionClass, args)
}

func (e *Evaluator) dbOpen(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	driverName, dsn, err := dbConnectionSpec(call, args)
	if err != nil {
		return nil, err
	}
	if driverName == "sqlite" {
		dsn, err = e.sqliteDSNWithOptions(call, dsn, args)
		if err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := dbApplyPoolOptions(call, db, args); err != nil {
		_ = db.Close()
		return nil, err
	}
	// A private in-memory sqlite db is distinct per connection, so a pool of
	// more than one would see separate empty databases. Pin to a single
	// connection so concurrent access shares one db.
	if driverName == "sqlite" && sqliteIsPrivateMemory(dsn) {
		db.SetMaxOpenConns(1)
	}
	e.dbMu.Lock()
	defer e.dbMu.Unlock()
	e.nextDBID++
	e.dbs[e.nextDBID] = db
	e.dbDrivers[e.nextDBID] = driverName
	return runtime.NewInt64(e.nextDBID), nil
}

func (e *Evaluator) dbExec(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, driver, args[1:])
	if err != nil {
		return nil, err
	}
	result, err := db.Exec(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbExecStandard(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects database handle and query", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, driver, args[1:])
	if err != nil {
		return nil, err
	}
	result, err := db.Exec(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbQuery(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, driver, args[1:])
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	return sqlRowsToRuntime(rows)
}

func (e *Evaluator) dbQueryRows(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, driver, args[1:])
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	return e.registerDBRows(rows)
}

func (e *Evaluator) dbBegin(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	defer e.dbMu.Unlock()
	e.nextTxID++
	e.txs[e.nextTxID] = &dbTxHandle{tx: tx, driver: driver}
	return runtime.NewInt64(e.nextTxID), nil
}

func (e *Evaluator) dbTxExec(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	tx, err := e.txHandle(args[0])
	if err != nil {
		return nil, err
	}
	query, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s query must be string", call.Callee.String())
	}
	sqlArgs, err := dbArgs(args[2:])
	if err != nil {
		return nil, err
	}
	result, err := tx.tx.Exec(query.Value, sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbTxExecStandard(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects transaction handle and query", call.Callee.String())
	}
	tx, err := e.txHandle(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, tx.driver, args[1:])
	if err != nil {
		return nil, err
	}
	result, err := tx.tx.Exec(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbTxQuery(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	tx, err := e.txHandle(args[0])
	if err != nil {
		return nil, err
	}
	query, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s query must be string", call.Callee.String())
	}
	sqlArgs, err := dbArgs(args[2:])
	if err != nil {
		return nil, err
	}
	rows, err := tx.tx.Query(query.Value, sqlArgs...)
	if err != nil {
		return nil, err
	}
	return sqlRowsToRuntime(rows)
}

func (e *Evaluator) dbTxQueryRows(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("%s expects at least two arguments", call.Callee.String())
	}
	tx, err := e.txHandle(args[0])
	if err != nil {
		return nil, err
	}
	query, sqlArgs, err := dbStandardQueryAndArgs(call, tx.driver, args[1:])
	if err != nil {
		return nil, err
	}
	rows, err := tx.tx.Query(query, sqlArgs...)
	if err != nil {
		return nil, err
	}
	return e.registerDBRows(rows)
}

func (e *Evaluator) dbCommit(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	tx, err := e.takeTxHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, tx.tx.Commit()
}

func (e *Evaluator) dbRollback(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	tx, err := e.takeTxHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, tx.tx.Rollback()
}

func (e *Evaluator) dbPrepare(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects exactly two arguments", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(args[0])
	if err != nil {
		return nil, err
	}
	query, ok := args[1].(runtime.String)
	if !ok {
		return nil, fmt.Errorf("%s query must be string", call.Callee.String())
	}
	preparedQuery, paramNames, err := dbNormalizeQuery(query.Value, driver)
	if err != nil {
		return nil, err
	}
	stmt, err := db.Prepare(preparedQuery)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	defer e.dbMu.Unlock()
	e.nextStmtID++
	e.stmts[e.nextStmtID] = &dbStmtHandle{stmt: stmt, driver: driver, paramNames: paramNames}
	return runtime.NewInt64(e.nextStmtID), nil
}

func (e *Evaluator) dbStmtExec(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	stmt, err := e.stmtHandle(args[0])
	if err != nil {
		return nil, err
	}
	sqlArgs, err := dbArgs(args[1:])
	if err != nil {
		return nil, err
	}
	result, err := stmt.stmt.Exec(sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbStmtExecStandard(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects statement handle", call.Callee.String())
	}
	stmt, err := e.stmtHandle(args[0])
	if err != nil {
		return nil, err
	}
	sqlArgs, err := dbPreparedArgs(args[1:], stmt.paramNames)
	if err != nil {
		return nil, err
	}
	result, err := stmt.stmt.Exec(sqlArgs...)
	if err != nil {
		return nil, err
	}
	rows, _ := result.RowsAffected()
	lastID, _ := result.LastInsertId()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "rowsAffected", runtime.NewInt64(rows))
	putDict(entries, "lastInsertId", runtime.NewInt64(lastID))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbStmtQuery(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	stmt, err := e.stmtHandle(args[0])
	if err != nil {
		return nil, err
	}
	sqlArgs, err := dbArgs(args[1:])
	if err != nil {
		return nil, err
	}
	rows, err := stmt.stmt.Query(sqlArgs...)
	if err != nil {
		return nil, err
	}
	return sqlRowsToRuntime(rows)
}

func (e *Evaluator) dbStmtQueryRows(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s expects at least one argument", call.Callee.String())
	}
	stmt, err := e.stmtHandle(args[0])
	if err != nil {
		return nil, err
	}
	sqlArgs, err := dbPreparedArgs(args[1:], stmt.paramNames)
	if err != nil {
		return nil, err
	}
	rows, err := stmt.stmt.Query(sqlArgs...)
	if err != nil {
		return nil, err
	}
	return e.registerDBRows(rows)
}

func (e *Evaluator) dbStmtClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	stmt, err := e.takeStmtHandle(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, stmt.stmt.Close()
}

// dbApplyPoolOptions applies pool tuning from the connect-time options
// dict (previously only db.configure applied them, so the dict form
// silently ignored maxOpenConns and churned connections at Go's
// 2-idle default). Bare connections default to 8 idle conns; setting
// maxOpenConns without maxIdleConns aligns idle to open.
func dbApplyPoolOptions(call *ast.CallExpression, db *sql.DB, args []runtime.Value) error {
	var options runtime.Dict
	hasOptions := false
	if len(args) == 1 {
		if d, ok := args[0].(runtime.Dict); ok {
			options = d
			hasOptions = true
		}
	}
	maxOpen, hasMaxOpen := 0, false
	maxIdle, hasMaxIdle := 0, false
	if hasOptions {
		if value, ok := dictField(options, "maxOpenConns"); ok {
			n, err := intOption(call, value, "maxOpenConns")
			if err != nil {
				return err
			}
			maxOpen, hasMaxOpen = n, true
		}
		if value, ok := dictField(options, "maxIdleConns"); ok {
			n, err := intOption(call, value, "maxIdleConns")
			if err != nil {
				return err
			}
			maxIdle, hasMaxIdle = n, true
		}
		if value, ok := dictField(options, "connMaxLifetimeMs"); ok {
			n, err := intOption(call, value, "connMaxLifetimeMs")
			if err != nil {
				return err
			}
			db.SetConnMaxLifetime(time.Duration(n) * time.Millisecond)
		}
		if value, ok := dictField(options, "connMaxIdleTimeMs"); ok {
			n, err := intOption(call, value, "connMaxIdleTimeMs")
			if err != nil {
				return err
			}
			db.SetConnMaxIdleTime(time.Duration(n) * time.Millisecond)
		}
	}
	switch {
	case hasMaxOpen && hasMaxIdle:
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxIdle)
	case hasMaxOpen:
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxOpen)
	case hasMaxIdle:
		db.SetMaxIdleConns(maxIdle)
	default:
		db.SetMaxIdleConns(8)
	}
	return nil
}

func (e *Evaluator) dbConfigure(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects database handle and options", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	options, ok := args[1].(runtime.Dict)
	if !ok {
		return nil, fmt.Errorf("%s options must be dict", call.Callee.String())
	}
	if value, ok := dictField(options, "maxOpenConns"); ok {
		n, err := intOption(call, value, "maxOpenConns")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(n)
	}
	if value, ok := dictField(options, "maxIdleConns"); ok {
		n, err := intOption(call, value, "maxIdleConns")
		if err != nil {
			return nil, err
		}
		db.SetMaxIdleConns(n)
	}
	if value, ok := dictField(options, "connMaxLifetimeMs"); ok {
		n, err := intOption(call, value, "connMaxLifetimeMs")
		if err != nil {
			return nil, err
		}
		db.SetConnMaxLifetime(time.Duration(n) * time.Millisecond)
	}
	if value, ok := dictField(options, "connMaxIdleTimeMs"); ok {
		n, err := intOption(call, value, "connMaxIdleTimeMs")
		if err != nil {
			return nil, err
		}
		db.SetConnMaxIdleTime(time.Duration(n) * time.Millisecond)
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) dbOptimize(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	handle, err := dbHandleID(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriver(handle)
	if err != nil {
		return nil, err
	}
	if driver != "sqlite" {
		return nil, fmt.Errorf("optimize is only supported for sqlite connections")
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA optimize"); err != nil {
		return nil, err
	}
	return runtime.Null{}, nil
}

func (e *Evaluator) dbStats(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	stats := db.Stats()
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "maxOpenConnections", runtime.NewInt64(int64(stats.MaxOpenConnections)))
	putDict(entries, "openConnections", runtime.NewInt64(int64(stats.OpenConnections)))
	putDict(entries, "inUse", runtime.NewInt64(int64(stats.InUse)))
	putDict(entries, "idle", runtime.NewInt64(int64(stats.Idle)))
	putDict(entries, "waitCount", runtime.NewInt64(stats.WaitCount))
	putDict(entries, "waitDurationMs", runtime.NewInt64(stats.WaitDuration.Milliseconds()))
	putDict(entries, "maxIdleClosed", runtime.NewInt64(stats.MaxIdleClosed))
	putDict(entries, "maxIdleTimeClosed", runtime.NewInt64(stats.MaxIdleTimeClosed))
	putDict(entries, "maxLifetimeClosed", runtime.NewInt64(stats.MaxLifetimeClosed))
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbMigrate(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects database handle and migrations", call.Callee.String())
	}
	handle, err := dbHandleID(args[0])
	if err != nil {
		return nil, err
	}
	db, err := e.dbHandle(args[0])
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriver(handle)
	if err != nil {
		return nil, err
	}
	idPlaceholder := dbPlaceholder(driver, 1)
	appliedAtPlaceholder := dbPlaceholder(driver, 2)
	migrations, ok := args[1].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s migrations must be list<dict>", call.Callee.String())
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`create table if not exists geblang_migrations (id text primary key, applied_at text not null)`); err != nil {
		return nil, err
	}
	applied := []runtime.Value{}
	skipped := []runtime.Value{}
	for _, migration := range migrations.Elements {
		dict, ok := migration.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s each migration must be dict", call.Callee.String())
		}
		id, ok := dictStringField(dict, "id")
		if !ok || id == "" {
			return nil, fmt.Errorf("%s migration.id must be non-empty string", call.Callee.String())
		}
		sqlText, ok := dictStringField(dict, "sql")
		if !ok || strings.TrimSpace(sqlText) == "" {
			return nil, fmt.Errorf("%s migration.sql must be non-empty string", call.Callee.String())
		}
		var existing string
		err := tx.QueryRow(`select id from geblang_migrations where id = `+idPlaceholder, id).Scan(&existing)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			skipped = append(skipped, runtime.String{Value: id})
			continue
		}
		if _, err := tx.Exec(sqlText); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`insert into geblang_migrations (id, applied_at) values (`+idPlaceholder+`, `+appliedAtPlaceholder+`)`, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, err
		}
		applied = append(applied, runtime.String{Value: id})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	entries := map[string]runtime.DictEntry{}
	putDict(entries, "applied", &runtime.List{Elements: applied})
	putDict(entries, "skipped", &runtime.List{Elements: skipped})
	return runtime.Dict{Entries: entries}, nil
}

func intOption(call *ast.CallExpression, value runtime.Value, name string) (int, error) {
	n, err := int64Argument(call, value, name)
	if err != nil {
		return 0, err
	}
	if n < 0 || n > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("%s %s out of range", call.Callee.String(), name)
	}
	return int(n), nil
}

func sqlRowsToRuntime(rows *sql.Rows) (runtime.Value, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	textCols := sqlTextColumns(rows)
	out := []runtime.Value{}
	for rows.Next() {
		row, err := scanSQLRow(rows, columns, textCols)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &runtime.List{Elements: out}, nil
}

// sqlTextColumns marks columns whose []byte scan values are text, not
// binary (MySQL returns TEXT/VARCHAR/DECIMAL/DATETIME as []byte); only
// BLOB/BINARY-typed columns stay bytes.
func sqlTextColumns(rows *sql.Rows) []bool {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil
	}
	out := make([]bool, len(types))
	for i, t := range types {
		name := strings.ToUpper(t.DatabaseTypeName())
		out[i] = !strings.Contains(name, "BLOB") && !strings.Contains(name, "BINARY")
	}
	return out
}

func scanSQLRow(rows *sql.Rows, columns []string, textCols []bool) (runtime.Value, error) {
	raw := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	entries := map[string]runtime.DictEntry{}
	for i, column := range columns {
		if b, ok := raw[i].([]byte); ok && i < len(textCols) && textCols[i] {
			raw[i] = string(b)
		}
		value, err := sqlValueToRuntime(raw[i])
		if err != nil {
			return nil, err
		}
		putDict(entries, column, value)
	}
	return runtime.Dict{Entries: entries}, nil
}

func (e *Evaluator) dbClose(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", call.Callee.String())
	}
	handle, err := dbHandleID(args[0])
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	db, ok := e.dbs[handle]
	if ok {
		delete(e.dbs, handle)
		delete(e.dbDrivers, handle)
	}
	e.dbMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown database handle %d", handle)
	}
	return runtime.Null{}, db.Close()
}

func (e *Evaluator) dbHandle(value runtime.Value) (*sql.DB, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	db, ok := e.dbs[handle]
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.dbHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown database handle %d", handle)
	}
	return db, nil
}

func (e *Evaluator) dbDriver(handle int64) (string, error) {
	e.dbMu.Lock()
	driver, ok := e.dbDrivers[handle]
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.dbDriver(handle)
	}
	if !ok {
		return "", fmt.Errorf("unknown database handle %d", handle)
	}
	return driver, nil
}

func (e *Evaluator) dbDriverFromValue(value runtime.Value) (string, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return "", err
	}
	return e.dbDriver(handle)
}

func (e *Evaluator) txHandle(value runtime.Value) (*dbTxHandle, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	tx, ok := e.txs[handle]
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.txHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown transaction handle %d", handle)
	}
	return tx, nil
}

func (e *Evaluator) takeTxHandle(value runtime.Value) (*dbTxHandle, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	tx, ok := e.txs[handle]
	if ok {
		delete(e.txs, handle)
	}
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.takeTxHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown transaction handle %d", handle)
	}
	return tx, nil
}

func (e *Evaluator) stmtHandle(value runtime.Value) (*dbStmtHandle, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	stmt, ok := e.stmts[handle]
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.stmtHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown statement handle %d", handle)
	}
	return stmt, nil
}

func (e *Evaluator) takeStmtHandle(value runtime.Value) (*dbStmtHandle, error) {
	handle, err := dbHandleID(value)
	if err != nil {
		return nil, err
	}
	e.dbMu.Lock()
	stmt, ok := e.stmts[handle]
	if ok {
		delete(e.stmts, handle)
	}
	e.dbMu.Unlock()
	if !ok && e.parent != nil {
		return e.parent.takeStmtHandle(value)
	}
	if !ok {
		return nil, fmt.Errorf("unknown statement handle %d", handle)
	}
	return stmt, nil
}

// sqliteDSNWithDefaults gives a file DSN server-friendly defaults (WAL, synchronous NORMAL, busy_timeout 5s); :memory: and DSNs with explicit _pragma/busy_timeout are left as-is.
func sqliteDSNWithDefaults(dsn string) string {
	if dsn == ":memory:" || strings.Contains(dsn, "busy_timeout") || strings.Contains(dsn, "_pragma") {
		return dsn
	}
	const pragma = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	out := dsn
	if !strings.HasPrefix(out, "file:") {
		out = "file:" + out
	}
	if strings.Contains(out, "?") {
		return out + "&" + pragma
	}
	return out + "?" + pragma
}

// sqliteDSNWithOptions turns the sqlite tuning options into `_pragma=` DSN params.
func (e *Evaluator) sqliteDSNWithOptions(call *ast.CallExpression, dsn string, args []runtime.Value) (string, error) {
	var options runtime.Dict
	hasOptions := false
	if len(args) == 1 {
		if d, ok := args[0].(runtime.Dict); ok {
			options, hasOptions = d, true
		}
	}
	if !hasOptions || dsn == ":memory:" || sqliteIsPrivateMemory(dsn) {
		return sqliteDSNWithDefaults(dsn), nil
	}
	pragmas := []string{}
	if !strings.Contains(dsn, "busy_timeout") && !strings.Contains(dsn, "_pragma") {
		busyMs := int64(5000)
		if v, ok := dictField(options, "busyTimeoutMs"); ok {
			n, err := intOption(call, v, "busyTimeoutMs")
			if err != nil {
				return "", err
			}
			busyMs = int64(n)
		}
		pragmas = append(pragmas, fmt.Sprintf("busy_timeout(%d)", busyMs))
		// File databases default to WAL + synchronous(NORMAL); wal:false opts out, an explicit synchronous overrides.
		wal := true
		if b, ok := dictBoolField(options, "wal"); ok {
			wal = b
		}
		if wal {
			pragmas = append(pragmas, "journal_mode(WAL)")
		}
		sync := "NORMAL"
		if s, ok := dictStringField(options, "synchronous"); ok {
			sync = strings.ToUpper(s)
		}
		switch sync {
		case "OFF", "NORMAL", "FULL", "EXTRA":
		default:
			return "", fmt.Errorf("%s synchronous must be OFF, NORMAL, FULL, or EXTRA", call.Callee.String())
		}
		pragmas = append(pragmas, "synchronous("+sync+")")
	}
	if b, ok := dictBoolField(options, "foreignKeys"); ok && b {
		pragmas = append(pragmas, "foreign_keys(ON)")
	}
	if v, ok := dictField(options, "cacheSizeKb"); ok {
		n, err := intOption(call, v, "cacheSizeKb")
		if err != nil {
			return "", err
		}
		pragmas = append(pragmas, fmt.Sprintf("cache_size(-%d)", n))
	}
	if v, ok := dictField(options, "mmapSizeMb"); ok {
		n, err := intOption(call, v, "mmapSizeMb")
		if err != nil {
			return "", err
		}
		pragmas = append(pragmas, fmt.Sprintf("mmap_size(%d)", int64(n)*1024*1024))
	}
	if b, ok := dictBoolField(options, "tempStoreMemory"); ok && b {
		pragmas = append(pragmas, "temp_store(MEMORY)")
	}
	if len(pragmas) == 0 {
		return sqliteDSNWithDefaults(dsn), nil
	}
	out := dsn
	if !strings.HasPrefix(out, "file:") {
		out = "file:" + out
	}
	sep := "?"
	if strings.Contains(out, "?") {
		sep = "&"
	}
	for _, p := range pragmas {
		out += sep + "_pragma=" + p
		sep = "&"
	}
	return out, nil
}

// sqliteIsPrivateMemory reports whether a sqlite DSN names a private in-memory database.
func sqliteIsPrivateMemory(dsn string) bool {
	if !strings.Contains(dsn, ":memory:") && !strings.Contains(dsn, "mode=memory") {
		return false
	}
	return !strings.Contains(dsn, "cache=shared")
}

func dbHandleID(value runtime.Value) (int64, error) {
	// Class-API objects (db.Connection / Transaction / Statement / Rows)
	// carry their raw id in a handle field; unwrap so the functional API
	// composes with them.
	if inst, ok := value.(*runtime.Instance); ok {
		if h, ok := inst.GetField("handle"); ok {
			return dbHandleID(h)
		}
	}
	if small, ok := value.(runtime.SmallInt); ok {
		return small.Value, nil
	}
	id, ok := value.(runtime.Int)
	if !ok || !id.Value.IsInt64() {
		return 0, fmt.Errorf("database handle must be int")
	}
	return id.Value.Int64(), nil
}

func dbConnectionSpec(call *ast.CallExpression, args []runtime.Value) (string, string, error) {
	if len(args) == 1 {
		options, ok := args[0].(runtime.Dict)
		if !ok {
			return "", "", fmt.Errorf("%s expects options dict or driver and connection string", call.Callee.String())
		}
		driver, ok := dictStringField(options, "driver")
		if !ok || driver == "" {
			return "", "", fmt.Errorf("%s options.driver must be a non-empty string", call.Callee.String())
		}
		driverName, err := dbDriverName(driver)
		if err != nil {
			return "", "", err
		}
		if dsn, ok := firstDictStringField(options, "dsn", "connectionString", "url"); ok {
			return driverName, dsn, nil
		}
		dsn, err := dbBuildDSN(driver, options)
		if err != nil {
			return "", "", fmt.Errorf("%s %v", call.Callee.String(), err)
		}
		return driverName, dsn, nil
	}
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s expects options dict or driver and connection string", call.Callee.String())
	}
	driver, ok := args[0].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("%s driver must be string", call.Callee.String())
	}
	dsn, ok := args[1].(runtime.String)
	if !ok {
		return "", "", fmt.Errorf("%s connection string must be string", call.Callee.String())
	}
	driverName, err := dbDriverName(driver.Value)
	if err != nil {
		return "", "", err
	}
	return driverName, dsn.Value, nil
}

func dbDriverName(name string) (string, error) {
	switch name {
	case "sqlite":
		return "sqlite", nil
	case "postgres":
		return "pgx", nil
	case "mysql":
		return "mysql", nil
	default:
		return "", fmt.Errorf("unsupported database driver %q", name)
	}
}

func dbBuildDSN(driver string, options runtime.Dict) (string, error) {
	switch driver {
	case "sqlite":
		if path, ok := firstDictStringField(options, "path", "file", "database", "dbname"); ok && path != "" {
			return path, nil
		}
		if memory, ok := dictBoolField(options, "memory"); ok && memory {
			return ":memory:", nil
		}
		return "", fmt.Errorf("sqlite options require path or memory")
	case "postgres":
		host, _ := dictStringField(options, "host")
		if host == "" {
			host = "localhost"
		}
		port := int64(5432)
		if value, ok := dictField(options, "port"); ok {
			n, err := runtimeInt64(value, "port")
			if err != nil {
				return "", err
			}
			port = n
		}
		database, _ := firstDictStringField(options, "database", "dbname")
		user, _ := dictStringField(options, "user")
		password, _ := dictStringField(options, "password")
		sslmode, _ := dictStringField(options, "sslmode")
		if sslmode == "" {
			sslmode = "disable"
		}
		parts := []string{fmt.Sprintf("host=%s", host), fmt.Sprintf("port=%d", port)}
		if user != "" {
			parts = append(parts, fmt.Sprintf("user=%s", user))
		}
		if password != "" {
			parts = append(parts, fmt.Sprintf("password=%s", password))
		}
		if database != "" {
			parts = append(parts, fmt.Sprintf("dbname=%s", database))
		}
		parts = append(parts, fmt.Sprintf("sslmode=%s", sslmode))
		return strings.Join(parts, " "), nil
	case "mysql":
		user, _ := dictStringField(options, "user")
		password, _ := dictStringField(options, "password")
		database, _ := firstDictStringField(options, "database", "dbname")
		protocol := "tcp"
		host, _ := dictStringField(options, "host")
		socket, _ := dictStringField(options, "socket")
		if socket != "" {
			protocol = "unix"
			host = socket
		} else {
			if host == "" {
				host = "127.0.0.1"
			}
			port := int64(3306)
			if value, ok := dictField(options, "port"); ok {
				n, err := runtimeInt64(value, "port")
				if err != nil {
					return "", err
				}
				port = n
			}
			host = fmt.Sprintf("%s:%d", host, port)
		}
		auth := user
		if password != "" {
			auth += ":" + password
		}
		query := []string{}
		if parseTime, ok := dictBoolField(options, "parseTime"); ok {
			query = append(query, "parseTime="+strconv.FormatBool(parseTime))
		} else {
			query = append(query, "parseTime=true")
		}
		if charset, ok := dictStringField(options, "charset"); ok && charset != "" {
			query = append(query, "charset="+neturl.QueryEscape(charset))
		}
		if loc, ok := dictStringField(options, "loc"); ok && loc != "" {
			query = append(query, "loc="+neturl.QueryEscape(loc))
		}
		return fmt.Sprintf("%s@%s(%s)/%s?%s", auth, protocol, host, database, strings.Join(query, "&")), nil
	default:
		return "", fmt.Errorf("unsupported database driver %q", driver)
	}
}

func dbPlaceholder(driver string, index int) string {
	if driver == "pgx" {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func dbStandardQueryAndArgs(call *ast.CallExpression, driver string, args []runtime.Value) (string, []any, error) {
	if len(args) < 1 {
		return "", nil, fmt.Errorf("%s expects query", call.Callee.String())
	}
	query, ok := args[0].(runtime.String)
	if !ok {
		return "", nil, fmt.Errorf("%s query must be string", call.Callee.String())
	}
	normalized, paramNames, err := dbNormalizeQuery(query.Value, driver)
	if err != nil {
		return "", nil, err
	}
	sqlArgs, err := dbBindArgs(args[1:], paramNames)
	if err != nil {
		return "", nil, fmt.Errorf("%s %v", call.Callee.String(), err)
	}
	return normalized, sqlArgs, nil
}

func dbNormalizeQuery(query, driver string) (string, []string, error) {
	var out strings.Builder
	names := []string{}
	index := 1
	for i := 0; i < len(query); {
		ch := query[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			next := copyQuotedSQL(&out, query, i, ch)
			i = next
			continue
		}
		if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
			next := copySQLUntilNewline(&out, query, i)
			i = next
			continue
		}
		if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
			next := copySQLBlockComment(&out, query, i)
			i = next
			continue
		}
		if ch == '?' {
			out.WriteString(dbPlaceholder(driver, index))
			index++
			i++
			continue
		}
		if ch == ':' && i+1 < len(query) && query[i+1] != ':' && isDBParamStart(query[i+1]) {
			j := i + 2
			for j < len(query) && isDBParamPart(query[j]) {
				j++
			}
			names = append(names, query[i+1:j])
			out.WriteString(dbPlaceholder(driver, index))
			index++
			i = j
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String(), names, nil
}

func copyQuotedSQL(out *strings.Builder, query string, start int, quote byte) int {
	out.WriteByte(query[start])
	for i := start + 1; i < len(query); i++ {
		out.WriteByte(query[i])
		if query[i] == quote {
			if i+1 < len(query) && query[i+1] == quote {
				i++
				out.WriteByte(query[i])
				continue
			}
			return i + 1
		}
		if query[i] == '\\' && i+1 < len(query) {
			i++
			out.WriteByte(query[i])
		}
	}
	return len(query)
}

func copySQLUntilNewline(out *strings.Builder, query string, start int) int {
	for i := start; i < len(query); i++ {
		out.WriteByte(query[i])
		if query[i] == '\n' {
			return i + 1
		}
	}
	return len(query)
}

func copySQLBlockComment(out *strings.Builder, query string, start int) int {
	out.WriteString("/*")
	for i := start + 2; i < len(query); i++ {
		out.WriteByte(query[i])
		if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
			out.WriteByte('/')
			return i + 2
		}
	}
	return len(query)
}

func isDBParamStart(ch byte) bool {
	return ch == '_' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func isDBParamPart(ch byte) bool {
	return isDBParamStart(ch) || (ch >= '0' && ch <= '9')
}

func dbBindArgs(values []runtime.Value, paramNames []string) ([]any, error) {
	if len(values) == 1 {
		switch value := values[0].(type) {
		case *runtime.List:
			return dbArgs(value.Elements)
		case runtime.Dict:
			if len(paramNames) == 0 {
				return nil, fmt.Errorf("named parameter dict requires :name placeholders")
			}
			return dbNamedArgs(value, paramNames)
		}
	}
	return dbArgs(values)
}

func dbPreparedArgs(values []runtime.Value, paramNames []string) ([]any, error) {
	return dbBindArgs(values, paramNames)
}

func dbNamedArgs(dict runtime.Dict, names []string) ([]any, error) {
	args := make([]any, 0, len(names))
	for _, name := range names {
		value, ok := dictField(dict, name)
		if !ok {
			return nil, fmt.Errorf("missing named database parameter %q", name)
		}
		arg, err := dbArg(value)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func dbArgs(values []runtime.Value) ([]any, error) {
	args := make([]any, 0, len(values))
	for _, value := range values {
		arg, err := dbArg(value)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	return args, nil
}

func dbArg(value runtime.Value) (any, error) {
	switch value := value.(type) {
	case runtime.Null:
		return nil, nil
	case runtime.Bool:
		return value.Value, nil
	case runtime.SmallInt:
		return value.Value, nil
	case runtime.Int:
		if !value.Value.IsInt64() {
			return nil, fmt.Errorf("database int argument is out of int64 range")
		}
		return value.Value.Int64(), nil
	case runtime.Decimal:
		return value.Value.FloatString(10), nil
	case runtime.Float:
		return value.Value, nil
	case runtime.String:
		return value.Value, nil
	case runtime.Bytes:
		return value.Value, nil
	default:
		return nil, fmt.Errorf("unsupported database argument type %s", value.TypeName())
	}
}

func sqlValueToRuntime(value any) (runtime.Value, error) {
	switch value := value.(type) {
	case nil:
		return runtime.Null{}, nil
	case bool:
		return runtime.Bool{Value: value}, nil
	case int64:
		return runtime.NewInt64(value), nil
	case int:
		return runtime.NewInt64(int64(value)), nil
	case int32:
		return runtime.NewInt64(int64(value)), nil
	case int16:
		return runtime.NewInt64(int64(value)), nil
	case int8:
		return runtime.NewInt64(int64(value)), nil
	case uint64:
		return runtime.Int{Value: new(big.Int).SetUint64(value)}, nil
	case uint:
		return runtime.Int{Value: new(big.Int).SetUint64(uint64(value))}, nil
	case uint32:
		return runtime.NewInt64(int64(value)), nil
	case uint16:
		return runtime.NewInt64(int64(value)), nil
	case uint8:
		return runtime.NewInt64(int64(value)), nil
	case float64:
		return runtime.Float{Value: value}, nil
	case float32:
		return runtime.Float{Value: float64(value)}, nil
	case string:
		return runtime.String{Value: value}, nil
	case []byte:
		return runtime.Bytes{Value: value}, nil
	case time.Time:
		return runtime.String{Value: value.UTC().Format(time.RFC3339Nano)}, nil
	default:
		return nil, fmt.Errorf("unsupported database value type %T", value)
	}
}
