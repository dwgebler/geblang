package runtime_test

import (
	"strconv"
	"sync"
	"testing"

	"geblang/internal/ast"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

func collectionType(t testing.TB, name string) *ast.TypeRef {
	t.Helper()
	ref, err := parser.ParseTypeRef(name)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func matchesLeaf(value runtime.Value, ref *ast.TypeRef) bool {
	return ref.Name == "any" || value.TypeName() == ref.Name
}

func TestCollectionType_NestedAliasesKeepWriteConstraints(t *testing.T) {
	inner := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	outer := &runtime.List{Elements: []runtime.Value{inner}}
	declared := collectionType(t, "list<list<int>>")

	matched, handled := runtime.CheckCollectionType(outer, declared, matchesLeaf)

	if !handled || !matched {
		t.Fatal("valid nested collection rejected")
	}
	if runtime.CollectionWriteMatches(inner, 0, runtime.String{Value: "wrong"}) {
		t.Fatal("nested alias accepted an incompatible write")
	}
	if !runtime.CollectionWriteMatches(inner, 0, runtime.SmallInt{Value: 2}) {
		t.Fatal("nested alias rejected a compatible write")
	}
}

func TestCollectionType_FailedValidationLeavesNestedValuesUntyped(t *testing.T) {
	first := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	wrong := &runtime.List{Elements: []runtime.Value{runtime.String{Value: "wrong"}}}
	outer := &runtime.List{Elements: []runtime.Value{first, wrong}}

	matched, _ := runtime.CheckCollectionType(outer, collectionType(t, "list<list<int>>"), matchesLeaf)

	if matched || runtime.HasCollectionType(first) || runtime.HasCollectionType(outer) {
		t.Fatal("failed validation published collection constraints")
	}
}

func TestCollectionType_CompatibilityProbeDoesNotBind(t *testing.T) {
	value := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}

	matched, _ := runtime.MatchCollectionType(value, collectionType(t, "list<int>"), matchesLeaf)

	if !matched || runtime.HasCollectionType(value) {
		t.Fatal("compatibility probe changed collection type")
	}
}

func TestCollectionType_DictionaryAliasesShareConstraints(t *testing.T) {
	for _, value := range []runtime.Dict{runtime.NewDict(), runtime.DictFromEntries(map[string]runtime.DictEntry{}, &[]string{})} {
		alias := value.WithElementTypes([]string{"string", "any"})

		matched, _ := runtime.CheckCollectionType(value, collectionType(t, "dict<string,int>"), matchesLeaf)

		if !matched || runtime.CollectionWriteMatches(alias, 1, runtime.String{Value: "wrong"}) {
			t.Fatal("dictionary alias bypassed backing constraints")
		}
	}
}

func TestCollectionType_CovariantPassingKeepsBackingType(t *testing.T) {
	dogs := collectionType(t, "list<Dog>")
	dogs.Arguments[0].Supertypes = []*ast.TypeRef{collectionType(t, "Animal")}
	value := &runtime.List{}
	runtime.CheckCollectionType(value, dogs, matchesLeaf)

	matched, _ := runtime.MatchCollectionType(value, collectionType(t, "list<Animal>"), matchesLeaf)
	wrong, _ := runtime.MatchCollectionType(value, collectionType(t, "list<string>"), matchesLeaf)

	if !matched || wrong || runtime.CollectionDeclaredType(value).Arguments[0].Name != "Dog" {
		t.Fatal("covariant passing changed the backing type")
	}
}

func TestCollectionType_DynamicNarrowingKeepsMixedWrites(t *testing.T) {
	value := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	runtime.CheckCollectionType(value, collectionType(t, "list<any>"), matchesLeaf)

	matched, _ := runtime.CheckCollectionType(value, collectionType(t, "list<int>"), matchesLeaf)
	allowsString := runtime.CollectionWriteMatches(value, 0, runtime.String{Value: "mixed"})
	value.Elements = append(value.Elements, runtime.String{Value: "mixed"})
	again, _ := runtime.MatchCollectionType(value, collectionType(t, "list<int>"), matchesLeaf)

	if !matched || !allowsString || again {
		t.Fatal("dynamic narrowing changed mixed-type behavior")
	}
}

func TestCollectionType_ProtectedNestedWriteRejectsDynamicChild(t *testing.T) {
	outer := &runtime.List{}
	child := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	runtime.CheckCollectionType(outer, collectionType(t, "list<list<int>>"), matchesLeaf)
	runtime.CheckCollectionType(child, collectionType(t, "list<any>"), matchesLeaf)

	accepted := runtime.CollectionWriteMatches(outer, 0, child)

	if accepted || !runtime.CollectionWriteMatches(child, 0, runtime.String{Value: "mixed"}) {
		t.Fatal("nested write accepted an unprotected dynamic child")
	}
}

func TestCollectionType_DynamicDescendantPreventsOuterGuarantee(t *testing.T) {
	child := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	runtime.CheckCollectionType(child, collectionType(t, "list<any>"), matchesLeaf)
	outer := &runtime.List{Elements: []runtime.Value{child}}

	matched, _ := runtime.CheckCollectionType(outer, collectionType(t, "list<list<int>>"), matchesLeaf)
	child.Elements = append(child.Elements, runtime.String{Value: "mixed"})
	again, _ := runtime.MatchCollectionType(outer, collectionType(t, "list<list<int>>"), matchesLeaf)

	if !matched || runtime.HasCollectionType(outer) || again {
		t.Fatal("outer guarantee hid a dynamic descendant mutation")
	}
}

func TestCollectionType_FrozenAliasesCanValidateConcurrently(t *testing.T) {
	values := []struct {
		value runtime.Value
		typ   string
	}{
		{runtime.ShallowFreeze(&runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}), "list<int>"},
		{runtime.ShallowFreeze(runtime.NewDict()), "dict<string,int>"},
		{runtime.ShallowFreeze(runtime.NewSet(map[string]runtime.SetEntry{})), "set<int>"},
	}
	for _, item := range values {
		t.Run(item.typ, func(t *testing.T) {
			ref := collectionType(t, item.typ)
			var workers sync.WaitGroup
			results := make(chan bool, 16)

			for range 16 {
				workers.Go(func() {
					matched, _ := runtime.CheckCollectionType(item.value, ref, matchesLeaf)
					results <- matched
				})
			}
			workers.Wait()
			close(results)

			for matched := range results {
				if !matched {
					t.Fatal("concurrent validation rejected a compatible frozen alias")
				}
			}
		})
	}
}

