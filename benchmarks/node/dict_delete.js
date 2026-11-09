"use strict";

const n = process.argv[2] !== undefined ? parseInt(process.argv[2], 10) : 40_000;
const d = new Map();
for (let i = 0; i < n; i++) {
    d.set("k" + i, i);
}
for (let i = 0; i < n; i++) {
    d.delete("k" + i);
}
console.log(d.size);
