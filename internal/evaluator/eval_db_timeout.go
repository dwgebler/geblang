package evaluator

import (
	"context"
	"fmt"
	"math"
	"time"

	"geblang/internal/runtime"
)

func dbTimeoutContext(value runtime.Value) (context.Context, context.CancelFunc, error) {
	ms, err := rawInt64(value, "timeoutMs")
	if err != nil {
		return nil, nil, err
	}
	if ms <= 0 || ms > math.MaxInt64/int64(time.Millisecond) {
		return nil, nil, fmt.Errorf("timeoutMs must be a positive integer within duration range")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(ms)*time.Millisecond)
	return ctx, cancel, nil
}

func (e *Evaluator) dbTimedOperation(handle runtime.Value, args []runtime.Value, queryRows bool) (runtime.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("database timed operation expects timeoutMs, query and optional parameters")
	}
	db, err := e.dbHandle(handle)
	if err != nil {
		return nil, err
	}
	driver, err := e.dbDriverFromValue(handle)
	if err != nil {
		return nil, err
	}
	query, params, err := dbStandardQueryAndArgs(syntheticCall("db.Connection"), driver, args[1:])
	if err != nil {
		return nil, err
	}
	ctx, cancel, err := dbTimeoutContext(args[0])
	if err != nil {
		return nil, err
	}
	if queryRows {
		rows, err := db.QueryContext(ctx, query, params...)
		if err != nil {
			cancel()
			return nil, err
		}
		// Streaming rows own the deadline until they are closed or exhausted.
		return e.registerDBRowsContext(rows, cancel)
	}
	defer cancel()
	result, err := db.ExecContext(ctx, query, params...)
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
