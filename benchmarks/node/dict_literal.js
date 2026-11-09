"use strict";

const n = process.argv[2] !== undefined ? parseInt(process.argv[2], 10) : 200_000;
let total = 0;
for (let i = 0; i < n; i++) {
    const row = { a: i, b: i + 1, c: i + 2 };
    total = total + row.c;
}
console.log(total);
