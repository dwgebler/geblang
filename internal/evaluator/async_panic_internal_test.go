package evaluator

import (
	"io"
	"strings"
	"testing"

	"geblang/internal/runtime"
)

func TestAsyncTaskPanicBecomesTaskError(t *testing.T) {
	e := New(io.Discard)
	panicking := runtime.Function{Name: "explode", Native: func(*runtime.Instance, []runtime.Value) (runtime.Value, error) { panic("boom") }}
	result := e.startAsyncFunction(panicking, nil, nil).Await()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "internal error: boom") {
		t.Fatalf("task result = %#v", result)
	}
}
