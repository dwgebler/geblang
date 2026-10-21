package native

import (
	"fmt"
	"geblang/internal/runtime"
	mrand "math/rand"
	"sync/atomic"
	"time"
)

// randomNextID numbers random.Generator handles. Each handle carries its
// *mrand.Rand as the payload, so the generator is freed with the handle.
var randomNextID atomic.Int64

// registerRandom registers a deterministic pseudo-random number generator
// module backed by Go's math/rand. Use this for sampling, shuffling,
// procedural generation, and any application where reproducibility matters
// (with a fixed seed). For cryptographic randomness - keys, tokens,
// salts - use the `secrets` module instead.
func registerRandom(r *Registry) {
	// A package-level RNG with a process-wide default seed lets the
	// module-level random.* helpers act like Python's `random` while
	// keeping seeded determinism available through random.seed().
	defaultRNG := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	rngFromArg := func(args []runtime.Value, name string, takesOneArg bool) (*mrand.Rand, []runtime.Value, error) {
		// If the first arg is a generator handle (NativeObject of kind
		// "Random") use it; otherwise fall back to the package default.
		if len(args) > 0 {
			if obj, ok := args[0].(runtime.NativeObject); ok && obj.Kind == "Random" {
				gen, ok := obj.Payload.(*mrand.Rand)
				if !ok {
					return nil, nil, fmt.Errorf("%s: unknown generator handle", name)
				}
				return gen, args[1:], nil
			}
		}
		_ = takesOneArg
		return defaultRNG, args, nil
	}
	r.Register("random", "seed", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("random.seed expects exactly one int seed")
		}
		seed, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("random.seed seed must be int")
		}
		defaultRNG.Seed(seed)
		return runtime.Null{}, nil
	})
	r.Register("random", "next", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.next", false)
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, fmt.Errorf("random.next expects only an optional generator")
		}
		return runtime.NewInt64(gen.Int63()), nil
	})
	r.Register("random", "intRange", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.intRange", false)
		if err != nil {
			return nil, err
		}
		if len(rest) != 2 {
			return nil, fmt.Errorf("random.intRange expects min and max")
		}
		min, ok := AsInt64(rest[0])
		if !ok {
			return nil, fmt.Errorf("random.intRange min must be int")
		}
		max, ok := AsInt64(rest[1])
		if !ok {
			return nil, fmt.Errorf("random.intRange max must be int")
		}
		if max <= min {
			return nil, fmt.Errorf("random.intRange max must be > min")
		}
		return runtime.NewInt64(min + gen.Int63n(max-min)), nil
	})
	r.Register("random", "float", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.float", false)
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, fmt.Errorf("random.float expects only an optional generator")
		}
		return runtime.Float{Value: gen.Float64()}, nil
	})
	r.Register("random", "bool", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.bool", false)
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, fmt.Errorf("random.bool expects only an optional generator")
		}
		return runtime.Bool{Value: gen.Intn(2) == 1}, nil
	})
	r.Register("random", "choice", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.choice", true)
		if err != nil {
			return nil, err
		}
		if len(rest) != 1 {
			return nil, fmt.Errorf("random.choice expects a non-empty list")
		}
		lst, ok := rest[0].(*runtime.List)
		if !ok || len(lst.Elements) == 0 {
			return nil, fmt.Errorf("random.choice expects a non-empty list")
		}
		return lst.Elements[gen.Intn(len(lst.Elements))], nil
	})
	r.Register("random", "shuffle", func(args []runtime.Value) (runtime.Value, error) {
		gen, rest, err := rngFromArg(args, "random.shuffle", true)
		if err != nil {
			return nil, err
		}
		if len(rest) != 1 {
			return nil, fmt.Errorf("random.shuffle expects a list")
		}
		lst, ok := rest[0].(*runtime.List)
		if !ok {
			return nil, fmt.Errorf("random.shuffle expects a list")
		}
		out := append([]runtime.Value(nil), lst.Elements...)
		gen.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return &runtime.List{Elements: out}, nil
	})
	r.Register("random", "Generator", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("random.Generator expects a single int seed")
		}
		seed, ok := AsInt64(args[0])
		if !ok {
			return nil, fmt.Errorf("random.Generator seed must be int")
		}
		gen := mrand.New(mrand.NewSource(seed))
		return runtime.NativeObject{Kind: "Random", ID: randomNextID.Add(1), Payload: gen}, nil
	})
}
