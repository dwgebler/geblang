"use strict";

const n = process.argv[2] !== undefined ? parseInt(process.argv[2], 10) : 50_000;
let s = "";
for (let i = 0; i < n; i++) {
    s = s + i + ",";
}
console.log(s.length);
