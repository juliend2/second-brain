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
- **Mobile PWA** — served from `cmd/server` alongside the API, installable and
  offline-capable (service worker caches the app shell; the API is never cached).
  Main feature is **read-with-related**: show a node's markdown plus links in/out
  and nearest neighbors via embeddings; `[[wikilinks]]` are tappable (resolved
  via `/api/graph/resolve`).

No CLI (dropped — interactions happen via MCP and the PWA).

## Hosting

ThinkCentre, ~4GB RAM, on the tailnet. Budget RAM carefully: Ollama +
nomic-embed-text (~1–2GB) + Go service + SQLite must all fit.

One process (`cmd/server`) serves both the API and the PWA. For the phone:
`tailscale cert <hostname>` (Let's Encrypt, auto-renewed), then run the server
with `TLS_CERT` / `TLS_KEY` pointing at the emitted files — it serves HTTPS at
`https://<hostname>`. Traffic stays on the tailnet; the cert exists only so the
mobile browser treats it as a secure context (service workers / install require
HTTPS).

## Deployment (ThinkCentre)

The whole brain runs from four Go binaries; there is no runtime besides
Ollama. All binaries read `data/brain.db` / `data/notes` from env vars
(`BRAIN_DB`, `BRAIN_NOTES`), which must point at the same shared paths.

1. **Build on the ThinkCentre** (or cross-compile and copy the four binaries):

   ```sh
   go build -o brain-server ./cmd/server
   go build -o brain-mcp    ./cmd/mcp
   go build -o brain-ingest ./cmd/ingest
   go build -o brain-enrich ./cmd/enrich
   ```

2. **Configure**: copy `.env.tpl` to `.env` and fill in `NOTION_SECRET_KEY`,
   `NOTION_ROOT_PAGE`, `DROPBOX_DIR` (locally synced Dropbox folder) and
   `LLM_API_KEY`. Set `OLLAMA_URL` if Ollama is not on localhost.

3. **Install Ollama** and pull the embedding model:

   ```sh
   curl -fsSL https://ollama.com/install.sh | sh
   ollama pull nomic-embed-text
   ```

4. **First ingest** (crawl Notion, mirror Dropbox once):

   ```sh
   ./brain-ingest notion
   ./brain-ingest dropbox-sync
   ```

5. **Serve** — HTTPS for the phone via a Tailscale cert (auto-renewed by
   tailscaled):

   ```sh
   tailscale cert <hostname>   # emits <hostname>.crt and <hostname>.key
   ```

   Then run the server, e.g. as a systemd unit. Example
   `/etc/systemd/system/brain.service`:

   ```ini
   [Unit]
   After=network-online.target tailscaled.service

   [Service]
   WorkingDirectory=/srv/brain
   EnvironmentFile=/srv/brain/.env
   Environment=TLS_CERT=/srv/brain/<hostname>.crt
   Environment=TLS_KEY=/srv/brain/<hostname>.key
   Environment=LISTEN_ADDR=:443
   ExecStart=/srv/brain/brain-server
   Restart=always

   [Install]
   WantedBy=multi-user.target
   ```

   Install the certs, then `sudo systemctl enable --now brain`. The PWA is now
   at `https://<hostname>` (install it from the phone's browser).

6. **Register the MCP server with opencode** on the same machine (see the
   [Serving](#serving) section for the config snippet). `BRAIN_API_URL` must
   point at the server; if it runs on `:443` with the cert, use
   `https://<hostname>`.

7. **Enrich**: embed everything once, then run the cloud pass; repeat the cloud
   pass after ingests, or schedule it:

   ```sh
   ./brain-enrich embed
   ./brain-enrich cloud
   # optional: every night, after ingest
   ```

   `notion` and `dropbox-watch` are idempotent, so a nightly `cron`/`systemd
   timer` running `brain-ingest` followed by `brain-enrich cloud` keeps the
   corpus fresh.

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
| `GET /api/graph/resolve?title=` | map a wikilink title to a node id |

In addition to `/api/*`, the server serves the PWA (`/`, `/app.js`, `/sw.js`,
`/manifest.webmanifest`, icons).

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
- [x] Serve: mobile PWA (read-with-related; `tailscale cert` for HTTPS)
