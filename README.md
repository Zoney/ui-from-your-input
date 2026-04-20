# ui from your input

Type a wish — a word, a phrase, anything, up to 40 characters. The LLM invents a URL for your wish (`dragons` → `scalesandfire.ring`) and then invents the page that "lives" at that URL. Every link on the page is another fake URL on this generative web; click one and that page gets generated on the spot. After the first wish, no more typing — only clicks.

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

1. `/` shows a single text input. You type a wish (max 40 chars), e.g. `weather app for dragons`.
2. `/go?wish=…` asks the LLM to invent a short fake URL for that wish (e.g. `wingcast.news/today?mood=stormy`) and redirects the browser to it.
3. Any non-root path is a catch-all: the server feeds the URL (path + query) to Groq as the user message; the system prompt tells the model it's an address on a fake 1990s generative web and to invent a page for it.
4. The model is told to make every link another internal URL like `/domain.tld/path?x=y`, so clicking a link just triggers the catch-all again and generates the next page.
5. Returned HTML is wrapped in a 90s GeoCities-style chrome (marquee, rainbow rule, beveled tables).
6. `/robots.txt` disallows all crawlers so bots don't burn through the TPM cap.

That's all.

## License

MIT
