package bytecode_test

// Parity tests run each snippet through both the evaluator and the VM and
// assert that the printed output matches.  They pin the intersection of
// features that both execution paths are expected to support identically.

import (
	"testing"
)

// stdlibModuleLoader is a minimal bytecode-side module loader used by
// parity tests for source-distributed stdlib modules (time.scheduler,
// async.rate, etc.). It resolves canonical module paths via the standard
// resolver (which walks ancestors to find stdlib/), compiles each
// imported source module to bytecode on demand, and caches the result
// inside the loader struct.

func TestParityArithmetic(t *testing.T) {
	runParity(t, `import io;
io.println(2 + 3);
io.println(10 - 4);
io.println(3 * 7);
io.println(15 // 4);
io.println(17 % 5);
io.println(2 ** 8);
io.println(-5 + 2);
`, "5\n6\n21\n3\n2\n256\n-3\n")
}

func TestParityDecimalArithmetic(t *testing.T) {
	// Decimal values print with 10 decimal places via FloatString(10).
	runParity(t, `import io;
decimal d = 1.5;
let n = 5;
io.println(d + 0.5);
io.println(d * 2.0);
io.println(d.format(0));
io.println(d.format(3));
io.println(d.toString(1));
io.println(n.toDecimal().format(2));
io.println("42".toInt() + 1);
io.println("2.5".toFloat());
io.println("true".toBool());
`, "2.0000000000\n3.0000000000\n2\n1.500\n1.5\n5.00\n43\n2.5\ntrue\n")
}

func TestParityNumericMethods(t *testing.T) {
	// Value-keeping rounding (returns same type), toDecimal(places),
	// and the sign/clamp/isEven/isOdd helpers across both backends.
	runParity(t, `import io;
io.println((2.567).round(2));
io.println((2.5).round());
io.println((-2.5).round());
io.println((2.9).floor());
io.println((2.1).ceil());
io.println((2.99).truncate(1));
io.println((3.14159f).round(2));
io.println((2.9f).floor());
io.println((7).toDecimal(2));
io.println((3.14159f).toDecimal(3));
io.println("12.3456".toDecimal(2));
io.println((-7).sign());
io.println((0).sign());
io.println((4.2).sign());
io.println((12).clamp(0, 10));
io.println((-3).clamp(0, 10));
io.println((5).clamp(0, 10));
io.println((19.99).clamp(0, 5));
io.println((4).isEven());
io.println((7).isOdd());
io.println((-4).isEven());
`, "2.5700000000\n3.0000000000\n-3.0000000000\n2.0000000000\n3.0000000000\n2.9000000000\n3.14\n2\n7.0000000000\n3.1420000000\n12.3500000000\n-1\n0\n1\n10\n0\n5\n5.0000000000\ntrue\ntrue\ntrue\n")
}

func TestParityFloatArithmetic(t *testing.T) {
	runParity(t, `import io;
float f = 2.5f;
io.println(f + 1.5f);
io.println(f * 2.0f);
io.println(f + 1);
`, "4\n5\n3.5\n")
}

func TestParityNumericLessEqualGreaterEqual(t *testing.T) {
	runParity(t, `import io;
io.println(3 <= 5);
io.println(5 <= 5);
io.println(6 <= 5);
io.println(5 >= 3);
io.println(5 >= 5);
io.println(3 >= 5);
`, "true\ntrue\nfalse\ntrue\ntrue\nfalse\n")
}

func TestParityTypeEquality(t *testing.T) {
	runParity(t, `import io;
class Foo {}
Foo f = Foo();
io.println(typeof(f) == Foo);
io.println(typeof(f) == string);
io.println(typeof(42) == int);
io.println(typeof("hi") == string);
io.println(typeof(true) == bool);
io.println(typeof(3.14) == decimal);
io.println(typeof(3.14f) == float);
io.println(int == typeof(42));
io.println(string == typeof("world"));
io.println(typeof(f) == f.type);
io.println(f.type == Foo);
io.println(42.type == int);
io.println("hi".type == string);
io.println(typeof(Foo));
`, "true\nfalse\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\nFoo\n")
}

func TestParityPrimitiveNumericMethods(t *testing.T) {
	runParity(t, `import io;
int n = -7;
io.println(n.abs());
io.println(n.toString());
io.println(n.isNegative());
io.println(n.isPositive());
io.println(n.isZero());
`, "7\n-7\ntrue\nfalse\nfalse\n")
}

func TestParityIntBaseConversion(t *testing.T) {
	runParity(t, `import io;
io.println((255).toString(16));
io.println((255).toString(2));
io.println((-13).toString(2));
io.println("ff".toInt(16));
io.println("-101".toInt(2));
`, "ff\n11111111\n-1101\n255\n-5\n")
}

func TestParityPostfixIncrement(t *testing.T) {
	runParity(t, `import io;
int x = 5;
x++;
io.println(x);
x--;
x--;
io.println(x);
`, "6\n4\n")
}

