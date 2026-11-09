<?php
$n = isset($argv[1]) ? (int) $argv[1] : 40000;
$d = [];
for ($i = 0; $i < $n; $i++) {
    $d["k" . $i] = $i;
}
for ($i = 0; $i < $n; $i++) {
    unset($d["k" . $i]);
}
echo count($d), PHP_EOL;
