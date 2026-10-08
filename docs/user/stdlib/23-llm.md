# LLM (`llm`)

A provider-agnostic LLM client. One Geblang interface for chat
completions, text embeddings, image analysis, and image generation
across OpenAI, Anthropic, and AWS Bedrock.

```gb
import llm;
import sys;
import io;

let article = "Geblang is a typed scripting language with a dual-backend runtime.";

let c = llm.client({
    "provider": "anthropic",
    "apiKey":   sys.getenv("ANTHROPIC_API_KEY") as string
});

let resp = c.chat([
    {"role": "user", "content": "Summarise this in one sentence: " + article}
], {"model": "claude-opus-4-8", "maxTokens": 256});

io.println(resp["content"]);
io.println("tokens: " + ((resp["usage"] as dict<string, any>)["totalTokens"] as string));
```

## Client interface

Every provider implements the `llm.Client` interface (an alias of
`llm.contract.Client`, the module that providers import to declare
`implements`):

| Method | Description |
|--------|-------------|
| `chat(messages, opts)` | Chat completion. Returns `{content, model, stopReason, usage}`. |
| `embed(text, opts)` | Single-string text embedding. Returns `{vector, model, usage}`. |
| `analyzeImage(image, prompt, opts)` | Multimodal vision call. `image` is the raw bytes; returns the same shape as `chat`. |
| `generateImage(prompt, opts)` | Image generation. Returns `{images, model}` where `images` is a list of `{bytes, format}`. |
| `models()` | List the models available to this account. Returns a list of dicts, each with at least `id`. |
| `embedBatch(texts, opts)` | Embed many strings in one call. Returns `{vectors, model, usage}` with `vectors` aligned to `texts`. |

`usage` is a dict with `inputTokens`, `outputTokens`,
`totalTokens` (zeros when the provider does not report a number).

`opts.model` is required for every call and is a free-form string.
The library does not validate model names locally; pass whatever
the provider exposes (e.g. `"gpt-5"`, `"claude-opus-4-8"`,
`"anthropic.claude-opus-4-8-v1:0"` for Bedrock).

Other common opts: `maxTokens` (int), `temperature` (decimal /
float), `topP` (decimal / float), `system` (string for system
prompt).

## Picking a provider

```gb
let openai    = llm.client({"provider": "openai",    "apiKey": "..."});
let anthropic = llm.client({"provider": "anthropic", "apiKey": "..."});
let bedrock   = llm.client({"provider": "bedrock",
    "region":    "us-east-1",
    "accessKey": "...",
    "secretKey": "..."});
```

Provider-specific options:

| Provider | Required | Optional |
|----------|----------|----------|
| `openai` | `apiKey` | `endpoint` (default `https://api.openai.com`), `organization` |
| `anthropic` | `apiKey` | `endpoint` (default `https://api.anthropic.com`), `apiVersion` (default `2023-06-01`) |
| `bedrock` | `region`, `accessKey`, `secretKey` | `endpoint` (default regional Bedrock Runtime host) |

## Capability matrix

Not every provider supports every operation. Calls into a
combination that is not supported throw a `RuntimeError` naming
the missing operation, so failures are immediate.

| Operation | OpenAI | Anthropic | Bedrock |
|-----------|--------|-----------|---------|
| `chat` | yes | yes | yes (Claude models via Anthropic Messages schema) |
| `embed` | yes | throws | yes (Titan / Cohere model families) |
| `analyzeImage` | yes (gpt-4o, etc.) | yes (claude vision) | yes (Claude models) |
| `generateImage` | yes (dall-e-*) | throws | yes (Titan / Stability model families) |
| `models` | yes (`GET /v1/models`) | yes (`GET /v1/models`) | yes (ListFoundationModels) |
| `embedBatch` | yes (input array) | throws | yes (per-text loop) |
| `chatStream` | yes (SSE) | yes (SSE) | throws (binary event-stream) |

Bedrock dispatches by model id prefix:

- `chat` / `analyzeImage`: routed through the Anthropic Messages
  schema, covering every `anthropic.claude-*` model on Bedrock.
- `embed`: `amazon.titan-embed-*` and `cohere.embed-*` are folded
  into the common shape. The Cohere call accepts an extra
  `inputType` opt (default `"search_document"`); pass
  `"search_query"` when embedding a user query.
- `generateImage`: `amazon.titan-image-*` and `stability.*` are
  folded into the common shape. Common opts: `width`, `height`,
  `n` (Titan), `steps`, `cfgScale`, `seed` (Stability).

Calls with an unrecognised Bedrock model id throw a clear
`RuntimeError` directing the caller to the lower-level
`bedrock.invoke(model, payload)` escape hatch for model families
the v1 common shape does not cover (e.g. Llama, Mistral).

## Embeddings

