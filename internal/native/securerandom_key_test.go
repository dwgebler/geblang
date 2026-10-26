package native

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"

	"geblang/internal/runtime"
)

// reference is HMAC-SHA-256(seed, message) chunks as deriveBytes defines
// them, keyed afresh for every chunk with no shared state.
func reference(seed []byte, message string, n int) []byte {
	var out []byte
	for chunk := 0; len(out) < n; chunk++ {
		msg := message
		if chunk > 0 {
			msg = message + ":chunk:" + strconv.Itoa(chunk)
		}
		mac := hmac.New(sha256.New, seed)
		mac.Write([]byte(msg))
		out = append(out, mac.Sum(nil)...)
	}
	return out[:n]
}

// A session's reused, reset HMAC derives exactly the bytes a fresh HMAC
// does, for every message and length, in any order.
func TestSessionKeyMatchesFreshHMAC(t *testing.T) {
	for s := 0; s < 20; s++ {
		seed := sha256.Sum256([]byte(fmt.Sprintf("seed-%d", s)))
		reused := &seedKey{seed: seed[:], mac: hmac.New(sha256.New, seed[:])}
		fresh := &seedKey{seed: seed[:]}
		for i, n := range []int{1, 8, 31, 32, 33, 64, 100, 8, 1} {
			msg := fmt.Sprintf(`{"clientSeed":"c%d","nonce":%d,"method":"randomInt","args":[0,%d]}`, s, i, n)
			want := reference(seed[:], msg, n)
			if got := deriveBytes(reused, msg, n); !bytes.Equal(got, want) {
				t.Fatalf("seed %d message %d: reused key %x, want %x", s, i, got, want)
			}
			if got := deriveBytes(fresh, msg, n); !bytes.Equal(got, want) {
				t.Fatalf("seed %d message %d: fresh key %x, want %x", s, i, got, want)
			}
		}
	}
}

// Every draw a session makes replays to the same output through the
// independent replay path, and draws stop at reveal.
func TestSessionDrawsReplayIndependently(t *testing.T) {
	seedHex := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	h, err := secureRandomFromSeed([]runtime.Value{runtime.String{Value: seedHex}, runtime.String{Value: "client"}})
	if err != nil {
		t.Fatal(err)
	}
	list := &runtime.List{Elements: []runtime.Value{runtime.String{Value: "a"}, runtime.String{Value: "b"}, runtime.String{Value: "c"}}}
	draws := []struct {
		method string
		call   func() (runtime.Value, error)
		args   []runtime.Value
	}{
		{"randomInt", func() (runtime.Value, error) {
			return secureRandomRandomInt([]runtime.Value{h, runtime.NewInt64(1), runtime.NewInt64(1000)})
		}, []runtime.Value{runtime.NewInt64(1), runtime.NewInt64(1000)}},
		{"float", func() (runtime.Value, error) { return secureRandomFloat([]runtime.Value{h}) }, nil},
		{"bool", func() (runtime.Value, error) { return secureRandomBool([]runtime.Value{h}) }, nil},
		{"choice", func() (runtime.Value, error) { return secureRandomChoice([]runtime.Value{h, list}) }, []runtime.Value{list}},
		{"randomInt", func() (runtime.Value, error) {
			return secureRandomRandomInt([]runtime.Value{h, runtime.NewInt64(0), runtime.NewInt64(5)})
		}, []runtime.Value{runtime.NewInt64(0), runtime.NewInt64(5)}},
	}
	for nonce, d := range draws {
		got, err := d.call()
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := secureRandomReplay([]runtime.Value{
			runtime.String{Value: seedHex}, runtime.String{Value: "client"}, runtime.NewInt64(int64(nonce)),
			runtime.String{Value: d.method}, &runtime.List{Elements: d.args},
		})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(replayed) {
			t.Fatalf("draw %d (%s): session %v, replay %v", nonce, d.method, got, replayed)
		}
	}
	if _, err := secureRandomReveal([]runtime.Value{h}); err != nil {
		t.Fatal(err)
	}
	if _, err := secureRandomFloat([]runtime.Value{h}); err == nil {
		t.Fatal("a draw after reveal succeeded")
	}
}
