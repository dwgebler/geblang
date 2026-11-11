package bytecode_test

import (
	"testing"
)

const paramSpellingDonor = `module spelling_donor;

export type Scores = dict<string, int>;

export class Base {
    func take(int[] xs): int { return 1; }
    func scores(Scores s): int { return 1; }
}

export func takeScores(Scores s): int { return 1; }
`

const paramSpellingMain = `import io;
import reflect;
import spelling_donor as donor;
from spelling_donor import Scores;

type Local = dict<string, int>;

func arr(int[] xs): int { return 1; }
func aliased(Local s): int { return 1; }
func nullable(?list<int> xs): int { return 1; }
func union(list<int> | string x): int { return 1; }
func imported(Scores s, donor.Scores t): int { return 1; }
class K { func K(dict<string,int> d) {} }
class Child extends donor.Base { func own(dict<string,int> d): int { return 1; } }

func report(callable f): void {
    try { f(); } catch (TypeError e) { io.println(e.message); }
}

any badList = ["x"];
any badDict = {"a": "x"};
report(func(): void { arr(badList); });
report(func(): void { aliased(badDict); });
report(func(): void { nullable(badList); });
report(func(): void { union(badList); });
report(func(): void { imported(badDict, {}); });
report(func(): void { imported({}, badDict); });
report(func(): void { donor.takeScores(badDict); });
report(func(): void { K(badDict); });
report(func(): void { Child().take(badList); });
report(func(): void { Child().scores(badDict); });
report(func(): void { Child().own(badDict); });
let lambda = func(Local d, int[] xs): int { return 1; };
report(func(): void { lambda(badDict, []); });
report(func(): void { lambda({}, badList); });
for (p in reflect.parameters(aliased)) { io.println(p["type"]); }
for (p in reflect.parameters(imported)) { io.println(p["type"]); }
for (p in reflect.parameters(arr)) { io.println(p["type"]); }
for (p in reflect.parameters(lambda)) { io.println(p["type"]); }
`

const paramSpellingWant = `arr expects int[] for parameter 'xs', got list (element at index 0 is string)
aliased expects dict<string, int> for parameter 's', got dict (value for key a is string)
nullable expects ?list<int> for parameter 'xs', got list (element at index 0 is string)
union expects list<int> | string for parameter 'x', got list<string>
imported expects dict<string, int> for parameter 's', got dict (value for key a is string)
imported expects dict<string, int> for parameter 't', got dict (value for key a is string)
takeScores expects dict<string, int> for parameter 's', got dict (value for key a is string)
K expects dict<string, int> for parameter 'd', got dict (value for key a is string)
Base.take expects int[] for parameter 'xs', got list (element at index 0 is string)
Base.scores expects dict<string, int> for parameter 's', got dict (value for key a is string)
Child.own expects dict<string, int> for parameter 'd', got dict (value for key a is string)
<closure> expects dict<string, int> for parameter 'd', got dict (value for key a is string)
<closure> expects int[] for parameter 'xs', got list (element at index 0 is string)
dict<string, int>
dict<string, int>
dict<string, int>
int[]
dict<string, int>
int[]
`

func TestParityParameterTypeSpelling(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{"spelling_donor.gb": paramSpellingDonor}, paramSpellingMain)
	if ev != vm {
		t.Fatalf("vm diverged from evaluator:\nvm:\n%s\nevaluator:\n%s", vm, ev)
	}
	if ev != paramSpellingWant {
		t.Fatalf("unexpected output:\n%s", ev)
	}
}
