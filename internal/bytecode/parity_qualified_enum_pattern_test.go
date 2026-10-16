package bytecode_test

import "testing"

func TestParityModuleQualifiedEnumVariantPatterns(t *testing.T) {
	ev, vm := parityModuleOutputs(t, map[string]string{
		"quotes.gb": "module quotes;\nexport enum Quote { Offered(string, int), Declined(string), Pending }\n",
	}, `import io;
import quotes as q;
func describe(q.Quote quote): string {
    return match (quote) {
        case q.Quote.Offered(string carrier, int pence) => "${carrier} ${pence}";
        case q.Quote.Declined(string reason) => "declined: ${reason}";
        default => "pending";
    };
}
io.println(describe(q.Quote.Offered("dhl", 499)));
io.println(describe(q.Quote.Declined("too heavy")));
io.println(describe(q.Quote.Pending));
let offer = q.Quote.Offered("ups", 1);
io.println("${offer instanceof q.Quote.Offered} ${offer instanceof q.Quote.Declined} ${offer instanceof q.Quote}");
`)
	want := "dhl 499\ndeclined: too heavy\npending\ntrue false true\n"
	if ev != want || vm != want {
		t.Errorf("qualified enum patterns:\n  evaluator: %q\n  vm:        %q\n  want:      %q", ev, vm, want)
	}
}
