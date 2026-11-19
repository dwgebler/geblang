# Local models (`transformers`, `onnx`)

> Experimental (1.24.0). Local, offline AI/ML: WordPiece tokenization, ONNX model
> inference, and sentence-transformer embedding. The surface may change.

These modules run models on the local machine with no network call, complementing
the API-backed [`llm`](23-llm.md) module. Tokenization (`transformers.tokenize`)
is pure Geblang-native and needs nothing external. Model inference
(`onnx.session`, `transformers.pool`, `rag.LocalEmbedder`) loads the ONNX Runtime
shared library, is gated behind the `--allow-onnx` launch flag, and needs the
one-time **[Setup](#setup)** below.

## Setup

Model inference needs two things that are not bundled: the ONNX Runtime shared
library, and an ONNX model with its tokenizer. (Tokenization alone needs neither.)

### 1. Install ONNX Runtime

Download a prebuilt ONNX Runtime for your platform from the official releases
(<https://github.com/microsoft/onnxruntime/releases>) and extract it. Geblang
pins the ONNX Runtime C API to version 27, so use **ONNX Runtime 1.27.0 or
newer**; an older runtime fails at load with a clear "does not support ORT API
version 27" message.

```sh
# Linux x64 (pick the matching asset for macOS / Windows / arm64)
curl -LO https://github.com/microsoft/onnxruntime/releases/download/v1.27.0/onnxruntime-linux-x64-1.27.0.tgz
tar xzf onnxruntime-linux-x64-1.27.0.tgz
```

The library is `lib/libonnxruntime.so` (`.dylib` on macOS, `onnxruntime.dll` on
Windows). Geblang locates it, in order of precedence:

1. `opts.libPath` passed to `onnx.session(modelPath, {"libPath": "..."})`,
2. the `GEBLANG_ONNXRUNTIME` environment variable,
3. the system loader path (e.g. after copying the library into `/usr/local/lib`
   and running `ldconfig`).

```sh
export GEBLANG_ONNXRUNTIME="$PWD/onnxruntime-linux-x64-1.27.0/lib/libonnxruntime.so"
```

### 2. Get a model

You need an ONNX model file plus its `tokenizer.json`. Any BERT-family WordPiece
sentence encoder works (`all-MiniLM-L6-v2`, `bge-small-en`, `e5-small`);
HuggingFace publishes ONNX exports under each model's `onnx/` folder.

```sh
mkdir -p models/all-MiniLM-L6-v2 && cd models/all-MiniLM-L6-v2
BASE=https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main
curl -L -o model.onnx     "$BASE/onnx/model.onnx"
curl -L -o tokenizer.json "$BASE/tokenizer.json"
```

`onnx.session` and `rag.LocalEmbedder` expect `model.onnx` and `tokenizer.json`
side by side in the model directory:

```
models/all-MiniLM-L6-v2/
  model.onnx
  tokenizer.json
```

### 3. Run

Pass `--allow-onnx` (model inference loads a native shared library). A minimal
first run:

```gb
/* embed.gb */
import rag;
import io;

let embedder = rag.LocalEmbedder("./models/all-MiniLM-L6-v2");
io.println(embedder.embed("hello world").length());   # 384 for all-MiniLM
embedder.close();
```

```sh
geblang --allow-onnx embed.gb
```

Without `--allow-onnx` the call raises a `PermissionError`; a missing library or
an incompatible ONNX Runtime version raises a clear load error.

## `transformers.tokenize`

`transformers.tokenize(tokenizerJson, texts, opts = {})` runs WordPiece
tokenization, the input stage for BERT-family sentence encoders (all-MiniLM,
bge, e5). `tokenizerJson` is the contents of a HuggingFace `tokenizer.json`
(read it yourself, e.g. with `io.readText`), and `texts` is a list of strings.

It returns a dict of `input_ids`, `attention_mask`, and `token_type_ids`, each a
list of integer rows aligned to `texts` and padded to a common width:

```gb
import transformers;
import io;

let tokenizerJson = io.readText("./model/tokenizer.json");
let batch = transformers.tokenize(tokenizerJson, ["Hello world", "Another sentence"]);

batch["input_ids"];        # list<list<int>>, e.g. [[101, 7592, 2088, 102, 0], ...]
batch["attention_mask"];   # 1 for real tokens, 0 for padding
batch["token_type_ids"];   # all 0 for single-sequence input
```

The tokenizer honours the `tokenizer.json` `normalizer` (lowercasing and accent
stripping for BertNormalizer), splits on whitespace and punctuation, performs
greedy longest-match WordPiece with `##` continuations, and wraps each sequence
with the `[CLS]` / `[SEP]` special tokens.

| `opts` key | Default | Meaning |
|------------|---------|---------|
| `maxLength` | 512 | Truncate each sequence to this many tokens (including specials). |
| `addSpecialTokens` | `true` | Wrap with `[CLS]` / `[SEP]`. |
| `padding` | batch | `"max_length"` pads every row to `maxLength`; otherwise rows pad to the longest row in the batch. |

Only WordPiece tokenizers are supported; BPE and SentencePiece are out of scope.
A parsed `tokenizer.json` is cached by content, so repeated calls with the same
tokenizer do not re-parse it.

## `onnx.session`

`onnx.session(modelPath, opts = {})` loads an ONNX model for local inference. It
requires the `--allow-onnx` launch flag (inference loads a native shared library)
and the ONNX Runtime library, located via `opts.libPath`, the
`$GEBLANG_ONNXRUNTIME` environment variable, or the system loader path. Without
the flag the call throws a `PermissionError`.

The returned `Session`:

| Method | Description |
|--------|-------------|
| `run(inputs)` | Runs the model. `inputs` maps each tensor name to an **int64** ndarray; returns a dict mapping each output name to a **float64** ndarray. |
| `inputNames()` | The model's input tensor names. |
| `outputNames()` | The model's output tensor names. |
| `close()` | Releases the session and ONNX Runtime resources. |

`opts`: `libPath` (path to `libonnxruntime.so`), `intraOpThreads` (int).

```gb
import onnx;
import transformers;
import ndarray;
import io;

let dir = "./models/all-MiniLM-L6-v2";
let tok = transformers.tokenize(io.readText(dir + "/tokenizer.json"), ["hello world"]);

let sess = onnx.session(dir + "/model.onnx");          # run with: geblang --allow-onnx
let out = sess.run({
    "input_ids":      ndarray.array(tok["input_ids"]),
    "attention_mask": ndarray.array(tok["attention_mask"]),
    "token_type_ids": ndarray.array(tok["token_type_ids"]),
});
io.println(out["last_hidden_state"].shape());          # [1, 4, 384]
sess.close();
```

Inputs must be int64 ndarrays (the model's token ids/masks); outputs come back
as float64 ndarrays, so they feed straight into `ndarray` pooling and `vecmath`.
The binding pins the ONNX Runtime C API to a fixed version; an incompatible
runtime fails loudly at load.

## Sentence embeddings (end-to-end)

`transformers.pool(hidden, attentionMask, opts = {})` reduces a model's
`[batch, seq, dim]` hidden state and its `[batch, seq]` attention mask to
`[batch, dim]` sentence embeddings. `opts.pooling` is `"mean"` (default, weighted
by the mask), `"cls"`, or `"max"`; `opts.normalize` (default true) L2-normalizes
each row. This is the last step of a sentence-transformer encoder.

Putting tokenize, `onnx.session`, and `pool` together gives fully local, offline
sentence embeddings (no API call):

```gb
import onnx;
import transformers;
import ndarray;
import vecmath;
import io;

let dir   = "./models/all-MiniLM-L6-v2";
let tjson = io.readText(dir + "/tokenizer.json");
let sess  = onnx.session(dir + "/model.onnx");        # geblang --allow-onnx

func encode(list<string> texts): list<any> {
    let tok = transformers.tokenize(tjson, texts);
    let out = sess.run({
        "input_ids":      ndarray.array(tok["input_ids"]),
        "attention_mask": ndarray.array(tok["attention_mask"]),
        "token_type_ids": ndarray.array(tok["token_type_ids"]),
    });
    return transformers.pool(out["last_hidden_state"], ndarray.array(tok["attention_mask"])).toList() as list<any>;
}

let e = encode(["a dog runs in the park", "a puppy plays outside", "quantum physics"]);
vecmath.score("cosine", e[0] as list<any>, e[1] as list<any>);   # ~0.45 (similar)
vecmath.score("cosine", e[0] as list<any>, e[2] as list<any>);   # ~-0.09 (unrelated)
sess.close();
```

The resulting vectors plug directly into the [`vectorstore` and `rag`](24-vectorstore-rag.md)
modules and `vecmath.semanticSearch`, giving a fully on-device retrieval stack.
