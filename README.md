# ui from your input

Type a wish — a word, a phrase, anything, up to 40 characters. The LLM invents a URL for your wish (`dragons` → `scalesandfire.ring`) and then invents the page that "lives" at that URL. Every link on the page is another fake URL on this generative web; click one and that page gets generated on the spot. After the first wish, no more typing — only clicks.

Default backend is [Vercel AI Gateway](https://vercel.com/docs/ai-gateway) — one key, many providers and models behind an OpenAI-compatible endpoint. Default model is `xai/grok-4-fast-non-reasoning` (fast + cheap); swap in any [listed model](https://vercel.com/ai-gateway/models) via env. ~200 lines of Go, stdlib only.

## Run locally

```sh
export AI_GATEWAY_API_KEY=vckg_...
go run .
# open http://localhost:8080
```

Get a key at <https://vercel.com/dashboard/ai-gateway/api-keys>.

## Deploy (Railway, Fly, anything with Docker)

```sh
docker build -t ui-from-your-input .
docker run -e AI_GATEWAY_API_KEY=vckg_... -p 8080:8080 ui-from-your-input
```

On Railway: new project → deploy from GitHub repo → set `AI_GATEWAY_API_KEY`. The Dockerfile is picked up automatically.

## Configuration

| Env var | Default | Purpose |
| --- | --- | --- |
| `AI_GATEWAY_API_KEY` / `INFERENCE_API_KEY` | _(required)_ | Bearer token sent to the inference endpoint. |
| `INFERENCE_URL` | `https://ai-gateway.vercel.sh/v1/chat/completions` | OpenAI-compatible chat completions endpoint. |
| `INFERENCE_MODEL` | `xai/grok-4-fast-non-reasoning` | Model id passed in the request body. Any AI Gateway model works, e.g. `openai/gpt-5.4-mini`, `anthropic/claude-haiku-4.5`, `google/gemini-2.5-flash`. |
| `MAX_TOKENS` | `4096` | `max_tokens` per request. |
| `TPM_LIMIT` | `1000000` | Global tokens/min cap across all users. Returns 429 when hit. |
| `PORT` | `8080` | HTTP listen port. |

Swap providers by changing `INFERENCE_URL` + `INFERENCE_MODEL` + key — point at Groq, OpenAI, NVIDIA build, a self-hosted vLLM, anything OpenAI-compatible. Initial wish is always capped to 40 characters server-side.

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
