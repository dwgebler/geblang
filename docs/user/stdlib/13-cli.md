# CLI

Use `cli` for interactive console applications and `args` for argument parsing.
The source module `cli.command` adds reusable command option builders.

## Argument parsing

`cli.parseArgs(argv, spec)` parses a list of command-line strings according to
a spec dict and returns the parsed values:

```gb
import cli;
import sys;
import io;

let opts = cli.parseArgs(sys.args(), {
    "name":    {"type": "string",  "short": "n", "required": true},
    "verbose": {"type": "bool",    "short": "v"},
    "port":    {"type": "int",     "default": 8080},
    "tag":     {"type": "string"}
});

io.println(opts["name"]);
if (opts["verbose"] as bool) {
    io.println("verbose mode");
}
```

`cli.help(name, spec)` returns usage text for the program `name`, derived from
the spec. The low-level `args.parse(argv, spec)` and `args.help(name, spec)`
functions provide the same interface without the interactive features.

## Interactive prompts

```gb
let name  = cli.prompt("Your name: ");
let pass  = cli.password("Password: ");        # input hidden
let pass2 = cli.secret("Confirm password: ");  # same as password
let ok    = cli.confirm("Continue? [y/N] ");   # returns bool
let lang  = cli.choose("Language: ", ["gb", "py", "js"]);  # returns chosen string
let langs = cli.multiChoose("Languages:", ["gb", "py", "js"]);  # returns list<string>
```

`confirm` accepts `y`/`yes` and `n`/`no`, ignoring case and surrounding spaces.
Invalid answers re-prompt. Blank input uses its second argument, defaulting
to false. `choose` displays numbered options and retries invalid choices;
its optional third argument is a zero-based default index for blank input.
Both raise `IOError` at EOF, even if a default is supplied.

### Nullable input and typed prompts

`cli.readLine(label = "") -> ?string` preserves blank input as `""` and
returns null at EOF. A final line without a newline is returned once before
EOF. `cli.prompt` retains its string result, returning `""` at EOF.

`cli.promptInt(label, options = {}) -> ?int` parses and validates a whole
number, re-prompting after invalid text or a rejected value:

```gb
let count = cli.promptInt("Tosses (q to stop): ", {
    "min": 1, "max": 1000000, "cancel": ["q", "quit", "exit"]
});
if (count != null) { io.println("Simulating ${count} tosses"); }
```

Entering `no`, `0`, then `10` prints a parsing error, then a minimum error,
and returns 10. EOF or a cancel token returns null. Cancel matching ignores
case and outer spaces. Blank input retries unless a `default` is supplied;
a default takes precedence over a blank cancel token. Add `""` to `cancel`
to let blank input cancel when there is no default. Integer parsing matches
`string.toInt()`, including literal prefixes and digit separators.

Shared rules work in `promptInt` options and each `cli.parseArgs` option:

| Rule | Meaning |
|------|---------|
| `min`, `max` | Inclusive finite numeric bounds. |
| `choices` | List of permitted values, compared by value. |
| `validate` | Callable receiving the parsed value; null accepts it, a string rejects it with that message. |

Bounds and choices run before the custom validator. Invalid prompt
configuration or an invalid default raises an error. Argument validation
returns a message in the parser's existing `error` entry:

```gb
let parsed = cli.parseArgs(["--count", "0"], {
    "count": {"type": "int", "min": 1, "max": 1000000}
});
io.println(parsed["error"]); /* option --count: must be at least 1 */
```

`args.parse` supports bounds and choices; custom Geblang callbacks are
supported by `cli.parseArgs`.

### Scoped CLI contexts

`cli.withContext(options, body)` returns the callback result and restores
the previous context after a return or an exception. Options are `stdin`
(a string or readable IO stream/file), `stdout` and `stderr` (writable
streams/files), `quiet` (suppress prompts and retry messages), and `terminal`
(override terminal detection). Omitted options inherit the current context.

```gb
let captured = io.memory();
let value = cli.withContext({"stdin": "bad\n12\n", "stdout": captured}, func(): ?int {
    return cli.promptInt("Count: ", {"min": 1});
});
io.println(value); /* 12 */
io.println(io.toString(captured)); /* prompts and the parsing error */
```

CLI and IO readers share buffered input, including `io.stdinReadLine`,
`io.stdinReadAll` and `io.readLine(io.stdin())`. Nested contexts preserve
unread outer input. `io.withStdin` also shares this input and suppresses
prompts for existing tests. Scope contexts around synchronous work; finish
or await any child tasks before the callback returns. Concurrent code must
not change the same active context.

`cli.isTerminal()` reports whether stderr is a terminal, respecting the
context's `terminal` override. Progress goes to stderr so stdout can carry
machine-readable results.

