package evaluator_test

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"geblang/internal/evaluator"
	"geblang/internal/lexer"
	"geblang/internal/parser"
	"geblang/internal/runtime"
)

func TestCapturedLocalScopeSupportsConcurrentNativeAccess(t *testing.T) {
	for name, body := range map[string]string{
		"literal": `return func(): int { return value; };`,
		"named":   `func read(): int { return value; } return read;`,
	} {
		t.Run(name, func(t *testing.T) {
			session, err := evaluator.NewSession(io.Discard, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := `func make(): callable { int value = 0; ` + body + ` } make();`
			p := parser.New(lexer.New(source))
			program := p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatal(p.Errors())
			}
			result, err := session.Eval(program)
			if err != nil {
				t.Fatal(err)
			}
			function, ok := result.Value.(runtime.Function)
			if !ok {
				t.Fatalf("returned %T, want callable", result.Value)
			}
			var workers sync.WaitGroup
			failures := make(chan error, 4)
			start := make(chan struct{})

			for worker := 0; worker < 4; worker++ {
				workers.Go(func() {
					<-start
					for n := 0; n < 500; n++ {
						if err := function.Env.Assign("value", runtime.SmallInt{Value: int64(n)}); err != nil {
							failures <- err
							return
						}
						if value, found := function.Env.Get("value"); !found || value.TypeName() != "int" {
							failures <- fmt.Errorf("captured binding disappeared")
							return
						}
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
