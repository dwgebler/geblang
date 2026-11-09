import sys

n = int(sys.argv[1]) if len(sys.argv) > 1 else 200_000
total = 0
for i in range(n):
    row = {"a": i, "b": i + 1, "c": i + 2}
    total = total + row["c"]
print(total)
