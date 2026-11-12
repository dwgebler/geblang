package native

import (
	"errors"
	"testing"

	"geblang/internal/runtime"
)

func TestWaitAnyTimeoutPreservesTaskOwnership(t *testing.T) {
	registry := NewBuiltinRegistry()
	task := runtime.NewTask()
	tasks := &runtime.List{Elements: []runtime.Value{task}}

	value, err := registry.Call("async", "waitAny", []runtime.Value{tasks, runtime.SmallInt{Value: 1}})

	if err != nil || value.Inspect() != "-1" {
		t.Fatalf("wait: %v %v", value, err)
	}
	if task.Done() || task.Cancelled() {
		t.Fatal("waiting changed the pending task")
	}
	task.Complete(runtime.SmallInt{Value: 7}, nil)
	if task.Await().Value.Inspect() != "7" {
		t.Fatal("task result was lost")
	}
}

func TestWaitAnyReportsCompletionWithoutConsumingFailure(t *testing.T) {
	registry := NewBuiltinRegistry()
	pending := runtime.NewTask()
	complete := runtime.NewTask()
	failure := errors.New("handler failed")
	complete.Complete(runtime.Null{}, failure)

	value, err := registry.Call("async", "waitAny", []runtime.Value{&runtime.List{Elements: []runtime.Value{pending, complete}}, runtime.SmallInt{Value: 1000}})

	if err != nil || value.Inspect() != "1" {
		t.Fatalf("wait: %v %v", value, err)
	}
	if !errors.Is(complete.Await().Err, failure) {
		t.Fatal("failure was consumed")
	}
	if pending.Done() {
		t.Fatal("sibling was cancelled")
	}
	pending.Complete(runtime.Null{}, nil)
}

func TestWaitAnyRejectsInvalidInputsBeforeWaiting(t *testing.T) {
	registry := NewBuiltinRegistry()
	for _, args := range [][]runtime.Value{
		{},
		{runtime.Null{}, runtime.SmallInt{Value: 0}},
		{&runtime.List{}, runtime.SmallInt{Value: -1}},
		{&runtime.List{Elements: []runtime.Value{runtime.Null{}}}, runtime.SmallInt{Value: 0}},
	} {
		if _, err := registry.Call("async", "waitAny", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
