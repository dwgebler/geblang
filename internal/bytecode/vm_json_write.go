package bytecode

import (
	"fmt"
	"geblang/internal/native"
	"geblang/internal/runtime"
)

func (vm *VM) writeJSONBytes(stream runtime.Value, data []byte) (int, error) {
	result, err := vm.statefulNativeCall("io", "writeBytes", []runtime.Value{stream, runtime.Bytes{Value: data}}, nil)
	if err != nil {
		return 0, err
	}
	count, ok := native.IntValueToBigInt(result)
	if !ok || !count.IsInt64() {
		return 0, fmt.Errorf("json.write: stream must return an integer write count")
	}
	n := count.Int64()
	if n < 0 || n > int64(len(data)) {
		return 0, runtime.ClassifiedError{Class: "ValueError", Message: "json.write: invalid write count"}
	}
	return int(n), nil
}