func TestParityChainedMethodOnSmallIntResult(t *testing.T) {
	runParity(t, `import io;
let s = "hello";
io.println(s.length().toString());
let xs = [1, 2, 3];
io.println(xs.length().toString());
let d = {"a": 1, "b": 2};
io.println(d.length().toString());
let n = "42".length();
io.println(n.toString());
`, "5\n3\n2\n2\n")
}

func TestParityCompoundAssignArithmetic(t *testing.T) {
	runParity(t, `import io;
let x = 10;
x += 5;
io.println(x as string);
x -= 3;
io.println(x as string);
x *= 4;
io.println(x as string);
x %= 7;
io.println(x as string);
`, "15\n12\n48\n6\n")
}

func TestParityCompoundAssignIntDiv(t *testing.T) {
	runParity(t, `import io;
let x = 20;
x //= 3;
io.println(x as string);
`, "6\n")
}

func TestParityCompoundAssignPower(t *testing.T) {
	runParity(t, `import io;
let x = 3;
x **= 3;
io.println(x as string);
`, "27\n")
}

func TestParityCompoundAssignBitwise(t *testing.T) {
	runParity(t, `import io;
let a = 12;
a &= 10;
io.println(a as string);
let b = 5;
b |= 10;
io.println(b as string);
let c = 15;
c ^= 9;
io.println(c as string);
let d = 3;
d <<= 2;
io.println(d as string);
let e = 20;
e >>= 2;
io.println(e as string);
`, "8\n15\n6\n12\n5\n")
}

func TestParityInterpolateInt(t *testing.T) {
	runParity(t, `import io;
let n = 42;
io.println("n = ${n}");
`, "n = 42\n")
}

func TestParityForByStepNegative(t *testing.T) {
	runParity(t, `import io;
for (i in 10..0 by -2) {
    io.print(i as string + " ");
}
io.println("");
`, "10 8 6 4 2 0 \n")
}

func TestParityDecimalFormatSpec(t *testing.T) {
	// A decimal formats from its exact value, not a float64 round-trip,
	// so :f matches toString and shows no binary noise.
	runParity(t, `import io;
let d = (3.1415926536 as decimal);
io.println("${d:.13f}");
io.println(d.toString(13));
io.println("${(2.567 as decimal):.2f}");
io.println("${(-2.5 as decimal):.3f}");
io.println("${(0.125 as decimal):.1%}");
io.println("${(1234.5 as decimal):,.2f}");
`, "3.1415926536000\n3.1415926536000\n2.57\n-2.500\n12.5%\n1,234.50\n")
}

// TestParitySmallIntArithmetic verifies that integer arithmetic works correctly
// for values in the int64 range (SmallInt fast path in VM) and produces correct
// results visible at the Geblang level regardless of internal representation.
func TestParitySmallIntArithmetic(t *testing.T) {
	// Basic arithmetic
	runParity(t, `import io; io.println(2 + 3);`, "5\n")
	runParity(t, `import io; io.println(10 - 4);`, "6\n")
	runParity(t, `import io; io.println(3 * 7);`, "21\n")
	runParity(t, `import io; io.println(15 / 4);`, "3.7500000000\n")
	runParity(t, `import io; io.println(15 // 4);`, "3\n")
	runParity(t, `import io; io.println(17 % 5);`, "2\n")

	// Comparisons between int literals
	runParity(t, `import io; io.println(4 == 4);`, "true\n")
	runParity(t, `import io; io.println(4 != 5);`, "true\n")
	runParity(t, `import io; io.println(3 < 4);`, "true\n")
	runParity(t, `import io; io.println(4 <= 4);`, "true\n")
	runParity(t, `import io; io.println(5 > 4);`, "true\n")
	runParity(t, `import io; io.println(4 >= 4);`, "true\n")

	// Arithmetic with variables
	runParity(t, `import io; let x = 10; let y = 3; io.println(x + y);`, "13\n")
	runParity(t, `import io; let x = 10; let y = 3; io.println(x * y);`, "30\n")
	runParity(t, `import io; let x = 10; let y = 3; io.println(x % y);`, "1\n")

	// Negation
	runParity(t, `import io; let x = 5; io.println(-x);`, "-5\n")
	runParity(t, `import io; io.println(-(3 + 2));`, "-5\n")

	// Loop accumulation (typical integer fast-path scenario)
	runParity(t, `import io;
let sum = 0;
for (let i = 0; i < 100; i = i + 1) {
    sum = sum + i;
}
io.println(sum);
`, "4950\n")

	// Type name is "int" for integer expressions
	runParity(t, `import io; io.println(typeof(42));`, "int\n")
	runParity(t, `import io; io.println(typeof(1 + 1));`, "int\n")
}

