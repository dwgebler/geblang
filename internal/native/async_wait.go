package native

import (
	"fmt"
	"reflect"
	"time"

	"geblang/internal/runtime"
)

func registerAsyncWait(r *Registry) {
	r.Register("async", "waitAny", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("async.waitAny expects tasks and timeoutMs")
		}
		list, ok := args[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("async.waitAny tasks must be a list")
		}
		if len(list.Elements) > 65535 {
			return nil, fmt.Errorf("async.waitAny accepts at most 65535 tasks")
		}
		timeout, ok := AsInt64(args[1])
		if !ok || timeout < 0 || timeout > 2147483647 {
			return nil, fmt.Errorf("async.waitAny timeoutMs must be a nonnegative 32-bit integer")
		}
		cases := make([]reflect.SelectCase, len(list.Elements), len(list.Elements)+1)
		for i, value := range list.Elements {
			task, ok := value.(*runtime.Task)
			if !ok {
				return nil, fmt.Errorf("async.waitAny task %d is not a Task", i)
			}
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(task.DoneChan())}
		}
		for i, value := range list.Elements {
			if value.(*runtime.Task).Done() {
				return runtime.SmallInt{Value: int64(i)}, nil
			}
		}
		if timeout == 0 {
			return runtime.SmallInt{Value: -1}, nil
		}
		timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)
		defer timer.Stop()
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timer.C)})
		selected, _, _ := reflect.Select(cases)
		if selected == len(list.Elements) {
			return runtime.SmallInt{Value: -1}, nil
		}
		return runtime.SmallInt{Value: int64(selected)}, nil
	})
}
