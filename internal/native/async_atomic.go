package native

import (
	"fmt"
	"sync/atomic"

	"geblang/internal/runtime"
)

func registerAsyncAtomic(r *Registry) {
	r.Register("async.atomic", "intNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("async.atomic.intNew expects (initial)")
		}
		initial, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("async.atomic.intNew initial must be int")
		}
		id := nextSyncID()
		v := &atomic.Int64{}
		v.Store(initial)
		return runtime.NativeObject{Kind: "AsyncAtomicInt", ID: id, Payload: v}, nil
	})
	r.Register("async.atomic", "intLoad", func(args []runtime.Value) (runtime.Value, error) {
		v, err := lookupAtomicInt(args, "async.atomic.intLoad")
		if err != nil {
			return nil, err
		}
		return runtime.SmallInt{Value: v.Load()}, nil
	})
	r.Register("async.atomic", "intStore", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("async.atomic.intStore expects (handle, value)")
		}
		v, err := atomicIntFromHandle(args[0], "async.atomic.intStore")
		if err != nil {
			return nil, err
		}
		next, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("async.atomic.intStore value must be int")
		}
		v.Store(next)
		return runtime.Null{}, nil
	})
	r.Register("async.atomic", "intAdd", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("async.atomic.intAdd expects (handle, delta)")
		}
		v, err := atomicIntFromHandle(args[0], "async.atomic.intAdd")
		if err != nil {
			return nil, err
		}
		delta, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("async.atomic.intAdd delta must be int")
		}
		return runtime.SmallInt{Value: v.Add(delta)}, nil
	})
	r.Register("async.atomic", "intCompareAndSwap", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("async.atomic.intCompareAndSwap expects (handle, old, new)")
		}
		v, err := atomicIntFromHandle(args[0], "async.atomic.intCompareAndSwap")
		if err != nil {
			return nil, err
		}
		old, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("async.atomic.intCompareAndSwap old must be int")
		}
		next, ok := AsInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("async.atomic.intCompareAndSwap new must be int")
		}
		return runtime.Bool{Value: v.CompareAndSwap(old, next)}, nil
	})

	r.Register("async.atomic", "boolNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("async.atomic.boolNew expects (initial)")
		}
		initial, ok := args[0].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("async.atomic.boolNew initial must be bool")
		}
		id := nextSyncID()
		v := &atomic.Bool{}
		v.Store(initial.Value)
		return runtime.NativeObject{Kind: "AsyncAtomicBool", ID: id, Payload: v}, nil
	})
	r.Register("async.atomic", "boolLoad", func(args []runtime.Value) (runtime.Value, error) {
		v, err := lookupAtomicBool(args, "async.atomic.boolLoad")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: v.Load()}, nil
	})
	r.Register("async.atomic", "boolStore", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("async.atomic.boolStore expects (handle, value)")
		}
		v, err := atomicBoolFromHandle(args[0], "async.atomic.boolStore")
		if err != nil {
			return nil, err
		}
		next, ok := args[1].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("async.atomic.boolStore value must be bool")
		}
		v.Store(next.Value)
		return runtime.Null{}, nil
	})
	r.Register("async.atomic", "boolCompareAndSwap", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("async.atomic.boolCompareAndSwap expects (handle, old, new)")
		}
		v, err := atomicBoolFromHandle(args[0], "async.atomic.boolCompareAndSwap")
		if err != nil {
			return nil, err
		}
		old, ok := args[1].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("async.atomic.boolCompareAndSwap old must be bool")
		}
		next, ok := args[2].(runtime.Bool)
		if !ok {
			return nil, fmt.Errorf("async.atomic.boolCompareAndSwap new must be bool")
		}
		return runtime.Bool{Value: v.CompareAndSwap(old.Value, next.Value)}, nil
	})
}

func lookupAtomicInt(args []runtime.Value, label string) (*atomic.Int64, error) {
	obj, err := singleHandleObject(args, "AsyncAtomicInt", label)
	if err != nil {
		return nil, err
	}
	v, ok := obj.Payload.(*atomic.Int64)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return v, nil
}

func atomicIntFromHandle(v runtime.Value, label string) (*atomic.Int64, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "AsyncAtomicInt" {
		return nil, fmt.Errorf("%s: argument is not an AtomicInt handle", label)
	}
	a, ok := obj.Payload.(*atomic.Int64)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return a, nil
}

func lookupAtomicBool(args []runtime.Value, label string) (*atomic.Bool, error) {
	obj, err := singleHandleObject(args, "AsyncAtomicBool", label)
	if err != nil {
		return nil, err
	}
	v, ok := obj.Payload.(*atomic.Bool)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return v, nil
}

func atomicBoolFromHandle(v runtime.Value, label string) (*atomic.Bool, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "AsyncAtomicBool" {
		return nil, fmt.Errorf("%s: argument is not an AtomicBool handle", label)
	}
	a, ok := obj.Payload.(*atomic.Bool)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return a, nil
}