// TestParitySmallIntComparisons verifies comparison between integer values
// including those returned from native functions (which may return SmallInt).
func TestParitySmallIntComparisons(t *testing.T) {
	// secrets.randomInt returns a SmallInt; compare against literal (also SmallInt in VM)
	runParity(t, `import io; import secrets;
let n = secrets.randomInt(1, 100);
io.println(n >= 1);
io.println(n <= 100);
`, "true\ntrue\n")

	// Comparison in sorted/ordered context
	runParity(t, `import io;
let nums = [5, 3, 1, 4, 2];
let sorted = nums.sorted();
io.println(sorted[0]);
io.println(sorted[4]);
`, "1\n5\n")

	// Range with int bounds (1..5 is inclusive, gives 5 iterations)
	runParity(t, `import io;
let count = 0;
for (n in 1..5) { count = count + 1; }
io.println(count);
`, "5\n")
}

// TestParitySmallIntOverflow verifies that arithmetic overflowing int64 promotes
// correctly to arbitrary-precision Int rather than wrapping or erroring.
func TestParitySmallIntOverflow(t *testing.T) {
	// 2^62 * 4 overflows int64 (max is ~9.2e18); result must still be correct
	runParity(t, `import io;
let a = 4611686018427387904;
let b = a * 4;
io.println(b > 0);
`, "true\n")

	// Adding large values
	runParity(t, `import io;
let big = 9223372036854775807;
let bigger = big + 1;
io.println(bigger > big);
`, "true\n")
}

// TestParitySmallIntWithNativeFunctions verifies that functions accepting int
// arguments work correctly when passed SmallInt values (the VM literal path).
func TestParitySmallIntWithNativeFunctions(t *testing.T) {
	// list.chunk with int size
	runParity(t, `import io;
let parts = [1,2,3,4,5].chunk(2);
io.println(parts.length());
`, "3\n")

	// String repeat
	runParity(t, `import io; io.println("ab".repeat(3));`, "ababab\n")

	// list.topK
	runParity(t, `import io;
let top = [3,1,4,1,5,9,2,6].topK(3);
io.println(top.length());
`, "3\n")

	// Bitwise operations
	runParity(t, `import io; io.println(6 & 3);`, "2\n")
	runParity(t, `import io; io.println(6 | 3);`, "7\n")
	runParity(t, `import io; io.println(6 ^ 3);`, "5\n")
	runParity(t, `import io; io.println(1 << 4);`, "16\n")
	runParity(t, `import io; io.println(16 >> 2);`, "4\n")
}

// TestParityFloorDivOnDecimalAndFloat verifies the `//` floor-
// division operator handles decimal / float operands (eval used
// to error with "unsupported decimal operator //"). Floor toward
// negative infinity matches Python's `//` and Geblang's
// established int//int behaviour.
func TestParityFloorDivOnDecimalAndFloat(t *testing.T) {
	runParity(t, `import io;

io.println(5 // 2);
io.println(-7 // 2);
io.println(7.5 // 2.0);
io.println((-7.5) // 2.0);
io.println(10.0 % 3.0);
io.println((-10.0) % 3.0);
`, "2\n-4\n3.0000000000\n-4.0000000000\n1.0000000000\n2.0000000000\n")
}

func TestParityConstantFolding(t *testing.T) {
	runParity(t, `import io;

io.println(3 + 5);
io.println(2 * 10);
io.println(20 - 4);
io.println(13 // 5);
io.println(-7 // 3);
io.println(7 % -3);
io.println(5 == 5);
io.println(5 == 6);
io.println(2 < 3);
io.println(1.5 + 2.5);
io.println(2.0 * 3.5);
io.println(0.5 < 1.0);
io.println("foo" + "bar");
io.println("a" == "b");
io.println(true == true);
io.println(true != false);
`, "8\n20\n16\n2\n-3\n-2\ntrue\nfalse\ntrue\n4.0000000000\n7.0000000000\ntrue\nfoobar\nfalse\ntrue\ntrue\n")
}

// Cross-type numeric mixing: int<->float promote in arithmetic; comparisons and
// membership compare by exact value across int/decimal/float.
func TestParityNumericMixing(t *testing.T) {
	runParity(t, `import io;
io.println(3 + 2.5f);
io.println(2.5f + 3);
io.println(5 / 2.0f);
io.println(3 + 2.5);
io.println(3 == 3.0f);
io.println(3 == 3.5f);
io.println(2.5 == 2.5f);
io.println(0.1 == 0.1f);
io.println(3 < 2.5f);
io.println(2.5 < 3.0f);
io.println([1, 2, 3].contains(2.0f));
io.println(3 != 3.0f);
`, "5.5\n5.5\n2.5\n5.5000000000\ntrue\nfalse\ntrue\nfalse\nfalse\ntrue\ntrue\nfalse\n")
}

// Followup adversarial: a function body that forward-references a top-level const (or let) declared later in the file resolves on both backends (the VM compiler now pre-allocates the global slot in the hoist pass).
func TestParityForwardConstReference(t *testing.T) {
	runParity(t, `import io;
func use(): int { return h + tail(); }
func tail(): int { return extra; }
const h = 42;
let extra = 8;
io.println(use() as string);
`, "50\n")
}
