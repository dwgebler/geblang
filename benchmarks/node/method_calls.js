"use strict";

const n = process.argv[2] !== undefined ? parseInt(process.argv[2], 10) : 500_000;
const s = "hello world";
const xs = [1, 2, 3, 4];
const d = new Map([["a", 1], ["b", 2]]);
let total = 0;
for (let i = 0; i < n; i++) {
    total = total + s.length + xs.length + d.get("a");
}
console.log(total);
