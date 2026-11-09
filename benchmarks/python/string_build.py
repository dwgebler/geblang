import sys

n = int(sys.argv[1]) if len(sys.argv) > 1 else 50_000
s = ""
for i in range(n):
    s = s + str(i) + ","
print(len(s))
