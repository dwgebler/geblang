package lsp

import (
	"strings"
	"testing"
)

const localDefinitionSource = `import io;

let total = 10;

class Counter {
    int value;

    func Counter(int start) {
        this.value = start;
    }

    func bump(int step): int {
        let next = this.value + step;
        this.value = next;
        return this.reset(next);
    }

    func reset(int value): int {
        return value;
    }
}

func compute(int limit, list<int> items): int {
    let sum = 0;
    int typed = 2;
    for (item in items) {
        let sum = item * typed;
        io.println(sum);
    }
    for (let int i = 0; i < limit; i++) {
        sum += i;
    }
    for (key, val in {"a": 1}) {
        io.println(key + val);
    }
    let squares = [n * n for n in items];
    let doubler = func(int n): int {
        let local = n * 2;
        return local + total;
    };
    try {
        doubler(sum);
    } catch (RuntimeError err) {
        io.println(err.message);
    }
    with (handle = io.open("x")) {
        io.println(handle);
    }
    let [first, second] = items;
    let kind = match (first) {
        case int matched if (matched > 0) => matched + second;
        default => 0;
    };
    return sum + squares.length() + kind;
}

io.println(total);
`

// position finds the nth (1-based) whole-word occurrence of word.
func position(t *testing.T, source, word string, nth int) Position {
	t.Helper()
	seen := 0
	for lineIdx, line := range strings.Split(source, "\n") {
		start := 0
		for {
			idx := strings.Index(line[start:], word)
			if idx < 0 {
				break
			}
			col := start + idx
			if isWordBoundary(line, col, len(word)) {
				seen++
				if seen == nth {
					return Position{Line: lineIdx, Character: col}
				}
			}
			start = col + len(word)
		}
	}
	t.Fatalf("occurrence %d of %q not found", nth, word)
	return Position{}
}

func TestDefinitionResolvesLocalBindings(t *testing.T) {
	cases := []struct {
		label    string
		word     string
		use, def int
	}{
		{"top-level variable from a later statement", "total", 3, 1},
		{"top-level variable from inside a lambda", "total", 2, 1},
		{"constructor parameter", "start", 2, 1},
		{"method parameter", "step", 2, 1},
		{"local in a method", "next", 2, 1},
		{"local passed as an argument", "next", 3, 1},
		{"field through this", "value", 2, 1},
		{"field through this in another method", "value", 3, 1},
		{"parameter shadowing a field", "value", 6, 5},
		{"method through this", "reset", 1, 2},
		{"function parameter", "limit", 2, 1},
		{"typed local declaration", "typed", 2, 1},
		{"for-in variable", "item", 2, 1},
		{"shadowing local inside a loop body", "sum", 3, 2},
		{"outer local after the shadowing block", "sum", 4, 1},
		{"c-style loop variable", "i", 2, 1},
		{"c-style loop variable in the body", "i", 4, 1},
		{"two-name loop key", "key", 2, 1},
		{"two-name loop value", "val", 2, 1},
		{"comprehension variable used before its clause", "n", 1, 3},
		{"lambda parameter", "n", 5, 4},
		{"local in a lambda", "local", 2, 1},
		{"lambda bound to a local", "doubler", 2, 1},
		{"catch variable", "err", 2, 1},
		{"with binding", "handle", 2, 1},
		{"destructured name", "first", 2, 1},
		{"destructured name in a match arm", "second", 2, 1},
		{"match binding in its guard", "matched", 2, 1},
		{"match binding in its arm", "matched", 3, 1},
		{"local at the end of the function", "squares", 2, 1},
		{"match result local", "kind", 2, 1},
	}
	s, _ := newTestServer()
	uri := "file:///tmp/local.gb"
	s.docs[uri] = localDefinitionSource
	for _, tc := range cases {
		use := position(t, localDefinitionSource, tc.word, tc.use)
		want := position(t, localDefinitionSource, tc.word, tc.def)
		for _, offset := range []int{0, len(tc.word)} {
			at := Position{Line: use.Line, Character: use.Character + offset}
			result := s.definition(TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: at})
			loc, ok := result.(Location)
			if !ok {
				t.Fatalf("%s: expected Location at %+v, got %T", tc.label, at, result)
			}
			if loc.URI != uri || loc.Range.Start != want {
				t.Fatalf("%s: got %+v want start %+v", tc.label, loc.Range, want)
			}
			if loc.Range.End.Character != want.Character+len(tc.word) {
				t.Fatalf("%s: range end %+v does not cover the name", tc.label, loc.Range.End)
			}
		}
	}
}

func TestDefinitionOfLocalIgnoresOutOfScopeBindings(t *testing.T) {
	source := "func a(): int {\n    let inner = 1;\n    return inner;\n}\n\nfunc b(): int {\n    return inner;\n}\n"
	s, _ := newTestServer()
	uri := "file:///tmp/scope.gb"
	s.docs[uri] = source
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     position(t, source, "inner", 3),
	})
	if result != nil {
		t.Fatalf("expected no definition for an out-of-scope name, got %+v", result)
	}
}

func TestDefinitionOfLocalSurvivesIncompleteSource(t *testing.T) {
	source := "func a(int count): int {\n    let doubled = count * 2;\n    return doubled +\n"
	s, _ := newTestServer()
	uri := "file:///tmp/partial.gb"
	s.docs[uri] = source
	result := s.definition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     position(t, source, "doubled", 2),
	})
	loc, ok := result.(Location)
	if !ok || loc.Range.Start != position(t, source, "doubled", 1) {
		t.Fatalf("expected the local declaration, got %+v", result)
	}
}
