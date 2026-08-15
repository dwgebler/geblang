package native

import "testing"

// Guard: every bare builtin has a catalog entry, and vice versa.
func TestGlobalBuiltinsMatchBareBuiltins(t *testing.T) {
	// compile-time constructs cataloged for the IDE but not runtime-dispatched
	extras := map[string]struct{}{"embed": {}}
	bare := map[string]struct{}{}
	for _, name := range BareBuiltins {
		bare[name] = struct{}{}
		if _, ok := GlobalBuiltins[name]; !ok {
			t.Errorf("bare builtin %q has no GlobalBuiltins catalog entry", name)
		}
	}
	for name := range GlobalBuiltins {
		if _, ok := bare[name]; ok {
			continue
		}
		if _, ok := extras[name]; !ok {
			t.Errorf("GlobalBuiltins entry %q is not a bare builtin", name)
		}
	}
}
