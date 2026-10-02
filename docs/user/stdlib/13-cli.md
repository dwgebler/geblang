# CLI

Use `cli` for interactive console applications and `args` for argument parsing.
The source module `cli.command` adds structured subcommand support.

## Argument parsing

`cli.parseArgs(argv, spec)` parses a list of command-line strings according to
a spec dict and returns the parsed values:

```gb
import cli;
import sys;

let opts = cli.parseArgs(sys.args(), {
    "name":    {"type": "string",  "short": "n", "required": true},
    "verbose": {"type": "bool",    "short": "v"},
    "port":    {"type": "int",     "default": 8080},
    "tags":    {"type": "list"}
});

io.println(opts["name"]);
if (opts["verbose"] as bool) {
    io.println("verbose mode");
}
```

`cli.help(spec)` prints a usage summary derived from the spec. The low-level
`args.parse(argv, spec)` and `args.help(spec)` functions provide the same
interface without the interactive features.

## Interactive prompts

```gb
let name  = cli.prompt("Your name: ");
let pass  = cli.password("Password: ");        # input hidden
let pass2 = cli.secret("Confirm password: ");  # same as password
let ok    = cli.confirm("Continue? [y/N] ");   # returns bool
let lang  = cli.choose("Language: ", ["gb", "py", "js"]);  # returns chosen string
let langs = cli.multiChoose("Languages:", ["gb", "py", "js"]);  # returns list<string>
```

`confirm` returns `true` when the user types `y` or `Y`. `choose` displays the
options and validates the input, re-prompting on invalid choices.

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

## `cli.command` - structured subcommands

Import the source module `cli.command` when a tool has multiple subcommands or
needs a reusable command tree:

```gb
import cli.command as cmd;

let deploy = cmd.newCommand("deploy", "Deploy the application")
    .option(cmd.newOption("env",  "string").required().help("Target environment"))
    .option(cmd.newOption("dry",  "bool").help("Dry run, no changes"))
    .option(cmd.newOption("tag",  "string").help("Image tag to deploy"));

let parsed = deploy.parse(sys.args());
if (parsed == null) {
    deploy.help();
    sys.exit(1);
}

io.println("deploying to " + parsed["env"]);
```

Use `newOption(name, kind)` with:
- `.required()` - fail if the option is absent
- `.help(text)` - add help text
- `.default(value)` - set a default value
- `.short(char)` - single-character alias

## Progress bars and spinners

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
