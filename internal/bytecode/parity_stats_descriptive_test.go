package bytecode_test

import "testing"

func TestParityStatsDescriptiveSummary(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import stats;

let xs = [2, 4, 4, 4, 5, 5, 7, 9];
io.println(stats.variance(xs));
io.println(stats.variance(xs, {"population": true}));
io.println(stats.stdev(xs));
io.println(stats.stdev(xs, {"population": true}));
io.println(stats.variance({1, 3}));
io.println(stats.stdev([5], {"population": true}));
io.println(stats.geometricMean([1, 2, 4]));
io.println(stats.harmonicMean([1, 2, 4]));
io.println(stats.weightedMean([1, 2], [3, 1]));
io.println(stats.range([3, 9, 1]));
io.println(stats.iqr([1, 2, 3, 4, 5, 6, 7, 8]));
io.println(stats.mad([1, 1, 2, 2, 4, 6, 9]));
io.println(stats.zscores([1, 2, 3]));
io.println(stats.describe([1, 2, 3, 4]));
let one = stats.describe([7]);
io.println(one["count"]);
io.println(one["stdev"]);
`)
	want := "4.571428571428571\n4\n2.138089935299395\n2\n2\n0\n2\n1.7142857142857142\n1.25\n8\n3.5\n1\n" +
		"[-1, 0, 1]\n" +
		"{\"count\": 4, \"mean\": 2.5, \"stdev\": 1.2909944487358056, \"min\": 1, \"q1\": 1.75, \"median\": 2.5, \"q3\": 3.25, \"max\": 4}\n" +
		"1\nNaN\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}

func TestParityStatsDescriptiveSummaryErrors(t *testing.T) {
	ev, vm := parityOutputs(t, `import io;
import stats;

func report(func f): void {
    try {
        f();
    } catch (Error e) {
        io.println("${typeof(e)}: ${e.message}");
    }
}

report(func(): void { stats.variance([]); });
report(func(): void { stats.variance([5]); });
report(func(): void { stats.stdev([5]); });
report(func(): void { stats.variance([1, 2], {"population": 1}); });
report(func(): void { stats.geometricMean([1, 0]); });
report(func(): void { stats.harmonicMean([-1, 2]); });
report(func(): void { stats.weightedMean([1, 2], [1]); });
report(func(): void { stats.weightedMean([1, 2], [1, -1]); });
report(func(): void { stats.weightedMean([1, 2], [0, 0]); });
report(func(): void { stats.weightedMean({1, 2}, [1, 1]); });
report(func(): void { stats.zscores([4, 4]); });
report(func(): void { stats.zscores([4]); });
report(func(): void { stats.zscores({1, 2}); });
report(func(): void { stats.range([]); });
report(func(): void { stats.describe([]); });
list<any> strs = ["a"];
report(func(): void { stats.mad(strs); });
`)
	want := "RuntimeError: stats.variance: list must not be empty\n" +
		"RuntimeError: stats.variance: sample variance needs at least 2 values\n" +
		"RuntimeError: stats.stdev: sample variance needs at least 2 values\n" +
		"RuntimeError: stats.variance: population must be a bool\n" +
		"RuntimeError: stats.geometricMean: values must be positive\n" +
		"RuntimeError: stats.harmonicMean: values must be positive\n" +
		"RuntimeError: stats.weightedMean: xs and weights must have equal length\n" +
		"RuntimeError: stats.weightedMean: weights must not be negative\n" +
		"RuntimeError: stats.weightedMean: weights must not sum to zero\n" +
		"RuntimeError: stats.weightedMean: argument must be a list\n" +
		"RuntimeError: stats.zscores: zero variance\n" +
		"RuntimeError: stats.zscores: sample variance needs at least 2 values\n" +
		"RuntimeError: stats.zscores: argument must be a list\n" +
		"RuntimeError: stats.range: list must not be empty\n" +
		"RuntimeError: stats.describe: list must not be empty\n" +
		"RuntimeError: stats.mad: list element 0: expected numeric value, got string\n"
	if ev != vm || ev != want {
		t.Fatalf("evaluator %q\nvm        %q\nwant      %q", ev, vm, want)
	}
}