`embed(text, opts)` returns a single dense vector; `embedBatch(texts,
opts)` embeds many strings in one request and returns `vectors`
aligned to the input. The embedding model is chosen per call via
`opts.model` (e.g. `"text-embedding-3-small"`); list what the
provider exposes with `models()`.

```gb
import llm;

let client = llm.client({"provider": "openai", "apiKey": apiKey});

# Pick or discover a model
let available = client.models();          # [{id, ...}, ...]

# One text
let one = client.embed("hello world", {"model": "text-embedding-3-small"});
one["vector"];                            # list<float>

# Many texts in a single call (preferred when indexing a corpus)
let batch = client.embedBatch(
    ["first chunk", "second chunk", "third chunk"],
    {"model": "text-embedding-3-small"}
);
batch["vectors"];                         # list<list<float>>, aligned to input
```

The returned vectors feed directly into `vectorstore` (to persist
and search) and `rag` (to build retrieval context). See
[Vector stores and RAG](24-vectorstore-rag.md) for the end-to-end
create / store / search / RAG flow. For local, offline embeddings
without an API call, see the `transformers` encoder.

## Image analysis

`analyzeImage` takes bytes plus a prompt. The provider request is
constructed with the image inline as base64; pass the actual image
bytes (typically loaded via `io.readBytes` or fetched with
`http.get`).

```gb
let png = io.readBytes("./screenshot.png");
let resp = c.analyzeImage(png, "What error message is visible?", {
    "model":     "claude-opus-4-8",
    "mimeType":  "image/png",   /* optional; default image/png */
    "maxTokens": 512
});
io.println(resp["content"]);
```

## Image generation

```gb
let resp = c.generateImage("a photorealistic red square on a black background", {
    "model": "dall-e-3",
    "size":  "1024x1024",
    "n":     1
});
let images = resp["images"] as list<any>;
for (img in images) {
    let item = img as dict<string, any>;
    io.writeBytes("./out.png", item["bytes"] as bytes);
}
```

OpenAI is the only provider that supports image generation in
v1; calls on the other clients throw.

## Streaming

`chatStream(messages, opts, callback)` streams a chat completion. The callback
is invoked with each incremental content delta (a string) as it arrives, and
the method returns the assembled `{content, model, stopReason, usage}` once the
stream ends. Same `opts` as `chat` (including `tools`).

```gb
let full = c.chatStream([{"role": "user", "content": "Write a haiku."}],
                        {"model": "gpt-5"},
                        func(string delta): void {
    io.print(delta);   /* render tokens as they arrive */
});
io.println("");
io.println("done: " + (full["stopReason"] as string));
```

Streaming is supported for OpenAI and Anthropic (both speak server-sent events).
Bedrock uses a different binary event-stream protocol and `chatStream` throws
there; use `chat` for a single Bedrock response.

## Tool / function calling

Pass `opts.tools` to `chat` to let the model request a function
call. Tools are provider-neutral; each backend translates them to
its native format (OpenAI `tools`, Anthropic / Bedrock `input_schema`).
Each tool is `{name, description, parameters}` where `parameters`
is a JSON Schema object describing the arguments.

When the model decides to call a tool, the result gains a
`toolCalls` list of `{id, name, arguments}` (arguments already
parsed into a dict) and `stopReason` flags it (`"tool_calls"` /
`"tool_use"`). Run the tool yourself, then continue the
conversation: echo the assistant's call as a message with
`toolCalls`, and supply the result as a `{role: "tool",
toolCallId, content}` message. The same neutral shape works on
every provider.

```gb
let tools = [{
    "name":        "get_weather",
    "description": "Get the current weather for a city",
    "parameters":  {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
}];

let first = c.chat([{"role": "user", "content": "Weather in Paris?"}],
                   {"model": "gpt-5", "tools": tools});

if (first.contains("toolCalls")) {
    let call = (first["toolCalls"] as list<any>)[0] as dict<string, any>;
    let city = (call["arguments"] as dict<string, any>)["city"] as string;
    let result = lookupWeather(city);   /* your function */

    let final = c.chat([
        {"role": "user",      "content": "Weather in Paris?"},
        {"role": "assistant", "content": "", "toolCalls": first["toolCalls"]},
        {"role": "tool",      "toolCallId": call["id"], "content": result}
    ], {"model": "gpt-5", "tools": tools});
    io.println(final["content"]);
}
```

Tool calling works on `chat` for every provider that backs `chat`
(OpenAI, Anthropic, and Claude models on Bedrock). `opts.toolChoice`
(OpenAI) forces or restricts which tool the model may call.

## Errors

A non-2xx HTTP response from the upstream API raises a
`RuntimeError` carrying the operation, status code, and response
body. Catch as a normal Geblang error:

```gb
try {
    let resp = c.chat(messages, {"model": "claude-opus-4-8"});
} catch (RuntimeError e) {
    /* rate limit, invalid key, validation error, etc. */
    io.println("LLM call failed: " + e.message);
}
```
