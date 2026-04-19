# ui from your input

Type up to 40 characters. An LLM writes the HTML. Click links to go deeper — each click generates a new page. No other input, only clicks.

Powered by [Groq](https://console.groq.com/docs/model/llama-3.1-8b-instant) (`llama-3.1-8b-instant`). ~200 lines of Go, stdlib only.

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

On Railway: new project → deploy from GitHub repo → set `GROQ_API_KEY` env var. The Dockerfile is picked up automatically.

## Limits

- Prompt: max 40 characters (enforced server-side).
- Generation: `max_tokens=10000` per request.
- Global throttle: 1,000,000 tokens / minute across all users combined. When hit, requests get a 429 until the next minute.

## How it works

1. `/` shows a single text input.
2. Submit → `/g?q=<prompt>` asks Groq for raw HTML.
3. The system prompt instructs the model to return links as `<a href="/g?q=...">` so every click generates the next page.
4. The returned HTML is injected into a minimal wrapper and rendered.

That's all.

## License

MIT
