# Vector Search and RAG (`vectorstore`, `rag`)

Two source modules for working with embeddings: `vectorstore` stores dense
vectors and ranks them by similarity, and `rag` layers chunking, indexing,
retrieval, and prompt-context assembly on top for retrieval-augmented
generation. Both are pure Geblang and run on the bytecode VM.

They build on the embedding vectors produced by the [`llm`](llm.html) module
(`client.embed(text, opts)`), but `vectorstore` is independent of any provider
and `rag` accepts any embedder you supply.

## End-to-end: create, store, search

Embed a corpus with a cloud LLM, persist the vectors, and search them. The
embeddings come straight from `llm` (`embedBatch` for the corpus, `embed` for
the query); everything downstream is provider-agnostic.

```gb
import llm;
import vectorstore;
import io;

let client = llm.client({"provider": "openai", "apiKey": apiKey});
let model  = "text-embedding-3-small";

# 1. Embed a corpus in one batched request
let docs = [
    "Cats are small carnivorous mammals.",
    "Cars are wheeled motor vehicles.",
    "Python is a high-level programming language."
];
let vectors = client.embedBatch(docs, {"model": model})["vectors"] as list<any>;

# 2. Store the vectors with metadata
let store = vectorstore.MemoryVectorStore();
for (let i = 0; i < docs.length(); i = i + 1) {
    store.add("doc-" + (i as string), vectors[i], {"text": docs[i]});
}

# 3. Embed a query and retrieve the nearest documents
let q    = client.embed("Tell me about felines", {"model": model})["vector"];
let hits = store.search(q, 2);
for (hit in hits) {
    io.println((hit.score as string) + "  " + (hit.record.metadata["text"] as string));
}
```

Swap `MemoryVectorStore()` for `SqliteVectorStore(conn)` or `PgVectorStore(...)`
to persist, with no other changes. To turn retrieved documents into a grounded
answer, hand the same embedder to `rag` (next section) and feed the assembled
context to `client.chat`.

## `vectorstore`

A vector store keeps `(id, vector, metadata)` records and answers
nearest-neighbour queries. Two implementations share one `VectorStore`
interface:

| Store | Use |
|-------|-----|
| `MemoryVectorStore(metric = "cosine")` | Brute-force in-memory search; mutex-guarded so one shared store is safe under concurrent requests. Ideal up to ~1e4-1e5 vectors. |
| `SqliteVectorStore(conn, table = "vectors", metric = "cosine")` | Persistent store backed by a `db` Connection. Vectors are stored as little-endian float32 BLOBs and metadata as JSON; the table is created if absent and `add` upserts by id. |
| `PgVectorStore(conn, table = "vectors", dimension, metric = "cosine")` | Postgres + pgvector backend with real approximate-nearest-neighbour search. See the pgvector section below. |
| `HnswVectorStore(metric = "cosine", m = 16, efSearch = 20)` | In-process HNSW index: sublinear approximate search with no external service. See the HNSW section below. |

Metric is `"cosine"` (default), `"dot"`, or `"euclidean"`; all scores follow
"higher = closer". Vectors are `list<any>` so they accept the decimal numbers
that come back from JSON-parsed embeddings as well as float literals. Vectors are
stored packed as float32 and ranked by the native `vecmath` kernel (below), so
search is well off the interpreted path.

```gb
import vectorstore;
import io;

let store = vectorstore.MemoryVectorStore();
store.add("cats", [0.1, 0.2, 0.9], {"text": "about cats"});
store.add("cars", [0.9, 0.1, 0.1], {"text": "about cars"});

let hits = store.search([0.1, 0.2, 0.8], 1);
io.println(hits[0].record.metadata["text"]);   # about cats
io.println(hits[0].score);                      # similarity, higher = closer
```

### `VectorStore` interface

| Method | Description |
|--------|-------------|
| `add(id, vector, metadata)` | Adds or replaces a record by id. |
| `addAll(records)` | Adds or replaces a list of `VectorRecord`. |
| `get(id)` | Returns the `VectorRecord`, or `null`. |
| `delete(id)` | Removes id; returns `true` if it existed. |
| `search(query, k)` | Top `k` records by descending similarity. |
| `searchWhere(query, k, filter)` | Top `k` among records for which the callable `filter(record)` is true (in-process only). |
| `searchFilter(query, k, criteria)` | Top `k` among records matching a portable dict `criteria` (pushed down server-side by external backends). |
| `count()` | Number of stored records. |
| `clear()` | Removes everything. |

