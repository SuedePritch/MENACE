# Multi-Language Indexers

The built-in indexer handles TypeScript and JavaScript. Everything else gets regex, which catches top-level functions but misses methods, nested types, impl blocks, decorators — basically anything interesting.

Each new language is self-contained. Add a tree-sitter grammar, write a parser that extracts symbols, register it, write tests. Nothing else changes. Good first contribution if you want to get involved without learning the whole system.

The reference implementation is `code-indexer/parser.go` — it does TS/JS and shows exactly what a parser needs to do.

Languages that would matter most:
- **Go** — clean AST, straightforward
- **Python** — trickier with indentation but tree-sitter handles it
- **Rust** — impl blocks and traits make it interesting
- **Java/Kotlin** — verbose but well-structured

The external indexer protocol also works — any binary that speaks JSON over the CLI can be an indexer without touching Go code at all. See `internal/indexer/external.go`.

**Pick a language you know and go.**
