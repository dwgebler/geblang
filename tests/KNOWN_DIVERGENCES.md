# Known VM divergences

`geblang test` runs on the bytecode VM by default. A test file that the VM
cannot compile fails loudly, naming the file and the gap.

This ledger records the rare, temporary exceptions: files that are accepted as
unable to run on the VM for now. Such a file must carry a
`# @vm-divergence: <key>` comment whose key matches a row below; `geblang test`
then runs it on the tree-walking evaluator instead and reports the count in its
summary. The runner cross-validates both directions: an annotation with no row
here, or a row here whose file lacks the annotation, is an error.

Keep this list empty whenever possible. An entry is a promise to fix the VM gap,
not a permanent waiver.

| key | file | reason | date |
|-----|------|--------|------|
| partial-overload-resolution | functions/partials_test.gb | partial application (`f(_)`) over multiple same-arity overloads: the VM resolves overloads at compile time and cannot pick one without the argument, the evaluator resolves at application; documented divergence | 2026-07-06 |
