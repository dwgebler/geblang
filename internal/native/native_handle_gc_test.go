package native

import (
	mrand "math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"weak"

	geb "geblang/internal/runtime"
)

// TestSecureRandomSessionIsCollected checks that a session handle owns its
// session: once the program drops the handle, the session (and its audit log)
// can be garbage collected. Sessions used to stay in a process-wide registry
// forever, so a server opening one per game round grew without bound.
func TestSecureRandomSessionIsCollected(t *testing.T) {
	var ref weak.Pointer[secureSession]
	func() {
		h, err := secureRandomFromSeed([]geb.Value{geb.String{Value: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"}})
		if err != nil {
			t.Fatal(err)
		}
		sess, err := lookupSecureSession(h, "test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := drawAndLog(sess, "test", nil, func(key *seedKey, nonce int64) (geb.Value, error) {
			return geb.SmallInt{Value: nonce}, nil
		}); err != nil {
			t.Fatal(err)
		}
		ref = weak.Make(sess)
	}()
	for i := 0; i < 5 && ref.Value() != nil; i++ {
		runtime.GC()
	}
	if ref.Value() != nil {
		t.Fatal("secureRandom session still reachable after its handle was dropped")
	}
}

// TestNativeHandlesAreCollected checks the same ownership for the other
// handle kinds that used to live in process-wide registries.
func TestNativeHandlesAreCollected(t *testing.T) {
	r := NewBuiltinRegistry()
	cases := []struct {
		module, fn string
		args       []geb.Value
	}{
		{"store", "new", nil},
		{"async.channel", "make", []geb.Value{geb.SmallInt{Value: 1}}},
		{"async.sync", "mutexNew", nil},
		{"async.atomic", "intNew", []geb.Value{geb.SmallInt{Value: 0}}},
		{"random", "Generator", []geb.Value{geb.SmallInt{Value: 7}}},
	}
	for _, c := range cases {
		t.Run(c.module+"."+c.fn, func(t *testing.T) {
			var alive func() bool
			func() {
				h, err := r.Call(c.module, c.fn, c.args)
				if err != nil {
					t.Fatal(err)
				}
				obj := h.(geb.NativeObject)
				switch p := obj.Payload.(type) {
				case *concurrentStore:
					alive = weakAlive(p)
				case *ChannelHandle:
					alive = weakAlive(p)
				case *sync.Mutex:
					alive = weakAlive(p)
				case *atomic.Int64:
					alive = weakAlive(p)
				case *mrand.Rand:
					alive = weakAlive(p)
				default:
					t.Fatalf("%s handle payload %T is not tracked by this test", obj.Kind, obj.Payload)
				}
			}()
			for i := 0; i < 5 && alive(); i++ {
				runtime.GC()
			}
			if alive() {
				t.Fatal("native value still reachable after its handle was dropped")
			}
		})
	}
}

func weakAlive[T any](p *T) func() bool {
	w := weak.Make(p)
	return func() bool { return w.Value() != nil }
}