func BenchmarkCollectionType_RepeatedTypedBoundary(b *testing.B) {
	for _, size := range []int{1, 1000, 1000000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			values := make([]runtime.Value, size)
			for i := range values {
				values[i] = runtime.SmallInt{Value: int64(i)}
			}
			value := &runtime.List{Elements: values}
			ref := collectionType(b, "list<int>")
			runtime.CheckCollectionType(value, ref, matchesLeaf)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if matched, _ := runtime.MatchCollectionType(value, ref, matchesLeaf); !matched {
					b.Fatal("typed collection rejected")
				}
			}
		})
	}
}

func BenchmarkCollectionType_RepeatedArrayAliasBoundary(b *testing.B) {
	value := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
	ref := collectionType(b, "int[]")
	runtime.CheckCollectionType(value, ref, matchesLeaf)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if matched, _ := runtime.MatchCollectionType(value, ref, matchesLeaf); !matched {
			b.Fatal("array alias rejected")
		}
	}
}

func BenchmarkCollectionType_FirstValidation(b *testing.B) {
	for _, size := range []int{1, 1000, 100000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			elements := make([]runtime.Value, size)
			for i := range elements {
				elements[i] = runtime.SmallInt{Value: int64(i)}
			}
			ref := collectionType(b, "list<int>")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				value := &runtime.List{Elements: elements}
				if matched, _ := runtime.CheckCollectionType(value, ref, matchesLeaf); !matched {
					b.Fatal("valid collection rejected")
				}
			}
		})
	}
}

func BenchmarkCollectionType_SharedNestedChildren(b *testing.B) {
	for _, size := range []int{1, 1000, 100000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			ref := collectionType(b, "list<list<int>>")
			elements := make([]runtime.Value, size)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				child := &runtime.List{Elements: []runtime.Value{runtime.SmallInt{Value: 1}}}
				for i := range elements {
					elements[i] = child
				}
				value := &runtime.List{Elements: elements}
				if matched, _ := runtime.CheckCollectionType(value, ref, matchesLeaf); !matched {
					b.Fatal("valid nested collection rejected")
				}
			}
		})
	}
}

func BenchmarkCollectionType_CheckedWrite(b *testing.B) {
	value := &runtime.List{}
	runtime.CheckCollectionType(value, collectionType(b, "list<int>"), matchesLeaf)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !runtime.CollectionWriteMatches(value, 0, runtime.SmallInt{Value: 1}) {
			b.Fatal("valid write rejected")
		}
	}
}
