package native

import (
	"fmt"
	"math"

	"geblang/internal/runtime"
)

// hnsw module: native vector index; backend chosen per-platform by newAnnIndex.

type annResult struct {
	key string
	vec []float32
}

type annIndex interface {
	add(id string, vec []float32) error
	lookup(id string) ([]float32, bool)
	remove(id string) (bool, error)
	count() int
	clear() error
	search(query []float32, k int) ([]annResult, error)
	metricName() string
}

func registerHnsw(r *Registry) {
	r.Register("hnsw", "new", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("hnsw.new expects (metric, m, efSearch)")
		}
		metric, ok := args[0].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("hnsw.new: metric must be a string")
		}
		m, ok := vecToInt(args[1])
		if !ok {
			return nil, fmt.Errorf("hnsw.new: m must be an integer")
		}
		ef, ok := vecToInt(args[2])
		if !ok {
			return nil, fmt.Errorf("hnsw.new: efSearch must be an integer")
		}
		idx, err := newAnnIndex(metric.Value, m, ef)
		if err != nil {
			return nil, err
		}
		return runtime.NativeObject{Kind: "HnswIndex", ID: nextSyncID(), Payload: idx}, nil
	})

	r.Register("hnsw", "add", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("hnsw.add expects (handle, id, vector)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.add")
		if err != nil {
			return nil, err
		}
		id, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("hnsw.add: id must be a string")
		}
		vec, err := vecToFloat32(args[2])
		if err != nil {
			return nil, err
		}
		if err := idx.add(id.Value, vec); err != nil {
			return nil, err
		}
		return runtime.Null{}, nil
	})

	r.Register("hnsw", "get", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("hnsw.get expects (handle, id)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.get")
		if err != nil {
			return nil, err
		}
		id, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("hnsw.get: id must be a string")
		}
		vec, found := idx.lookup(id.Value)
		if !found {
			return runtime.Null{}, nil
		}
		return float32sToList(vec), nil
	})

	r.Register("hnsw", "delete", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("hnsw.delete expects (handle, id)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.delete")
		if err != nil {
			return nil, err
		}
		id, ok := args[1].(runtime.String)
		if !ok {
			return nil, fmt.Errorf("hnsw.delete: id must be a string")
		}
		removed, err := idx.remove(id.Value)
		if err != nil {
			return nil, err
		}
		return runtime.Bool{Value: removed}, nil
	})

	r.Register("hnsw", "count", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("hnsw.count expects (handle)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.count")
		if err != nil {
			return nil, err
		}
		return runtime.NewInt64(int64(idx.count())), nil
	})

	r.Register("hnsw", "clear", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("hnsw.clear expects (handle)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.clear")
		if err != nil {
			return nil, err
		}
		if err := idx.clear(); err != nil {
			return nil, err
		}
		return runtime.Null{}, nil
	})

	r.Register("hnsw", "search", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("hnsw.search expects (handle, query, k)")
		}
		idx, err := hnswFromHandle(args[0], "hnsw.search")
		if err != nil {
			return nil, err
		}
		query, err := vecToFloat32(args[1])
		if err != nil {
			return nil, err
		}
		k, ok := vecToInt(args[2])
		if !ok {
			return nil, fmt.Errorf("hnsw.search: k must be an integer")
		}
		if k < 0 {
			k = 0
		}
		results, err := idx.search(query, k)
		if err != nil {
			return nil, err
		}
		qNorm := vecNormF32(query)
		out := make([]runtime.Value, 0, len(results))
		for _, res := range results {
			score, err := vecScore(idx.metricName(), query, res.vec, qNorm)
			if err != nil {
				return nil, err
			}
			d := runtime.NewDictHint(3)
			idKey := runtime.String{Value: "id"}
			scoreKey := runtime.String{Value: "score"}
			vecKey := runtime.String{Value: "vector"}
			d.PutEntry(DictKey(idKey), runtime.DictEntry{Key: idKey, Value: runtime.String{Value: res.key}})
			d.PutEntry(DictKey(scoreKey), runtime.DictEntry{Key: scoreKey, Value: runtime.Float{Value: score}})
			d.PutEntry(DictKey(vecKey), runtime.DictEntry{Key: vecKey, Value: float32sToList(res.vec)})
			out = append(out, d)
		}
		return &runtime.List{Elements: out}, nil
	})
}

func hnswFromHandle(v runtime.Value, fn string) (annIndex, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "HnswIndex" {
		return nil, fmt.Errorf("%s expects an HnswIndex handle", fn)
	}
	idx, ok := obj.Payload.(annIndex)
	if !ok || idx == nil {
		return nil, fmt.Errorf("%s: unknown HnswIndex handle", fn)
	}
	return idx, nil
}

func float32sToList(vec []float32) runtime.Value {
	out := make([]runtime.Value, len(vec))
	for i, f := range vec {
		out[i] = runtime.Float{Value: float64(f)}
	}
	return &runtime.List{Elements: out}
}

func vecNormF32(v []float32) float64 {
	return math.Sqrt(dotF32(v, v))
}
