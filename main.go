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
	maxPromptChars = 40  // initial seed URL length
	maxURLChars    = 400 // upper bound on any generated URL we'll send to the model
)

var (
	apiURL       = envStr("INFERENCE_URL", "https://api.groq.com/openai/v1/chat/completions")
	apiModel     = envStr("INFERENCE_MODEL", "llama-3.1-8b-instant")
	apiKey       = firstNonEmpty(os.Getenv("GROQ_API_KEY"), os.Getenv("INFERENCE_API_KEY"), os.Getenv("NVIDIA_API_KEY"))
	maxTokens    = envInt("MAX_TOKENS", 2048)
	tokensPerMin = envInt("TPM_LIMIT", 6000)
)

func envStr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return d
}

const urlSystemPrompt = `You invent a fake URL for a fictional 1990s GeoCities-style website that matches the user's wish.

Output EXACTLY one URL, nothing else — no quotes, no prose, no leading slash, no scheme (no http://).

Format: domain.tld[/path[/subpath]][?key=value]
- First segment must be a domain ending in a TLD. Real (.com .net .org) or fictional (.zone .ring .fun .wiki .news .biz .pixel) — be creative.
- Total length under 60 characters.
- Evocative of the wish. Use lowercase, ASCII only, no spaces, no emoji.

EXAMPLES
wish: dragons
output: scalesandfire.ring

wish: weather app for dragons
output: wingcast.news/today?mood=stormy

wish: random screen selector
output: pick-a-pixel.zone/roulette?seed=42

wish: a bookstore
output: inkwell.biz/fiction?sort=new`

const systemPrompt = `You generate an HTML page for a simulated URL on a 1990s-style generative web.

USER MESSAGE: a URL of the form  domain.tld/path/subpath?key=value
- The first segment is the "domain" and MUST end in a TLD — real (.com .net .org) or fictional (.zone .ring .fun .wiki .news .biz .pixel are all fair game).
- Path, sub-paths, and query params describe what this specific page is about. Invent plausible content that lives at exactly that address.
- If there is no path beyond the domain, treat it as the site's home page and tease what's inside.

OUTPUT: ONLY raw HTML — body content. No markdown fences, no explanations, no <!doctype>, no <html>/<head>/<body> tags.

STYLE — 1990s GeoCities personal website:
- Enthusiastic, exclamatory tone. ALL CAPS OK. ~*~sparkles~*~.
- "Times New Roman" or "Comic Sans MS". Bright colors on dark/garish backgrounds.
- Use <marquee>, <center>, <blink> (or a .blink class), bevelled <table> layouts, rainbow <hr>, <font color>.
- Use ASCII art and emoji/unicode (&#9733; &#128187; &#9829;) instead of images — NEVER reference external image URLs.
- Small inline <style> is fine.

LINKS — the only way the user can continue:
- Every link MUST be an internal fake URL starting with "/" and whose first segment is a domain-with-TLD, e.g.  <a href="/dragons.news/articles/fire?heat=1000">
- Prefer keeping same-site links on the CURRENT domain. Link to NEW fake domains sometimes for cross-site jumps.
- Query params are welcome and will change what the clicked page looks like — use them (?sort=new, ?year=2026, ?color=red).
- NO external URLs, NO <script>, NO <form>, NO external assets.
- Offer several distinct paths to explore.`

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

type chatReq struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type chatResp struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func handleGenerated(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimPrefix(r.URL.Path, "/")
	if r.URL.RawQuery != "" {
		addr += "?" + r.URL.RawQuery
	}
	if addr == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if len(addr) > maxURLChars {
		addr = addr[:maxURLChars]
	}
	html, err := generate(addr)
	if err != nil {
		w.WriteHeader(http.StatusTooManyRequests)
		errTmpl.Execute(w, err.Error())
		return
	}
	pageTmpl.Execute(w, map[string]any{
		"URL":  addr,
		"HTML": template.HTML(html),
	})
}

func generate(prompt string) (string, error) {
	return callModel(systemPrompt, prompt, maxTokens)
}

