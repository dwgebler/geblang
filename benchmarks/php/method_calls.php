<?php
$n = isset($argv[1]) ? (int) $argv[1] : 500000;
$s = "hello world";
$xs = [1, 2, 3, 4];
$d = ["a" => 1, "b" => 2];
$total = 0;
for ($i = 0; $i < $n; $i++) {
    $total = $total + strlen($s) + count($xs) + ($d["a"] ?? 0);
}
echo $total, PHP_EOL;
