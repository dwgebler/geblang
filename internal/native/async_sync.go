package native

import (
	"fmt"
	"sync"

	"geblang/internal/runtime"
)

// Native handle modules for the C2-C4 synchronisation primitives.
//
// `async.sync` exposes Mutex / RWMutex / Semaphore / WaitGroup.
// `async.atomic` exposes AtomicInt / AtomicBool (in async_atomic.go).
//
// Each constructor returns an opaque NativeObject keyed by an ID;
// the Geblang-side wrapper class in `stdlib/async/sync.gb` carries
// the handle and delegates method calls to the free functions
// registered here.

// Sync primitive handles (mutexes, semaphores, wait groups, atomics) carry
// the Go value they own as their Payload, so it is freed with the last
// reference to the handle; nothing is kept in a process-wide registry.
var (
	syncMu     sync.Mutex
	syncNextID int64
)

func registerAsyncSync(r *Registry) {
	registerAsyncSyncMutex(r)
	registerAsyncSyncRWMutex(r)
	registerAsyncSyncSemaphore(r)
	registerAsyncSyncWaitGroup(r)
}

// ---- Mutex ----

func registerAsyncSyncMutex(r *Registry) {
	r.Register("async.sync", "mutexNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("async.sync.mutexNew expects no arguments")
		}
		return runtime.NativeObject{Kind: "AsyncMutex", ID: nextSyncID(), Payload: &sync.Mutex{}}, nil
	})
	r.Register("async.sync", "mutexLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupMutex(args, "async.sync.mutexLock")
		if err != nil {
			return nil, err
		}
		m.Lock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "mutexUnlock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupMutex(args, "async.sync.mutexUnlock")
		if err != nil {
			return nil, err
		}
		m.Unlock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "mutexTryLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupMutex(args, "async.sync.mutexTryLock")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: m.TryLock()}, nil
	})
}

func lookupMutex(args []runtime.Value, label string) (*sync.Mutex, error) {
	obj, err := singleHandleObject(args, "AsyncMutex", label)
	if err != nil {
		return nil, err
	}
	m, ok := obj.Payload.(*sync.Mutex)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return m, nil
}

// ---- RWMutex ----

func registerAsyncSyncRWMutex(r *Registry) {
	r.Register("async.sync", "rwmutexNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("async.sync.rwmutexNew expects no arguments")
		}
		return runtime.NativeObject{Kind: "AsyncRWMutex", ID: nextSyncID(), Payload: &sync.RWMutex{}}, nil
	})
	r.Register("async.sync", "rwmutexLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexLock")
		if err != nil {
			return nil, err
		}
		m.Lock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "rwmutexUnlock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexUnlock")
		if err != nil {
			return nil, err
		}
		m.Unlock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "rwmutexTryLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexTryLock")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: m.TryLock()}, nil
	})
	r.Register("async.sync", "rwmutexRLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexRLock")
		if err != nil {
			return nil, err
		}
		m.RLock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "rwmutexRUnlock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexRUnlock")
		if err != nil {
			return nil, err
		}
		m.RUnlock()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "rwmutexTryRLock", func(args []runtime.Value) (runtime.Value, error) {
		m, err := lookupRWMutex(args, "async.sync.rwmutexTryRLock")
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: m.TryRLock()}, nil
	})
}

func lookupRWMutex(args []runtime.Value, label string) (*sync.RWMutex, error) {
	obj, err := singleHandleObject(args, "AsyncRWMutex", label)
	if err != nil {
		return nil, err
	}
	m, ok := obj.Payload.(*sync.RWMutex)
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return m, nil
}

// ---- Semaphore ----
//
// A counted semaphore backed by a buffered channel. acquire reads
// one slot (blocks if empty); release writes one slot (errors on
// overflow). Permits are an upper bound on outstanding holders.