func callModel(sys, user string, mt int) (string, error) {
	if !limiter.allow() {
		return "", fmt.Errorf("global rate limit hit (%d tokens/min). try again in a minute.", tokensPerMin)
	}
	body, _ := json.Marshal(chatReq{
		Model: apiModel,
		Messages: []message{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		MaxTokens: mt,
	})
	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("inference %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var cr chatResp
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", err
	}
	limiter.add(cr.Usage.TotalTokens)
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return cr.Choices[0].Message.Content, nil
}

func inventURL(wish string) (string, error) {
	raw, err := callModel(urlSystemPrompt, wish, 64)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, '\n'); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.Trim(raw, "`\"' \t")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimLeft(raw, "/")
	if i := strings.IndexAny(raw, " \t\r"); i >= 0 {
		raw = raw[:i]
	}
	if len(raw) > 100 {
		raw = raw[:100]
	}
	if raw == "" || !strings.ContainsRune(strings.SplitN(raw, "/", 2)[0], '.') {
		return "", fmt.Errorf("couldn't invent a URL from that wish")
	}
	return raw, nil
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
  input[name=wish]{
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
Type a wish &mdash; up to <span class="blink">40 characters</span>.<br>
A <i>real artificial intelligence</i> will invent a URL for it,<br>
then invent the page that lives at that URL!!<br>
After that &mdash; just <b>CLICK THE LINKS</b> to browse the web-that-never-was.</p>

<div class="construction">
[!] TRY: dragons &bull; weather app for dragons &bull; a bookstore [!]
</div>

<form action="/go" method="get">
  <p><input name="wish" maxlength="40" autofocus required placeholder="type ur wish, max 40 chars" spellcheck="false"></p>
  <p><button type="submit">&gt;&gt; INVENT MY URL &lt;&lt;</button></p>
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
<html><head><meta charset="utf-8"><title>~ {{.URL}} ~ :: UIFYI ::</title>
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
<marquee scrollamount="8">&#9733; NOW VIEWING: &laquo;/{{.URL}}&raquo; &#9733; CLICK THE LINKS TO EXPLORE &#9733; PART OF THE AI-GENERATED WEB RING &#9733; BEST VIEWED IN NETSCAPE NAVIGATOR 4.0 &#9733;</marquee>

<table class="frame"><tr><td>
<div class="navbar">
  <a href="/">&#127968; HOME</a> &bull;
  <span style="font-family:'Courier New',monospace;background:#fff;color:#000;padding:2px 6px;border:2px inset #808080">http://{{.URL}}</span> &bull;
  <a href="/">&larr; NEW WISH</a> &bull;
  <a href="https://github.com/Zoney/ui-from-your-input">[ SOURCE ]</a>
</div>
<table><tr><td class="content">

{{.HTML}}

<div class="footer">
<hr>
<span class="blink">*</span> <span class="rainbow">~ UI FROM YOUR INPUT ~</span> <span class="blink">*</span><br>
&copy; MCMXCVII &mdash; page freshly generated by a <b>real A.I.</b> &mdash; sign the <a href="/homeworld.zone/guestbook">guestbook</a>!
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
		switch r.URL.Path {
		case "/":
			indexTmpl.Execute(w, nil)
			return
		case "/favicon.ico":
			http.NotFound(w, r)
			return
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "User-agent: *\nDisallow: /\n")
			return
		case "/go":
			wish := strings.TrimSpace(r.URL.Query().Get("wish"))
			if wish == "" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			if rn := []rune(wish); len(rn) > maxPromptChars {
				wish = string(rn[:maxPromptChars])
			}
			target, err := inventURL(wish)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				errTmpl.Execute(w, err.Error())
				return
			}
			http.Redirect(w, r, "/"+target, http.StatusFound)
			return
		}
		handleGenerated(w, r)
	})

	if apiKey == "" {
		log.Println("warn: no API key set (GROQ_API_KEY / INFERENCE_API_KEY) — /g requests will fail")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s  model=%s", port, apiModel)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
