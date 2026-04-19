# ui from your input

Type up to 40 characters. An LLM writes the HTML. Click links to go deeper — each click generates a new page. No other input, only clicks.

Default backend is [Groq](https://console.groq.com/docs/model/llama-3.1-8b-instant) (`llama-3.1-8b-instant`) — it's fast. Any OpenAI-compatible endpoint works; see env vars below. ~200 lines of Go, stdlib only.

## Run locally

```sh
export GROQ_API_KEY=gsk_...
go run .
# open http://localhost:8080
```

## Deploy (Railway, Fly, anything with Docker)

```sh
docker build -t ui-from-your-input .
docker run -e GROQ_API_KEY=gsk_... -p 8080:8080 ui-from-your-input
```

On Railway: new project → deploy from GitHub repo → set `GROQ_API_KEY`. The Dockerfile is picked up automatically.

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `GROQ_API_KEY` / `INFERENCE_API_KEY` | _(required)_ | Bearer token sent to the inference endpoint. |
| `INFERENCE_URL` | `https://api.groq.com/openai/v1/chat/completions` | OpenAI-compatible chat completions endpoint. |
| `INFERENCE_MODEL` | `llama-3.1-8b-instant` | Model id passed in the request body. |
| `MAX_TOKENS` | `2048` | `max_tokens` per request. Must fit inside the provider's per-request TPM budget. |
| `TPM_LIMIT` | `6000` | Global tokens/min cap across all users (matches Groq's free tier). Returns 429 when hit. |
| `PORT` | `8080` | HTTP listen port. |

Swap providers by changing `INFERENCE_URL` + `INFERENCE_MODEL` + key — e.g. point at OpenRouter, NVIDIA build, a self-hosted vLLM, etc. Bump `MAX_TOKENS` / `TPM_LIMIT` on paid tiers. Prompt is always capped to 40 characters server-side.

## How it works

1. `/` shows a single text input.
2. Submit → `/g?q=<prompt>` asks Groq for raw HTML.
3. The system prompt instructs the model to return links as `<a href="/g?q=...">` so every click generates the next page.
4. The returned HTML is injected into a minimal wrapper and rendered.

That's all.

## License

MIT
