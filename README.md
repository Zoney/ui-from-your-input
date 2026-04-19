# ui from your input

Type up to 40 characters. An LLM writes the HTML. Click links to go deeper — each click generates a new page. No other input, only clicks.

Default backend is the [NVIDIA API Catalog](https://build.nvidia.com/) (`nvidia/llama-3.1-nemotron-nano-vl-8b-v1`). Any OpenAI-compatible endpoint works — see env vars below. ~200 lines of Go, stdlib only.

## Run locally

```sh
export NVIDIA_API_KEY=nvapi-...
go run .
# open http://localhost:8080
```

Get a key at <https://build.nvidia.com/> (free credits).

## Deploy (Railway, Fly, anything with Docker)

```sh
docker build -t ui-from-your-input .
docker run -e NVIDIA_API_KEY=nvapi-... -p 8080:8080 ui-from-your-input
```

On Railway: new project → deploy from GitHub repo → set `NVIDIA_API_KEY`. The Dockerfile is picked up automatically.

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `NVIDIA_API_KEY` / `INFERENCE_API_KEY` | _(required)_ | Bearer token sent to the inference endpoint. |
| `INFERENCE_URL` | `https://integrate.api.nvidia.com/v1/chat/completions` | OpenAI-compatible chat completions endpoint. |
| `INFERENCE_MODEL` | `nvidia/llama-3.1-nemotron-nano-vl-8b-v1` | Model id passed in the request body. |
| `MAX_TOKENS` | `4096` | `max_tokens` per request. |
| `TPM_LIMIT` | `1000000` | Global tokens/min cap across all users. Returns 429 when hit. |
| `PORT` | `8080` | HTTP listen port. |

Swap providers by changing `INFERENCE_URL` + `INFERENCE_MODEL` + key — e.g. point it at Groq, OpenRouter, a self-hosted vLLM, or your own NIM container. Prompt is always capped to 40 characters server-side.

## How it works

1. `/` shows a single text input.
2. Submit → `/g?q=<prompt>` asks Groq for raw HTML.
3. The system prompt instructs the model to return links as `<a href="/g?q=...">` so every click generates the next page.
4. The returned HTML is injected into a minimal wrapper and rendered.

That's all.

## License

MIT
