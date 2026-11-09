<?php
$n = isset($argv[1]) ? (int) $argv[1] : 200000;
$total = 0;
for ($i = 0; $i < $n; $i++) {
    $row = ["a" => $i, "b" => $i + 1, "c" => $i + 2];
    $total = $total + $row["c"];
}
echo $total, PHP_EOL;
