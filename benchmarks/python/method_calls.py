import sys

n = int(sys.argv[1]) if len(sys.argv) > 1 else 500_000
s = "hello world"
xs = [1, 2, 3, 4]
d = {"a": 1, "b": 2}
total = 0
for i in range(n):
    total = total + len(s) + len(xs) + d.get("a")
print(total)