`multiChoose` selects several options at once. On an interactive terminal it
shows an arrow-key checkbox list (up/down or `j`/`k` move, space toggles, `a`
toggles all, enter confirms, `q` or ctrl-c cancels). When stdin is not a
terminal (piped input, CI) it falls back to a numbered list and reads
comma-separated choices, so scripts and tests keep working. An optional third
argument pre-checks options by index:

```gb
let picked = cli.multiChoose("Pick services:",
    ["web", "db", "cache", "queue"],
    [0, 1]);   # web and db checked to start
```

## Text styling

`cli.style(text, options)` returns an ANSI-styled string. `cli.stripAnsi(text)`
removes all escape sequences.

```gb
io.println(cli.style("Success", {"fg": "green", "bold": true}));
io.println(cli.style("Warning", {"fg": "yellow"}));
io.println(cli.style("Error",   {"fg": "red",   "bold": true}));
io.println(cli.style("Info",    {"fg": "cyan",  "italic": true}));
```

Style options:

| Key         | Values                                                    |
|-------------|-----------------------------------------------------------|
| `fg`        | `"black"`, `"red"`, `"green"`, `"yellow"`, `"blue"`, `"magenta"`, `"cyan"`, `"white"`, `"default"` |
| `bg`        | same colour names as `fg`                                 |
| `bold`      | `true`                                                    |
| `italic`    | `true`                                                    |
| `underline` | `true`                                                    |
| `dim`       | `true`                                                    |

`stripAnsi` is useful when writing styled output to a log file or when
detecting that stdout is not a terminal:

```gb
let message = cli.style("done", {"fg": "green"});
io.println(cli.stripAnsi(message));   # "done"
```

## Tables

`cli.table(rows, options)` renders a list of dicts as an aligned text table
and returns the formatted string:

```gb
let rows = [
    {"name": "Alice", "role": "admin",  "active": "yes"},
    {"name": "Bob",   "role": "viewer", "active": "no"},
    {"name": "Carol", "role": "editor", "active": "yes"}
];

io.println(cli.table(rows, {
    "columns": ["name", "role", "active"],
    "headers": ["Name", "Role", "Active"]
}));
```

Output:

```
Name   Role    Active
----   ------  ------
Alice  admin   yes
Bob    viewer  no
Carol  editor  yes
```

Options:

| Key         | Description                                                |
|-------------|------------------------------------------------------------|
| `columns`   | list of dict keys to include, in order                     |
| `headers`   | list of header labels (defaults to the column key names)   |
| `separator` | column separator string (default: two spaces)              |
| `strict` | Raise ValueError for missing keys instead of leaving blank cells (default: false). |

A column can also be a descriptor. `key` selects a dict field; `header`
defaults to that key; `align` is `left` (default), `right` or `center`;
`format` uses the same format specification as string interpolation:

```gb
io.println(cli.table([{"outcome": "Heads", "share": 0.5}], {"columns": [
    {"key": "outcome", "header": "Outcome"},
    {"key": "share", "header": "Share", "align": "right", "format": ".2%"}
]}));
/* Outcome   Share
   -------  ------
   Heads    50.00% */
```

For class instances, use `value` instead of `key` to supply a callable
receiving the row, for example `{"header": "Name", "value": func(User row):
string { return row.name; }}`. Accessors may return any formattable value.
Exceptions from accessors propagate to the caller. String columns and the
legacy bare header list keep their existing behavior.

Columns are sized by displayed width, not byte length: accented and other
multi-byte characters count as one column, East Asian wide characters as
two, and ANSI styling from `cli.style` or `cli.color` as none. A styled or
non-ASCII cell therefore lines up with plain ones:

```gb
io.println(cli.table([
    {"check": "build", "result": cli.style("ok", {"fg": "green"})},
    {"check": "café",  "result": cli.style("failed", {"fg": "red"})}
], {"columns": ["check", "result"]}));
```

## `cli.command` - command builders

Import the source module `cli.command` to build a reusable option schema,
parse arguments and generate help text:

```gb
import cli.command as cmd;
import io;
import sys;

let deploy = cmd.newCommand("deploy", "Deploy the application")
    .option(cmd.newOption("env",  "string").required().help("Target environment"))
    .option(cmd.newOption("dry",  "bool").help("Dry run, no changes"))
    .option(cmd.newOption("tag",  "string").help("Image tag to deploy"));

let parsed = deploy.parse(sys.args());
if (parsed["error"] != null) {
    io.println(parsed["error"]);
    io.println(deploy.help());
    sys.exit(1);
}

io.println("deploying to " + parsed["env"]);
```

