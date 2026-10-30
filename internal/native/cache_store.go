package native

import (
	"container/heap"
	"container/list"
	"fmt"
	"geblang/internal/runtime"
	"math/big"
	"sync"
	"time"
)

type cachePayloadEntry struct {
	key         string
	payload     runtime.String
	expires     *big.Int
	recent      *list.Element
	expiryIndex int
}
type cacheExpiry []*cachePayloadEntry

func (h cacheExpiry) Len() int           { return len(h) }
func (h cacheExpiry) Less(i, j int) bool { return h[i].expires.Cmp(h[j].expires) < 0 }
func (h cacheExpiry) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].expiryIndex = i
	h[j].expiryIndex = j
}
func (h *cacheExpiry) Push(value any) {
	entry := value.(*cachePayloadEntry)
	entry.expiryIndex = len(*h)
	*h = append(*h, entry)
}
func (h *cacheExpiry) Pop() any {
	old := *h
	entry := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	entry.expiryIndex = -1
	return entry
}

// Native handles keep cache state shared when request snapshots copy the wrapper.
type boundedCacheStore struct {
	mu       sync.Mutex
	capacity *big.Int
	entries  map[string]*cachePayloadEntry
	recent   list.List
	expiry   cacheExpiry
	now      func() time.Time
}

func (s *boundedCacheStore) remove(entry *cachePayloadEntry) {
	delete(s.entries, entry.key)
	s.recent.Remove(entry.recent)
	heap.Remove(&s.expiry, entry.expiryIndex)
}
func cacheDeadlinePassed(deadline *big.Int, now int64) bool {
	if deadline.IsInt64() {
		return deadline.Int64() <= now
	}
	return deadline.Sign() < 0
}
func (s *boundedCacheStore) prune() {
	now := s.now().Unix()
	for len(s.expiry) > 0 && cacheDeadlinePassed(s.expiry[0].expires, now) {
		s.remove(s.expiry[0])
	}
}
func (s *boundedCacheStore) get(key string) runtime.Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	entry := s.entries[key]
	if entry == nil {
		return runtime.Null{}
	}
	s.recent.MoveToFront(entry.recent)
	return entry.payload
}
func (s *boundedCacheStore) set(key string, payload runtime.String, expires *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if cacheDeadlinePassed(expires, s.now().Unix()) {
		if old := s.entries[key]; old != nil {
			s.remove(old)
		}
		return
	}
	if entry := s.entries[key]; entry != nil {
		entry.payload = payload
		entry.expires = new(big.Int).Set(expires)
		heap.Fix(&s.expiry, entry.expiryIndex)
		s.recent.MoveToFront(entry.recent)
		return
	}
	if s.capacity.IsInt64() && int64(len(s.entries)) >= s.capacity.Int64() {
		s.remove(s.recent.Back().Value.(*cachePayloadEntry))
	}
	entry := &cachePayloadEntry{key: key, payload: payload, expires: new(big.Int).Set(expires)}
	entry.recent = s.recent.PushFront(entry)
	s.entries[key] = entry
	heap.Push(&s.expiry, entry)
}
func (s *boundedCacheStore) delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	entry := s.entries[key]
	if entry == nil {
		return false
	}
	s.remove(entry)
	return true
}
func boundedCacheHandle(value runtime.Value) (*boundedCacheStore, error) {
	if handle, ok := value.(runtime.NativeObject); ok && handle.Kind == "MemoryCache" {
		if cache, ok := handle.Payload.(*boundedCacheStore); ok {
			return cache, nil
		}
	}
	return nil, runtime.ClassifiedError{Class: "TypeError", Message: "invalid memory cache handle"}
}
func registerCacheStore(r *Registry) {
	r.Register("store", "_cacheNew", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("store._cacheNew expects capacity")
		}
		capacity, ok := IntValueToBigInt(args[0])
		if !ok || capacity.Sign() <= 0 {
			return nil, runtime.ClassifiedError{Class: "ValueError", Message: "cache capacity must be a positive integer"}
		}
		return runtime.NativeObject{Kind: "MemoryCache", ID: nextSyncID(), Payload: &boundedCacheStore{capacity: new(big.Int).Set(capacity), entries: map[string]*cachePayloadEntry{}, now: time.Now}}, nil
	})
	for _, method := range []string{"_cacheGet", "_cacheSet", "_cacheDelete"} {
		r.Register("store", method, func(args []runtime.Value) (runtime.Value, error) {
			expected := 2
			if method == "_cacheSet" {
				expected = 4
			}
			if len(args) != expected {
				return nil, fmt.Errorf("store.%s expects %d arguments", method, expected)
			}
			cache, err := boundedCacheHandle(args[0])
			if err != nil {
				return nil, err
			}
			key, ok := args[1].(runtime.String)
			if !ok {
				return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cache key must be a string"}
			}
			switch method {
			case "_cacheGet":
				return cache.get(key.Value), nil
			case "_cacheDelete":
				return runtime.Bool{Value: cache.delete(key.Value)}, nil
			default:
				payload, ok := args[2].(runtime.String)
				if !ok {
					return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cache payload must be a string"}
				}
				expires, ok := IntValueToBigInt(args[3])
				if !ok {
					return nil, runtime.ClassifiedError{Class: "TypeError", Message: "cache expiry must be an integer"}
				}
				cache.set(key.Value, payload, expires)
				return runtime.Null{}, nil
			}
		})
	}
}
