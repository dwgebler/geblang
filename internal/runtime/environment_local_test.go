package runtime

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"geblang/internal/ast"
)

func TestLocalEnvironmentPreservesLexicalOperations(t *testing.T) {
	outer := NewLocalEnvironment()
	inner := NewLocalEnclosedEnvironment(outer)
	for i := 0; i < 8; i++ {
		if err := outer.Define(fmt.Sprint(i), SmallInt{Value: int64(i)}, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := inner.Define("fixed", SmallInt{Value: 9}, true); err != nil {
		t.Fatal(err)
	}
	outer.DefineTypeAlias("Number", &ast.TypeRef{Name: "int"})
	outer.DefineTypeBinding("T", "string")

	if err := inner.Assign("7", SmallInt{Value: 17}); err != nil {
		t.Fatal(err)
	}
	value, found := outer.Get("7")
	alias, aliasFound := inner.GetTypeAlias("NUMBER")
	binding, bound := inner.GetTypeBinding("T")

	if !found || value.Inspect() != "17" || !aliasFound || alias.Name != "int" || !bound || binding != "string" {
		t.Fatal("local lookup lost its enclosing bindings")
	}
	if err := inner.Assign("fixed", Null{}); err == nil {
		t.Fatal("constant became assignable")
	}
	if !inner.Delete("0") || inner.Delete("absent") {
		t.Fatal("lexical deletion changed")
	}
	if !reflect.DeepEqual(inner.VisibleNames(), []string{"1", "2", "3", "4", "5", "6", "7", "fixed"}) || !reflect.DeepEqual(inner.TypeBindingNames(), []string{"T"}) {
		t.Fatal("local enumeration lost names")
	}
}

func TestEnvironmentPublicationProtectsConcurrentBindings(t *testing.T) {
	factories := map[string]func() (*Environment, *Environment){
		"default": func() (*Environment, *Environment) {
			outer := NewEnvironment()
			return outer, NewEnclosedEnvironment(outer)
		},
		"zero": func() (*Environment, *Environment) {
			outer := &Environment{}
			return outer, NewEnclosedEnvironment(outer)
		},
		"captured local chain": func() (*Environment, *Environment) {
			outer := NewLocalEnvironment()
			inner := NewLocalEnclosedEnvironment(outer)
			inner.MarkShared()
			return outer, inner
		},
		"shared child of local parent": func() (*Environment, *Environment) {
			outer := NewLocalEnvironment()
			return outer, NewEnclosedEnvironment(outer)
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			outer, inner := factory()
			if err := outer.Define("value", SmallInt{Value: 0}, false); err != nil {
				t.Fatal(err)
			}
			var workers sync.WaitGroup
			failures := make(chan error, 8)
			start := make(chan struct{})

			for worker := 0; worker < 8; worker++ {
				workers.Go(func() {
					<-start
					for n := 0; n < 200; n++ {
						if err := inner.Assign("value", SmallInt{Value: int64(n)}); err != nil {
							failures <- err
							return
						}
						outer.DefineTypeAlias("Number", &ast.TypeRef{Name: "int"})
						outer.DefineTypeBinding("T", "int")
						value, ok := inner.Get("value")
						alias, aliasOK := inner.GetTypeAlias("Number")
						bound, boundOK := inner.GetTypeBinding("T")
						if !ok || value.TypeName() != "int" || !aliasOK || alias.Name != "int" || !boundOK || bound != "int" {
							failures <- fmt.Errorf("published scope lost bindings")
							return
						}
						inner.VisibleNames()
						outer.ForEachBinding(func(_ string, _ Binding) {})
					}
				})
			}
			close(start)
			workers.Wait()
			close(failures)

			for failure := range failures {
				t.Error(failure)
			}
		})
	}
}