Use `newOption(name, kind)` with:
- `.required()` - fail if the option is absent
- `.help(text)` - add help text
- `.default(value)` - set a default value
- `.short(char)` - single-character alias
- `.min(value)`, `.max(value)`, `.choices(values)`, `.validate(callback)` -
  apply the shared validation rules after parsing

## Progress bars and spinners

### Managed progress

Prefer `widgets.withProgress(total, label, body, options = {})` for scoped
work. It returns the body's result and always clears any rendered bar,
including when the body throws. Off-terminal it emits no progress output.

```gb
import cli.widgets as widgets;

widgets.withProgress(100, "Processing", func(widgets.ManagedProgress progress): void {
    for (i in 0..<100) {
        /* Process one item. */
        progress.advance();
    }
});
```

Options: `width` (default 30, range 1..1000), `intervalMs` (default 80,
minimum 0), and `enabled` (default true; false suppresses rendering).
Total must be positive. Updates are throttled using monotonic time;
completion renders immediately. `advance(n = 1)` and `set(value)` check
cancellation, clamp the count to 0..total, and update the display.
`updateLabel(text)` changes the label.

`progress.cancel()` requests cooperative cancellation. `checkCancelled()`
or the next advance/set raises `widgets.CancelledError`; `withProgress`
also checks before returning a successful result. Catch that class to
handle cancellation. The helper does not register signal handlers: the
application owns its signal handlers and decides when to call `cancel()`.
Cancellation requests use shared storage; rendering and count updates
belong to the task running the progress body.

Import the source module `cli.widgets` for two terminal progress
indicators. Both draw to stderr, so they don't interfere with piped
stdout.

### `Spinner`

`widgets.Spinner(message)` shows a rotating frame next to a message
while a task runs. The caller owns the cadence: call `.tick()` to
advance one frame, `.update(message)` to change the label, and
`.stop(finalMessage?)` to clear the line.

```gb
import cli.widgets as widgets;

let sp = widgets.Spinner("connecting");
for (let int i = 0; i < 20; i++) {
    sp.tick();
    # ... do a slice of work ...
}
sp.update("fetching data");
sp.tick();
sp.stop("connected");
```

| Method | Description |
|--------|-------------|
| `Spinner(message)` | Create a spinner with an initial message. |
| `tick()` | Draw the next frame. |
| `update(message)` | Change the label shown after the spinner. |
| `stop(finalMessage = "")` | Clear the line; optionally print a final message. |

### `ProgressBar`

`widgets.ProgressBar(total, width = 30, label = "")` renders a bar
like `[#####-----] 50% (5/10) label`. Use `.advance(n = 1)` for the
common increment, `.set(value)` for an explicit position,
`.updateLabel(label)` to change the trailing text, and `.finish()`
when done.

```gb
import cli.widgets as widgets;

let bar = widgets.ProgressBar(10, 20, "downloading");
for (let int i = 0; i < 10; i++) {
    bar.advance();
    # ... download one chunk ...
}
bar.finish("done");
```

| Method | Description |
|--------|-------------|
| `ProgressBar(total, width = 30, label = "")` | Create a bar of `width` columns over `total` units. |
| `advance(n = 1)` | Add `n` to the current count and redraw. |
| `set(value)` | Set the current count to `value` and redraw. |
| `updateLabel(label)` | Change the trailing label and redraw. |
| `finish(finalMessage = "")` | Clear the line; optionally print a final message. |

## `cli.color` - ANSI terminal styling

```gb
import cli.color as color;
import io;

io.println(color.bold(color.red("error: ") + "build failed"));
io.println(color.dim("hint: run with --verbose for details"));
```

Each helper wraps its argument with the matching ANSI escape sequence
and a reset. The helpers honour the [`NO_COLOR`](https://no-color.org/)
convention: when `NO_COLOR` is set in the environment to any non-empty
value, every wrapper returns its input unchanged. This makes the same
code work on plain terminals, in CI logs, and in interactive shells
without an `isTTY` check.

Styles: `bold`, `dim`, `italic`, `underline`.

Foreground colors: `black`, `red`, `green`, `yellow`, `blue`, `magenta`,
`cyan`, `white`.

Background colors: `bgBlack`, `bgRed`, `bgGreen`, `bgYellow`, `bgBlue`,
`bgMagenta`, `bgCyan`, `bgWhite`.

| Function | Returns | Description |
|----------|---------|-------------|
| `color.isEnabled()` | `bool` | `true` when NO_COLOR is unset or empty - i.e., when wrappers actually emit escape codes. |

To toggle color off at runtime, set the env var explicitly:

```gb
import sys;
sys.setenv("NO_COLOR", "1");
```
