package native

import (
	"fmt"
	"sort"
	"sync"

	"geblang/internal/runtime"
)

// concurrentStore is the thread-safe shared map backing the `store` module.
// Values are deep-copied in and out (copy-in/copy-out), so a stored value is an
// isolated snapshot and a caller cannot mutate shared state outside the lock.
// Deeply frozen values can never change, so they are stored and handed out by
// reference instead; copies of other values keep their frozen flags.
type concurrentStore struct {
	mu   sync.RWMutex
	data map[string]storeEntry
}

type storeEntry struct {
	key   runtime.Value
	value runtime.Value
	// shared marks a deeply frozen value, handed out without copying.
	shared bool
}

// newStoreEntry snapshots value for storage: deeply frozen values are kept
// by reference, anything else is deep-copied.
func newStoreEntry(key, value runtime.Value) storeEntry {
	if runtime.IsDeepFrozen(value) {
		return storeEntry{key: key, value: value, shared: true}
	}
	return storeEntry{key: key, value: runtime.CloneSnapshot(value)}
}

// read returns the stored value for a caller: the value itself when it is
// deeply frozen, otherwise a fresh copy.
func (e storeEntry) read() runtime.Value {
	if e.shared {
		return e.value
	}
	return runtime.CloneSnapshot(e.value)
}

func registerStore(r *Registry) {
	r.Register("store", "new", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("store.new expects ()")
		}
		// The handle owns its store (Payload), so the store is freed with
		// the last reference to the handle.
		return runtime.NativeObject{Kind: "Store", ID: nextSyncID(), Payload: &concurrentStore{data: map[string]storeEntry{}}}, nil
	})

	r.Register("store", "set", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("store.set expects (handle, key, value)")
		}
		s, err := storeFromHandle(args[0], "store.set")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.Lock()
		s.data[ks] = newStoreEntry(args[1], args[2])
		s.mu.Unlock()
		return runtime.Null{}, nil
	})

	r.Register("store", "get", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("store.get expects (handle, key)")
		}
		s, err := storeFromHandle(args[0], "store.get")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.RLock()
		entry, ok := s.data[ks]
		s.mu.RUnlock()
		if !ok {
			return runtime.Null{}, nil
		}
		return entry.read(), nil
	})

	r.Register("store", "has", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("store.has expects (handle, key)")
		}
		s, err := storeFromHandle(args[0], "store.has")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.RLock()
		_, ok := s.data[ks]
		s.mu.RUnlock()
		return runtime.Bool{Value: ok}, nil
	})

	r.Register("store", "delete", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("store.delete expects (handle, key)")
		}
		s, err := storeFromHandle(args[0], "store.delete")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.Lock()
		_, ok := s.data[ks]
		delete(s.data, ks)
		s.mu.Unlock()
		return runtime.Bool{Value: ok}, nil
	})

	r.Register("store", "clear", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("store.clear expects (handle)")
		}
		s, err := storeFromHandle(args[0], "store.clear")
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.data = map[string]storeEntry{}
		s.mu.Unlock()
		return runtime.Null{}, nil
	})

	r.Register("store", "len", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("store.len expects (handle)")
		}
		s, err := storeFromHandle(args[0], "store.len")
		if err != nil {
			return nil, err
		}
		s.mu.RLock()
		n := len(s.data)
		s.mu.RUnlock()
		return runtime.SmallInt{Value: int64(n)}, nil
	})

	r.Register("store", "keys", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("store.keys expects (handle)")
		}
		s, err := storeFromHandle(args[0], "store.keys")
		if err != nil {
			return nil, err
		}
		return &runtime.List{Elements: s.snapshotKeys()}, nil
	})

	r.Register("store", "values", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("store.values expects (handle)")
		}
		s, err := storeFromHandle(args[0], "store.values")
		if err != nil {
			return nil, err
		}
		return &runtime.List{Elements: s.snapshotValues()}, nil
	})

	r.Register("store", "incr", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("store.incr expects (handle, key, by)")
		}
		s, err := storeFromHandle(args[0], "store.incr")
		if err != nil {
			return nil, err
		}
		by, ok := AsInt64(args[2])
		if !ok {
			return nil, fmt.Errorf("store.incr: by must be int")
		}
		ks := DictKey(args[1])
		s.mu.Lock()
		defer s.mu.Unlock()
		var base int64
		if entry, ok := s.data[ks]; ok {
			base, ok = AsInt64(entry.value)
			if !ok {
				return nil, fmt.Errorf("store.incr: value at key is not int")
			}
		}
		next := runtime.SmallInt{Value: base + by}
		s.data[ks] = storeEntry{key: args[1], value: next}
		return next, nil
	})

	r.Register("store", "getOrSet", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("store.getOrSet expects (handle, key, default)")
		}
		s, err := storeFromHandle(args[0], "store.getOrSet")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.Lock()
		defer s.mu.Unlock()
		if entry, ok := s.data[ks]; ok {
			return entry.read(), nil
		}
		entry := newStoreEntry(args[1], args[2])
		s.data[ks] = entry
		return entry.read(), nil
	})

	r.Register("store", "compareAndSet", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 4 {
			return nil, fmt.Errorf("store.compareAndSet expects (handle, key, expected, next)")
		}
		s, err := storeFromHandle(args[0], "store.compareAndSet")
		if err != nil {
			return nil, err
		}
		ks := DictKey(args[1])
		s.mu.Lock()
		defer s.mu.Unlock()
		var current runtime.Value = runtime.Null{}
		if entry, ok := s.data[ks]; ok {
			current = entry.value
		}
		if !runtime.ValuesEqual(current, args[2]) {
			return runtime.Bool{Value: false}, nil
		}
		s.data[ks] = newStoreEntry(args[1], args[3])
		return runtime.Bool{Value: true}, nil
	})
}

func storeFromHandle(v runtime.Value, label string) (*concurrentStore, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "Store" {
		return nil, fmt.Errorf("%s: argument is not a Store handle", label)
	}
	s, ok := obj.Payload.(*concurrentStore)
	if !ok {
		return nil, fmt.Errorf("%s: unknown Store handle", label)
	}
	return s, nil
}

// sortedKeys returns the encoded keys in a deterministic order; Go map iteration
// is randomized, so keys/values snapshots sort to keep output reproducible.
func (s *concurrentStore) sortedKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ks := make([]string, 0, len(s.data))
	for k := range s.data {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (s *concurrentStore) snapshotKeys() []runtime.Value {
	ks := s.sortedKeys()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]runtime.Value, 0, len(ks))
	for _, k := range ks {
		out = append(out, runtime.CloneSnapshot(s.data[k].key))
	}
	return out
}

func (s *concurrentStore) snapshotValues() []runtime.Value {
	ks := s.sortedKeys()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]runtime.Value, 0, len(ks))
	for _, k := range ks {
		out = append(out, s.data[k].read())
	}
	return out
}
