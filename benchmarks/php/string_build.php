<?php
$n = isset($argv[1]) ? (int) $argv[1] : 50000;
$s = "";
for ($i = 0; $i < $n; $i++) {
    $s = $s . $i . ",";
}
echo strlen($s), PHP_EOL;
