import sys

n = int(sys.argv[1]) if len(sys.argv) > 1 else 40_000
d = {}
for i in range(n):
    d["k" + str(i)] = i
for i in range(n):
    del d["k" + str(i)]
print(len(d))
