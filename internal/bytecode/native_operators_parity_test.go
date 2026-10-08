package bytecode_test

import "testing"

// Operator dunders over native value classes: NDArray/Series
// arithmetic (+ - * / ** and unary -, scalars on either side), NDArray
// ordering masks (< > <= >=), dataframe Expr sugar, and pivot.

func TestParityNDArrayOperators(t *testing.T) {
	runParity(t, `import io;
import ndarray as nd;
let a = nd.array([1, 2, 3]);
let b = nd.array([10, 20, 30]);
io.println((a + b).toList());
io.println((b - a).toList());
io.println((a * 2).toList());
io.println((2 * a).toList());
io.println((10 / a).toList());
io.println((a ** 2).toList());
io.println((-a).toList());
io.println((a < b).toList());
io.println((a >= nd.array([1, 3, 2])).toList());
`, "[11, 22, 33]\n[9, 18, 27]\n[2, 4, 6]\n[2, 4, 6]\n[10, 5, 3.3333333333333335]\n[1, 4, 9]\n[-1, -2, -3]\n[1, 1, 1]\n[1, 0, 1]\n")
}

func TestParityNDArrayOperatorErrors(t *testing.T) {
	runParity(t, `import io;
import ndarray as nd;
let a = nd.array([1, 2]);
try {
    let bad = a + "text";
    io.println("added");
} catch (TypeError e) {
    io.println("caught: " + e.message);
}
`, "caught: unsupported operands for +: ndarray.NDArray and string\n")
}

func TestParityDataFrameExprOperatorSugar(t *testing.T) {
	runParity(t, `import io;
import dataframe as df;
let frame = df.fromDict({"age": [25, 35, 45], "score": [1, 2, 3]});
io.println(frame.filter(df.col("age") > 30).rows());
io.println(frame.filter(30 < df.col("age")).rows());
io.println(frame.withColumn("sum", df.col("age") + df.col("score")).col("sum").toList());
io.println(frame.withColumn("scaled", df.col("score") * 10).col("scaled").toList());
let s = frame.col("age");
io.println((s + 1).toList());
io.println((s / 5).toList());
io.println((-s).toList());
`, "2\n2\n[26, 37, 48]\n[10, 20, 30]\n[26, 36, 46]\n[5, 7, 9]\n[-25, -35, -45]\n")
}

func TestParityDataFramePivot(t *testing.T) {
	runParity(t, `import io;
import dataframe as df;
let sales = df.fromDict({
    "region": ["north", "north", "south", "south", "north"],
    "quarter": ["q1", "q2", "q1", "q2", "q1"],
    "amount": [10, 20, 30, 40, 5]
});
let p = sales.pivot({"index": "region", "columns": "quarter", "values": "amount", "agg": "sum"});
io.println(p.columns());
io.println("${p.toDicts()}");
let c = sales.pivot({"index": "region", "columns": "quarter", "values": "amount", "agg": "count"});
io.println("${c.toDicts()}");
`, "[\"region\", \"q1\", \"q2\"]\n[{\"region\": \"north\", \"q1\": 15, \"q2\": 20}, {\"region\": \"south\", \"q1\": 30, \"q2\": 40}]\n[{\"region\": \"north\", \"q1\": 2, \"q2\": 1}, {\"region\": \"south\", \"q1\": 1, \"q2\": 1}]\n")
}
