# Own the LLM Layer

Right now we depend on `go-llms` for the entire provider abstraction — streaming, tool execution, message format, all of it. It works, but it's a core dependency we don't control and it's doing a lot of heavy lifting for something that's central to what MENACE is.

I'd rather own this. The provider interface, the streaming loop, the tool execution cycle — that should be ours. Not a fork, not a copy. Our own thing, built for what MENACE actually needs.

**Don't copy go-llms.** Build what we need from the provider APIs directly. Anthropic, Google, and OpenAI all have well-documented streaming APIs. Ollama speaks OpenAI-compatible. The surface area isn't that big — it's just specific.

**Things to figure out:**
- What do we actually use from go-llms? Map the real surface area before building anything.
- Provider abstraction shape — one interface or per-provider adapters?
- Streaming model — channels, callbacks, iterators?
- Tool execution loop — go-llms handles the back-and-forth of tool calls automatically. That's the meatiest part to own.
- Migration path — swap it out without breaking everything. The `agent.go` wrapper is thin enough that this might not be terrible.