func registerAsyncSyncSemaphore(r *Registry) {
	r.Register("async.sync", "semaphoreNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("async.sync.semaphoreNew expects one int permits argument")
		}
		permits, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("async.sync.semaphoreNew permits must be int")
		}
		if permits < 1 {
			return nil, fmt.Errorf("async.sync.semaphoreNew permits must be >= 1")
		}
		ch := make(chan struct{}, int(permits))
		for i := int64(0); i < permits; i++ {
			ch <- struct{}{}
		}
		return runtime.NativeObject{Kind: "AsyncSemaphore", ID: nextSyncID(), Payload: ch}, nil
	})
	r.Register("async.sync", "semaphoreAcquire", func(args []runtime.Value) (runtime.Value, error) {
		ch, err := lookupSemaphore(args, "async.sync.semaphoreAcquire")
		if err != nil {
			return nil, err
		}
		<-ch
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "semaphoreRelease", func(args []runtime.Value) (runtime.Value, error) {
		ch, err := lookupSemaphore(args, "async.sync.semaphoreRelease")
		if err != nil {
			return nil, err
		}
		select {
		case ch <- struct{}{}:
			return runtime.Null{}, nil
		default:
			return nil, fmt.Errorf("async.sync.semaphoreRelease: more releases than acquires")
		}
	})
	r.Register("async.sync", "semaphoreTryAcquire", func(args []runtime.Value) (runtime.Value, error) {
		ch, err := lookupSemaphore(args, "async.sync.semaphoreTryAcquire")
		if err != nil {
			return nil, err
		}
		select {
		case <-ch:
			return runtime.Bool{Value: true}, nil
		default:
			return runtime.Bool{Value: false}, nil
		}
	})
}

func lookupSemaphore(args []runtime.Value, label string) (chan struct{}, error) {
	obj, err := singleHandleObject(args, "AsyncSemaphore", label)
	if err != nil {
		return nil, err
	}
	ch, ok := obj.Payload.(chan struct{})
	if !ok {
		return nil, fmt.Errorf("%s: unknown handle", label)
	}
	return ch, nil
}

// ---- WaitGroup ----

func registerAsyncSyncWaitGroup(r *Registry) {
	r.Register("async.sync", "waitgroupNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("async.sync.waitgroupNew expects no arguments")
		}
		return runtime.NativeObject{Kind: "AsyncWaitGroup", ID: nextSyncID(), Payload: &sync.WaitGroup{}}, nil
	})
	r.Register("async.sync", "waitgroupAdd", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("async.sync.waitgroupAdd expects (handle, delta)")
		}
		obj, ok := args[0].(runtime.NativeObject)
		if !ok || obj.Kind != "AsyncWaitGroup" {
			return nil, fmt.Errorf("async.sync.waitgroupAdd: not a WaitGroup handle")
		}
		delta, ok := AsInt64(args[1])
		if !ok {
			return nil, fmt.Errorf("async.sync.waitgroupAdd delta must be int")
		}
		wg, err := waitGroupPayload(obj)
		if err != nil {
			return nil, fmt.Errorf("async.sync.waitgroupAdd: %s", err.Error())
		}
		wg.Add(int(delta))
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "waitgroupDone", func(args []runtime.Value) (runtime.Value, error) {
		wg, err := lookupWaitGroup(args, "async.sync.waitgroupDone")
		if err != nil {
			return nil, err
		}
		wg.Done()
		return runtime.Null{}, nil
	})
	r.Register("async.sync", "waitgroupWait", func(args []runtime.Value) (runtime.Value, error) {
		wg, err := lookupWaitGroup(args, "async.sync.waitgroupWait")
		if err != nil {
			return nil, err
		}
		wg.Wait()
		return runtime.Null{}, nil
	})
}

func lookupWaitGroup(args []runtime.Value, label string) (*sync.WaitGroup, error) {
	obj, err := singleHandleObject(args, "AsyncWaitGroup", label)
	if err != nil {
		return nil, err
	}
	return waitGroupPayload(obj)
}

func waitGroupPayload(obj runtime.NativeObject) (*sync.WaitGroup, error) {
	wg, ok := obj.Payload.(*sync.WaitGroup)
	if !ok {
		return nil, fmt.Errorf("unknown handle")
	}
	return wg, nil
}

// ---- helpers ----

func nextSyncID() int64 {
	syncMu.Lock()
	syncNextID++
	id := syncNextID
	syncMu.Unlock()
	return id
}

// singleHandleObject checks that args is exactly one handle of kind and
// returns it; the handle's Payload holds the native value it owns.
func singleHandleObject(args []runtime.Value, kind, label string) (runtime.NativeObject, error) {
	if len(args) != 1 {
		return runtime.NativeObject{}, fmt.Errorf("%s expects (handle)", label)
	}
	obj, ok := args[0].(runtime.NativeObject)
	if !ok || obj.Kind != kind {
		return runtime.NativeObject{}, fmt.Errorf("%s: argument is not a %s handle", label, kind)
	}
	return obj, nil
}

func singleHandle(args []runtime.Value, kind, label string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s expects (handle)", label)
	}
	obj, ok := args[0].(runtime.NativeObject)
	if !ok || obj.Kind != kind {
		return 0, fmt.Errorf("%s: argument is not a %s handle", label, kind)
	}
	return obj.ID, nil
}
