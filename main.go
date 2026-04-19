package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	maxPromptChars = 40
	maxTokens      = 10000
	tokensPerMin   = 1_000_000
	groqURL        = "https://api.groq.com/openai/v1/chat/completions"
	groqModel      = "llama-3.1-8b-instant"
)

const systemPrompt = `You generate HTML UI fragments from a short user prompt.

Output ONLY raw HTML — no markdown fences, no explanations, no <!doctype>, no <html>/<head>/<body> tags. Just the inner body content.

RULES:
- Make it visually interesting and interactive. You may use inline <style> and inline CSS.
- Include clickable links as <a href="/g?q=URL_ENCODED_PROMPT">text</a>. Each q must be a URL-encoded prompt of at most 40 characters describing what the user sees when they click.
- Links are the only way the user can continue — the rendered page cannot accept text input. Offer several interesting paths.
- Avoid <script> tags, forms, and anything that needs a backend other than the /g?q= link pattern.
- Be creative; this is an exploratory, generative UI.`

type rateLimiter struct {
	mu          sync.Mutex
	windowStart time.Time
	used        int
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.windowStart) > time.Minute {
		r.windowStart = time.Now()
		r.used = 0
	}
	return r.used < tokensPerMin
}

func (r *rateLimiter) add(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.windowStart) > time.Minute {
		r.windowStart = time.Now()
		r.used = 0
	}
	r.used += n
}

var limiter = &rateLimiter{windowStart: time.Now()}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqReq struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type groqResp struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func generate(prompt string) (string, error) {
	if !limiter.allow() {
		return "", fmt.Errorf("global rate limit hit (1M tokens/min). try again in a minute.")
	}
	body, _ := json.Marshal(groqReq{
		Model: groqModel,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
	})
	req, _ := http.NewRequest("POST", groqURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("GROQ_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("groq %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var gr groqResp
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", err
	}
	limiter.add(gr.Usage.TotalTokens)
	if len(gr.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return gr.Choices[0].Message.Content, nil
}

var indexTmpl = template.Must(template.New("i").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>ui from your input</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body{font-family:system-ui,sans-serif;max-width:640px;margin:10vh auto;padding:0 20px;color:#111}
  h1{font-weight:500;font-size:28px;margin:0 0 8px}
  p{color:#555;margin:0 0 24px}
  input{font-size:20px;padding:12px 14px;width:100%;box-sizing:border-box;border:1px solid #ccc;border-radius:6px}
  input:focus{outline:none;border-color:#06c}
  small{color:#888;display:block;margin-top:8px}
  a{color:#06c}
</style></head>
<body>
<h1>ui from your input</h1>
<p>Write something (max 40 chars). An LLM writes the UI. Click links to go deeper.</p>
<form action="/g" method="get">
  <input name="q" maxlength="40" autofocus required placeholder="e.g. weather app for dragons">
  <small>no input after this — only clicks. <a href="https://github.com/Zoney/ui-from-your-input">source</a></small>
</form>
</body></html>`))

var pageTmpl = template.Must(template.New("p").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Q}}</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body{font-family:system-ui,sans-serif;max-width:860px;margin:24px auto;padding:0 20px;color:#111}
  .bar{font-size:13px;color:#888;margin-bottom:16px;border-bottom:1px solid #eee;padding-bottom:10px}
  .bar a{color:#06c;text-decoration:none;margin-right:12px}
  a{color:#06c}
</style></head>
<body>
<div class="bar"><a href="/">&larr; new</a><span>&ldquo;{{.Q}}&rdquo;</span></div>
{{.HTML}}
</body></html>`))

var errTmpl = template.Must(template.New("e").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>hmm</title>
<style>body{font-family:system-ui,sans-serif;max-width:640px;margin:10vh auto;padding:0 20px}</style>
</head><body><h1>hmm</h1><p>{{.}}</p><p><a href="/">&larr; back</a></p></body></html>`))

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		indexTmpl.Execute(w, nil)
	})

	http.HandleFunc("/g", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if len([]rune(q)) > maxPromptChars {
			q = string([]rune(q)[:maxPromptChars])
		}
		html, err := generate(q)
		if err != nil {
			w.WriteHeader(http.StatusTooManyRequests)
			errTmpl.Execute(w, err.Error())
			return
		}
		pageTmpl.Execute(w, map[string]any{
			"Q":    q,
			"HTML": template.HTML(html),
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
