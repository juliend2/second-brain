# data-agent

Aggregator and repository for a personal "second brain", built on top of the
existing [personal-search-engine](personal-search-engine/).

## Concept

Everything becomes a **node**: `(source, id, markdown, metadata, timestamps)`.
Nodes are mirrored as markdown into a local, git-versioned store (the working
copy — the web is the source of truth). Relationships between nodes form a
graph. The LLM enriches nodes automatically.

```
ingest → store → enrich → serve
```

## Sources

- **Notion** — via the `ingest notion` command. Depth-first search over child
  pages (same strategy as the legacy personal-search-engine crawler), title
  heading prefixed to the markdown. Tracks Notion's `last_edited_time` in node
  metadata so unchanged pages are skipped on re-sync.
- **Dropbox** — via the official Dropbox client, which syncs to a local folder
  (`~/Dropbox`); `ingest dropbox-watch` mirrors it with fsnotify, `ingest
  dropbox-sync` does a one-shot sync (with pruning of removed files).

Mobile input does **not** go through a capture inbox: all input from mobile goes
through the Notion mobile app.

## Data model

- Markdown store: git-versioned folder of node markdown files.
- SQLite: metadata, edges, embeddings (float32 blob, cosine in memory), status.
- SQLite FTS5: full-text search index, rebuilt from the store (`RebuildIndex()`).

## Enrichment (hybrid)

- **Local embeddings** — Ollama + `nomic-embed-text` on the host machine. Keeps
  indexing/retrieval and dedup on the machine.
- **Cloud LLM** — used only for semantic work, one call per new node:
  - tags
  - `[[wikilinks]]` candidates, written inline in the markdown (portable to
    Obsidian, parseable back into the edge table)
  - one-line summary
  - **"connects to"** edges — labeled edges with a short reason explaining
    *why* two nodes connect; surfaced in the UI.
- Auto-applied by default; audit occasionally.

Run with `enrich embed` (local embeddings for every node missing one) and
`enrich cloud` (one cloud LLM call per draft node; `enrich all` does both).
Concurrency via `ENRICH_EMBED_WORKERS` / `ENRICH_WORKERS`.

## Serving

One HTTP API (`/api/nodes`, `/api/search`, `/api/graph`) behind three clients:

- **MCP server** — thin wrapper around the API exposing tools to LLM clients
  (opencode today, others later). Served over stdio (`cmd/mcp`); register it
  with opencode running on the same machine:
  ```json
  {
    "mcp": {
      "brain": {
        "type": "local",
        "command": ["/path/to/brain-mcp"],
        "environment": { "BRAIN_API_URL": "http://127.0.0.1:8080" }
      }
    }
  }
  ```
  Tools: `search_corpus`, `get_node`, `related` (embedding neighbors), `find_path`
  (link-graph shortest path), `create_note` (`[[wikilinks]]` become edges).
- **Mobile PWA** — served over HTTPS via a Tailscale tailnet certificate
  (`machine.tailnet.ts.net`); mobile-first, installable. Main feature is
  **read-with-related**: show a node's markdown plus links in/out and nearest
  neighbors via embeddings.

No CLI (dropped — interactions happen via MCP and the PWA).

## Hosting

ThinkCentre, ~4GB RAM, on the tailnet. Budget RAM carefully: Ollama +
nomic-embed-text (~1–2GB) + Go service + SQLite must all fit.

## API

One HTTP service (`cmd/server`, run with `LISTEN_ADDR` defaulting to `:8080`).
Reachable over the tailnet only; no built-in auth (the Tailscale identity gates
access). Node ids contain `:` and `/` and must be URL-escaped in paths.

| Endpoint | Description |
| --- | --- |
| `GET /api/health` | liveness |
| `GET /api/nodes[?source=&status=&limit=&offset=]` | list (metadata only) |
| `GET /api/nodes/{id}` | node with markdown |
| `PUT /api/nodes/{id}` | upsert; `[[wikilinks]]` become graph edges |
| `DELETE /api/nodes/{id}` | remove node + edges + mirror file |
| `GET /api/search?q=&limit=` | FTS5 full-text search (title + content, snippets) |
| `GET /api/graph/neighbors/{id}` | edges in/out with resolved titles |
| `GET /api/graph/related/{id}?k=` | nearest neighbors via embeddings |
| `GET /api/graph/path/{from}/{to}` | shortest path between two nodes |

Search is SQLite FTS5 (kept in sync by the store, rebuilt with
`RebuildIndex()`), so bleve is not needed.

## TODO

- [ ] Cross-cluster pass: cluster nodes by similarity, ask the LLM to propose
      synthesis/idea notes that bridge clusters ("forming new ideas and
      intuitions"). Nice-to-have, deferred.
- [x] Ingest: Notion crawler (`ingest notion`)
- [x] Ingest: Dropbox watcher + one-shot sync (`ingest dropbox-watch` /
      `ingest dropbox-sync`)
- [x] Store: canonical markdown mirror + SQLite metadata + node API
      (`internal/store`)
- [x] Serve: HTTP API (`cmd/server`, `internal/server`)
- [ ] Ingest: parse binary files from Dropbox (pdf, docx) instead of skipping
      them
- [ ] Ingest: handle Notion pages inside child databases (currently only
      child pages are traversed)
- [x] Enrich: local embeddings (Ollama + nomic-embed-text, `enrich embed`)
- [x] Enrich: cloud pass (tags / wikilinks / summary / connects-to, `enrich cloud`)
- [x] Serve: MCP server (opencode on a tailnet machine → stdio, no Funnel)
- [ ] Serve: mobile PWA (read-with-related)
