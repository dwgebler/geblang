package bytecode

import (
	"io"
	"strings"
	"testing"

	"geblang/internal/runtime"
)

func TestVMAsyncTaskPanicBecomesTaskError(t *testing.T) {
	vm := NewVM(Chunk{}, io.Discard)
	panicking := runtime.Function{Name: "explode", Native: func(*runtime.Instance, []runtime.Value) (runtime.Value, error) { panic("boom") }}
	result := vm.startAsyncCallableWithForwardThis(panicking, nil, nil).Await()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "internal error: boom") {
		t.Fatalf("task result = %#v", result)
	}
}
