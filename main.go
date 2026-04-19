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
	groqURL        = "https://api.groq.com/openai/v1/chat/completions"
	groqModel      = "llama-3.1-8b-instant"
)

var (
	maxTokens    = envInt("MAX_TOKENS", 2048)
	tokensPerMin = envInt("TPM_LIMIT", 6000)
)

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return d
}

const systemPrompt = `You generate HTML UI fragments from a short user prompt.

Output ONLY raw HTML — no markdown fences, no explanations, no <!doctype>, no <html>/<head>/<body> tags. Just the inner body content.

STYLE — match a 1990s GeoCities-era personal website:
- Tone: enthusiastic, exclamatory, ALL CAPS OK, lots of ~*~sparkles~*~.
- Fonts: "Times New Roman" or "Comic Sans MS". Bright colors on dark or garish backgrounds.
- Use <marquee>, <center>, <blink> (or a .blink class), tiled bevelled <table> layouts, rainbow <hr>, and <font color> if you like.
- Use ASCII art and emoji/unicode symbols (&#9733; &#128187; &#9829;) instead of image URLs — never reference external images.
- Embed a small inline <style> block that fits the 90s theme.

INTERACTION:
- Clickable links use <a href="/g?q=URL_ENCODED_PROMPT">text</a>. Each q must be a URL-encoded prompt of at most 40 characters describing what the user sees when they click.
- Links are the only way the user can continue — the rendered page cannot accept text input. Offer several interesting paths.
- Avoid <script> tags, <form>s, and external assets. Stick to the /g?q= link pattern.
- Be creative; this is an exploratory, generative hypertext experience.`

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
		return "", fmt.Errorf("global rate limit hit (%d tokens/min). try again in a minute.", tokensPerMin)
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
<html><head><meta charset="utf-8"><title>~ UI FROM YOUR INPUT ~ :: Welcome!!! ::</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  @keyframes blink{50%{visibility:hidden}}
  @keyframes rainbow{0%{color:#f00}16%{color:#ff0}33%{color:#0f0}50%{color:#0ff}66%{color:#00f}83%{color:#f0f}100%{color:#f00}}
  body{
    font-family:"Times New Roman",Times,serif;
    background:#000 url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='40' height='40'><text x='0' y='20' font-size='18' fill='%23ffff00'>*</text><text x='20' y='36' font-size='14' fill='%2300ffff'>.</text></svg>");
    color:#ff0;
    text-align:center;
    margin:0;padding:0;
  }
  a{color:#0ff}
  a:visited{color:#f0f}
  .blink{animation:blink 1s step-end infinite}
  .rainbow{animation:rainbow 2s linear infinite;font-weight:bold}
  h1{font-size:48px;margin:10px 0;color:#f0f;text-shadow:3px 3px 0 #0ff}
  marquee{background:#00f;color:#ff0;font-family:"Comic Sans MS",cursive;padding:4px;border:2px ridge #f0f}
  table.frame{margin:12px auto;background:#808080;border:4px outset #c0c0c0;padding:8px}
  table.content{background:#000080;color:#fff;border:3px inset #000;padding:10px;width:560px;max-width:95vw}
  hr.rainbow{height:6px;border:0;background:linear-gradient(90deg,red,orange,yellow,green,cyan,blue,magenta,red)}
  input[name=q]{
    font-family:"Comic Sans MS",cursive;font-size:18px;padding:6px;width:80%;
    background:#fff;color:#000;border:3px inset #c0c0c0
  }
  button{
    font-family:"Comic Sans MS",cursive;font-size:16px;padding:6px 14px;
    background:#c0c0c0;color:#000;border:3px outset #fff;cursor:pointer;margin-top:8px
  }
  button:active{border-style:inset}
  .construction{font-family:monospace;color:#ff0;background:#000;padding:6px;border:2px dashed #ff0;display:inline-block;margin:10px 0}
  .counter{font-family:"Courier New",monospace;background:#000;color:#0f0;border:2px inset #444;padding:2px 8px;letter-spacing:4px}
  .webring{font-size:12px;color:#ccc;margin-top:16px}
  .webring a{color:#ff0}
  .badges{margin:10px 0}
  .badges span{display:inline-block;background:#000;color:#0f0;border:2px solid #0f0;padding:2px 6px;font-family:monospace;font-size:10px;margin:2px}
</style></head>
<body>
<marquee scrollamount="8">&#9733; &#9733; &#9733; WELCOME TO MY HOMEPAGE &#9733; &#9733; &#9733; YOU ARE VISITOR #000001337 &#9733; &#9733; &#9733; PLEASE SIGN MY GUESTBOOK &#9733; &#9733; &#9733; BEST VIEWED IN NETSCAPE NAVIGATOR 4.0 AT 800x600 &#9733; &#9733; &#9733;</marquee>

<table class="frame"><tr><td>
<table class="content"><tr><td>

<h1>~ UI FROM YOUR INPUT ~</h1>
<p class="rainbow">&laquo;&laquo; A G E N E R A T I V E H Y P E R T E X T E X P E R I E N C E &raquo;&raquo;</p>

<hr class="rainbow">

<p><b>Greetings, cybernaut!</b> &#128187;<br>
Type up to <span class="blink">40 characters</span> below.<br>
A <i>real artificial intelligence</i> will craft your HTML page!!<br>
Then <b>CLICK THE LINKS</b> to dive deeper into the web-of-the-future.</p>

<div class="construction">
[!] UNDER ETERNAL CONSTRUCTION [!]<br>
&lt;&lt;&lt; &gt;&gt;&gt;
</div>

<form action="/g" method="get">
  <p><input name="q" maxlength="40" autofocus required placeholder="type ur wish, max 40 chars"></p>
  <p><button type="submit">&gt;&gt; ENTER THE PAGE &lt;&lt;</button></p>
</form>

<hr class="rainbow">

<p>Visitors since 1997:<br><span class="counter">0000042</span></p>

<div class="badges">
  <span>MADE WITH NOTEPAD</span>
  <span>HTML 3.2</span>
  <span>NETSCAPE NOW!</span>
  <span>POWERED BY GROQ</span>
</div>

<p class="webring">
&laquo; <a href="/">PREV</a> &bull; <a href="https://github.com/Zoney/ui-from-your-input">[ SOURCE ]</a> &bull; <a href="/">NEXT</a> &raquo;<br>
part of the <b class="rainbow">AI-GENERATED WEB RING</b>
</p>

<p style="font-size:10px;color:#aaa">&copy; MCMXCVII &mdash; no rights reserved &mdash; <span class="blink">*</span> e-mail the webmaster <span class="blink">*</span></p>

</td></tr></table>
</td></tr></table>
</body></html>`))

var pageTmpl = template.Must(template.New("p").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>~ {{.Q}} ~ :: UIFYI ::</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  @keyframes blink{50%{visibility:hidden}}
  @keyframes rainbow{0%{color:#f00}16%{color:#ff0}33%{color:#0f0}50%{color:#0ff}66%{color:#00f}83%{color:#f0f}100%{color:#f00}}
  html,body{margin:0;padding:0}
  body{
    font-family:"Times New Roman",Times,serif;
    background:#000 url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='40' height='40'><text x='0' y='20' font-size='18' fill='%23ffff00'>*</text><text x='20' y='36' font-size='14' fill='%2300ffff'>.</text></svg>");
    color:#ff0;
  }
  a{color:#0ff}
  a:visited{color:#f0f}
  h1,h2,h3{color:#f0f;text-shadow:2px 2px 0 #0ff}
  .blink{animation:blink 1s step-end infinite}
  .rainbow{animation:rainbow 2s linear infinite;font-weight:bold}
  hr{height:6px;border:0;background:linear-gradient(90deg,red,orange,yellow,green,cyan,blue,magenta,red)}
  marquee{background:#00f;color:#ff0;font-family:"Comic Sans MS",cursive;padding:4px;border:2px ridge #f0f}
  table.frame{margin:12px auto;background:#808080;border:4px outset #c0c0c0;padding:8px;max-width:95vw}
  td.content{background:#000080;color:#fff;border:3px inset #000;padding:14px;min-width:min(760px,90vw)}
  td.content a{color:#0ff}
  .navbar{text-align:center;font-family:"Comic Sans MS",cursive;background:#c0c0c0;color:#000;border:3px outset #fff;padding:4px;margin-bottom:10px}
  .navbar a{color:#00f;text-decoration:none;margin:0 8px}
  .construction{font-family:monospace;color:#ff0;background:#000;padding:6px;border:2px dashed #ff0;display:inline-block;margin:10px 0;text-align:center}
  .footer{text-align:center;font-size:11px;color:#aaa;margin-top:14px;border-top:1px dotted #888;padding-top:8px}
</style></head>
<body>
<marquee scrollamount="8">&#9733; NOW VIEWING: &laquo;{{.Q}}&raquo; &#9733; CLICK THE LINKS TO EXPLORE &#9733; PART OF THE AI-GENERATED WEB RING &#9733; BEST VIEWED IN NETSCAPE NAVIGATOR 4.0 &#9733;</marquee>

<table class="frame"><tr><td>
<div class="navbar">
  <a href="/">&#127968; HOME</a> &bull;
  <a href="/">&larr; NEW WISH</a> &bull;
  <a href="https://github.com/Zoney/ui-from-your-input">[ SOURCE ]</a>
</div>
<table><tr><td class="content">

{{.HTML}}

<div class="footer">
<hr>
<span class="blink">*</span> <span class="rainbow">~ UI FROM YOUR INPUT ~</span> <span class="blink">*</span><br>
&copy; MCMXCVII &mdash; page freshly generated by a <b>real A.I.</b> &mdash; sign the <a href="/g?q=guestbook">guestbook</a>!
</div>

</td></tr></table>
</td></tr></table>
</body></html>`))

var errTmpl = template.Must(template.New("e").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>!! ERROR !! :: UIFYI ::</title>
<style>
  body{font-family:"Times New Roman",Times,serif;background:#000;color:#ff0;text-align:center;padding:40px}
  h1{color:#f00;font-size:40px;text-shadow:3px 3px 0 #ff0}
  a{color:#0ff}
  .box{display:inline-block;background:#800000;color:#fff;border:4px ridge #ff0;padding:20px;font-family:"Comic Sans MS",cursive}
</style></head><body>
<h1>&#9888; OOPSIE &#9888;</h1>
<div class="box">
<p>{{.}}</p>
<p><a href="/">&laquo; back to the HOMEPAGE &raquo;</a></p>
</div>
</body></html>`))

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