`searchWhere` takes an arbitrary callable and runs in process:

```gb
let hits = store.searchWhere(queryVector, 5, func(any rec): bool {
    return (rec as vectorstore.VectorRecord).metadata["lang"] == "en";
});
```

`searchFilter` takes a portable dict of criteria that external backends can push
down to the database. A scalar value means equality; a nested operator dict
supports `eq`, `ne`, `gt`, `gte`, `lt`, `lte`, and `in`. Multiple keys are ANDed.

```gb
/* lang == "en" AND year >= 2020 */
let hits = store.searchFilter(queryVector, 5, {"lang": "en", "year": {"gte": 2020}});
```

A persistent store is a drop-in replacement:

```gb
import db;
import vectorstore;

let conn  = db.connect("sqlite", "vectors.db");
let store = vectorstore.SqliteVectorStore(conn);
store.add("doc-1", embedding, {"text": "..."});
let hits = store.search(queryVector, 5);
```

The exported helper `vectorstore.score(metric, a, b)` computes a single
similarity score between two vectors.

### Postgres with pgvector

`PgVectorStore` is a production-scale backend using the
[pgvector](https://github.com/pgvector/pgvector) extension for real
approximate-nearest-neighbour search. It rides on the `db` module (no new
dependency) and is a drop-in `VectorStore`.

```gb
import db;
import vectorstore;

let conn  = db.connect("postgres", dsn);
let store = vectorstore.PgVectorStore(conn, "items", 1536);   /* dimension required */
store.add("doc-1", embedding, {"source": "handbook", "year": 2024});

let hits = store.searchFilter(queryVector, 5, {"source": "handbook"});
```

On construction it runs `CREATE EXTENSION IF NOT EXISTS vector`, creates the
table with a typed `vector(D)` column and a `jsonb` metadata column, and builds a
metric-matched HNSW index (`vector_cosine_ops` / `vector_l2_ops` /
`vector_ip_ops`). Searches use the index-backed distance operator (`<=>` / `<->`
/ `<#>`) with `ORDER BY embedding <op> query LIMIT k`, and `searchFilter` pushes
the criteria down to a SQL `WHERE` over the `jsonb` metadata (containment for
equality/`in`, numeric casts for ranges). The dimension is fixed at table
creation, so pass it to the constructor. The Postgres server must have the
pgvector extension available.

### In-process HNSW

`HnswVectorStore` gives sublinear approximate-nearest-neighbour search in memory,
with no external service. It is the middle ground between the brute-force
`MemoryVectorStore` (exact but O(n)) and a database backend: ideal when you have
more vectors than brute force handles comfortably but do not want to run
Postgres.

```gb
import vectorstore;

let store = vectorstore.HnswVectorStore("cosine");   /* or "dot" / "euclidean" */
store.add("doc-1", embedding, {"text": "..."});
let hits = store.search(queryVector, 5);
```

Results are approximate: tune recall versus speed with the constructor's `m`
(graph degree, default 16) and `efSearch` (search breadth, default 20). The index
holds the vectors; metadata is kept alongside in memory. `searchFilter` and
`searchWhere` over-fetch from the index and then filter, so a very selective
filter may return fewer than `k` hits; raise `k` or widen `efSearch` if needed.
The store is not persistent; rebuild it from your source data on startup, or use
`PgVectorStore` when you need durability.

## `rag`

`rag` turns documents into retrievable, prompt-ready context. It is built on a
small `Embedder` interface so it is not tied to any one provider:

```gb
interface Embedder { func embed(string text): list<any>; }
```

`LlmEmbedder` adapts an `llm` client to that interface; the options dict carries
the embedding model.

```gb
import db;
import llm;
import rag;
import vectorstore;

let store    = vectorstore.MemoryVectorStore();
let embedder = rag.LlmEmbedder(
    llm.client({"provider": "openai", "apiKey": key}),
    {"model": "text-embedding-3-small"}
);

rag.index(store, embedder, "handbook", longText, {"source": "handbook"}, {});

let hits   = rag.retrieve(store, embedder, "how do I reset my password?", 4);
let prompt = "Answer using only this context:\n" + rag.context(hits, {})
           + "\n\nQuestion: how do I reset my password?";
let answer = llm.client({"provider": "openai", "apiKey": key})
    .chat([{"role": "user", "content": prompt}], {"model": "gpt-4o-mini"})["content"];
```

`LocalEmbedder` is the offline alternative: it embeds with a local ONNX
sentence-transformer model (see [Local Models](25-onnx-transformers.md)) instead
of an API, so the whole index/retrieve loop runs on-device. It is a drop-in
`Embedder`, so nothing else changes:

```gb
import rag;
import vectorstore;

let embedder = rag.LocalEmbedder("./models/all-MiniLM-L6-v2");   # geblang --allow-onnx
let store    = vectorstore.MemoryVectorStore();
rag.index(store, embedder, "handbook", longText, {}, {});
let hits = rag.retrieve(store, embedder, "how do I reset my password?", 4);
embedder.close();
```

### Functions

| Function | Description |
|----------|-------------|
| `chunk(text, opts)` | Splits text into overlapping chunks; returns `list<string>`. |
| `index(store, embedder, docId, text, metadata, opts)` | Chunks, embeds, and stores a document. Returns the number of chunks. |
| `retrieve(store, embedder, query, k)` | Embeds the query and returns the top `k` `SearchHit`s. |
| `context(hits, opts)` | Assembles hits into a prompt-ready block. |

`chunk` options:

| Key | Default | Meaning |
|-----|---------|---------|
| `by` | `"words"` | `"words"`, `"chars"`, or `"paragraphs"`. |
| `size` | 200 words / 1000 chars | Window size for the chosen unit. |
| `overlap` | 40 words / 200 chars | Overlap between consecutive windows (ignored for paragraphs). |

`index` stores each chunk under id `"<docId>#<i>"` and attaches the caller's
metadata plus `text`, `docId`, and `chunk` (the index), so retrieved hits carry
their own source text.

`context` options: `withSources` (default `true`) prefixes each chunk with
`[n] (docId): `; `separator` (default a blank line) joins the chunks. Pass
`{"withSources": false}` for the bare chunk text.

### Testing without a network

`rag` depends only on the `Embedder` interface, so tests can supply a
deterministic stub embedder and avoid any API calls:

```gb
class StubEmbedder implements rag.Embedder {
    func embed(string text): list<any> {
        let v = [];
        for (k in ["cat", "dog", "car"]) {
            if (text.lower().contains(k as string)) { v = v.push(1.0f); }
            else { v = v.push(0.0f); }
        }
        return v;
    }
}
```

## `vecmath`

The float32 similarity kernel underpinning the stores. It scores in native code
rather than the interpreted loop, and accepts vectors as either a list of
numbers or a packed little-endian float32 BLOB (the stored form).

| Function | Description |
|----------|-------------|
| `score(metric, a, b)` | Similarity (higher = closer) between two vectors for `"cosine"` / `"dot"` / `"euclidean"`. |
| `topK(vectors, query, k, metric)` | Ranks `vectors` (a list of lists or float32 blobs) against `query`, returns up to `k` `{index, score}` dicts in descending order. |
| `normalize(vector)` | L2-normalized copy of a vector; pass a list of vectors to normalize each. A zero vector stays zero. |
| `semanticSearch(queries, corpus, k, metric = "cosine")` | The multi-query form of `topK`: for each query vector, the top-`k` corpus matches as `{index, score}`. One ranked list per query. |

```gb
import vecmath;

vecmath.score("cosine", [1.0, 0.0], [1.0, 0.0]);   # 1.0
let hits = vecmath.topK([[1.0, 0.0], [0.0, 1.0]], [1.0, 0.0], 1, "cosine");
hits[0]["index"];                                   # 0

vecmath.normalize([3.0, 4.0]);                      # [0.6, 0.8]

# One brute-force search over a corpus for several queries at once:
let corpus  = [[1.0, 0.0], [0.0, 1.0], [0.7, 0.7]];
let queries = [[1.0, 0.0], [0.0, 1.0]];
let results = vecmath.semanticSearch(queries, corpus, 2);
results[0][0]["index"];                             # 0  (best match for query 0)
```

`semanticSearch` is exact brute-force, a good fit for small to medium corpora;
use an HNSW `vectorstore` for large ones.

`vectorstore.score(metric, a, b)` delegates to `vecmath.score`; you rarely call
`vecmath` directly unless building your own ranking.
