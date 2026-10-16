package evaluator

import (
	"fmt"
	"geblang/internal/ast"
	"geblang/internal/runtime"
	"time"
)

func (e *Evaluator) asyncRun(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one function argument", call.Callee.String())
	}
	fn, ok := args[0].(runtime.Function)
	if !ok {
		return nil, fmt.Errorf("%s expects a function", call.Callee.String())
	}
	return e.startAsyncFunction(fn, nil, nil), nil
}

// asyncToken returns a fresh uncompleted Task whose only purpose is
// to carry a cancellation signal. Lets concurrent code share a
// cancellation point without mutating instance fields across
// goroutines.
func asyncToken(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s takes no arguments", call.Callee.String())
	}
	return runtime.NewTask(), nil
}

func asyncSleep(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one argument (milliseconds)", call.Callee.String())
	}
	duration, err := sleepDuration(args[0], call.Callee.String())
	if err != nil {
		return nil, err
	}
	task := runtime.NewTask()
	go func() {
		if duration > 0 {
			time.Sleep(duration)
		}
		task.Complete(runtime.Null{}, nil)
	}()
	return task, nil
}

func asyncAwait(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one task", call.Callee.String())
	}
	return awaitValue(args[0])
}

func asyncDone(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one task", call.Callee.String())
	}
	task, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("%s expects Task", call.Callee.String())
	}
	return runtime.Bool{Value: task.Done()}, nil
}

func asyncCancel(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one task", call.Callee.String())
	}
	task, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("%s expects Task", call.Callee.String())
	}
	task.Cancel()
	return runtime.Null{}, nil
}

func asyncTimeout(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("%s expects (task, milliseconds)", call.Callee.String())
	}
	inner, ok := args[0].(*runtime.Task)
	if !ok {
		return nil, fmt.Errorf("%s expects a Task as the first argument", call.Callee.String())
	}
	duration, err := sleepDuration(args[1], call.Callee.String())
	if err != nil {
		return nil, err
	}
	out := runtime.NewTask()
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-inner.DoneChan():
			result := inner.Await()
			out.Complete(result.Value, result.Err)
		case <-timer.C:
			inner.Cancel()
			out.Complete(runtime.Null{}, runtime.ClassifiedError{Class: "TimeoutError", Message: fmt.Sprintf("async.timeout: task did not complete within %v", duration)})
		}
	}()
	return out, nil
}

func asyncAll(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one list of tasks", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects a list of tasks", call.Callee.String())
	}
	tasks := make([]*runtime.Task, 0, len(list.Elements))
	for i, el := range list.Elements {
		task, ok := el.(*runtime.Task)
		if !ok {
			return nil, fmt.Errorf("%s: element %d is not a Task", call.Callee.String(), i)
		}
		tasks = append(tasks, task)
	}
	out := runtime.NewTask()
	go func() {
		results := make([]runtime.Value, len(tasks))
		for i, t := range tasks {
			r := t.Await()
			if r.Err != nil {
				// Cancel all siblings so they stop wasting work.
				for j, sibling := range tasks {
					if j != i {
						sibling.Cancel()
					}
				}
				out.Complete(runtime.Null{}, r.Err)
				return
			}
			results[i] = r.Value
		}
		out.Complete(&runtime.List{Elements: results}, nil)
	}()
	return out, nil
}

func asyncRace(call *ast.CallExpression, args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects one list of tasks", call.Callee.String())
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects a list of tasks", call.Callee.String())
	}
	if len(list.Elements) == 0 {
		return nil, fmt.Errorf("%s requires at least one task", call.Callee.String())
	}
	tasks := make([]*runtime.Task, 0, len(list.Elements))
	for i, el := range list.Elements {
		task, ok := el.(*runtime.Task)
		if !ok {
			return nil, fmt.Errorf("%s: element %d is not a Task", call.Callee.String(), i)
		}
		tasks = append(tasks, task)
	}
	out := runtime.NewTask()
	go func() {
		// Race all DoneChans; whichever fires first wins.
		winner := make(chan int, len(tasks))
		for i, t := range tasks {
			i, t := i, t
			go func() {
				<-t.DoneChan()
				winner <- i
			}()
		}
		first := <-winner
		for j, sibling := range tasks {
			if j != first {
				sibling.Cancel()
			}
		}
		r := tasks[first].Await()
		out.Complete(r.Value, r.Err)
	}()
	return out, nil
}

func sleepDuration(value runtime.Value, label string) (time.Duration, error) {
	switch v := value.(type) {
	case runtime.SmallInt:
		if v.Value <= 0 {
			return 0, nil
		}
		return time.Duration(v.Value) * time.Millisecond, nil
	case runtime.Int:
		if !v.Value.IsInt64() {
			return 0, fmt.Errorf("%s millisecond value is out of int64 range", label)
		}
		ms := v.Value.Int64()
		if ms <= 0 {
			return 0, nil
		}
		return time.Duration(ms) * time.Millisecond, nil
	case runtime.Float:
		if v.Value <= 0 {
			return 0, nil
		}
		return time.Duration(v.Value * float64(time.Millisecond)), nil
	default:
		return 0, fmt.Errorf("%s expects a numeric millisecond value", label)
	}
}
