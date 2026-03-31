# Mobile Vibe Coding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an independent mobile page (`/mobile`) to Phosphor with full-screen terminal, virtual key toolbar, and voice input via Gemini 2.5 Flash through OpenRouter.

**Architecture:** New `mobile.html` served at `/mobile` route, reusing existing terminal WebSocket infrastructure. New `POST /api/voice` endpoint proxies audio to OpenRouter API. No changes to desktop UI.

**Tech Stack:** Go 1.23, gorilla/websocket (existing), xterm.js (existing), MediaRecorder API (browser), OpenRouter API (Gemini 2.5 Flash)

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `main.go` | Modify | Add `-openrouter-key` flag, `/mobile` route, `/api/voice` handler, `go:embed mobile.html` |
| `mobile.html` | Create | Complete mobile page: xterm.js terminal + toolbar + voice recording + menu panel |
| `main_test.go` | Modify | Add tests for `/api/voice` endpoint and `/mobile` route |

Note: The spec mentions `static/js/terminal-shared.js` for shared WebSocket logic, but the existing terminal page (`handleTerminalPage`) embeds its JS inline. For consistency and simplicity (single-binary deployment), we'll inline the terminal JS in `mobile.html` too. The shared logic is small (~30 lines of WebSocket setup) and duplicating it avoids build complexity.

---

### Task 1: Backend — Add OpenRouter API Key flag and `/mobile` route

**Files:**
- Modify: `main.go:29-32` (embed directives)
- Modify: `main.go:990-1015` (main function — flags and routes)

- [ ] **Step 1: Write failing test for `/mobile` route**

Add to `main_test.go`:

```go
// --- Mobile ---

func TestHandleMobile(t *testing.T) {
	ts := setupTestServer(t)
	resp := doRequest(t, "GET", ts.URL+"/mobile", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}
```

Also add to `setupTestServer` mux routes:

```go
mux.HandleFunc("GET /mobile", handleMobile)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /root/Phosphor && go test -run TestHandleMobile -v`
Expected: compilation error — `handleMobile` undefined

- [ ] **Step 3: Create minimal `mobile.html` and add route**

Create `mobile.html` with minimal content:

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<title>Phosphor Mobile</title>
</head>
<body>
<h1>Phosphor Mobile</h1>
</body>
</html>
```

Add to `main.go` near line 29 (after existing embed directives):

```go
//go:embed mobile.html
var mobileHTML embed.FS
```

Add handler function (after `handleIndex`):

```go
func handleMobile(w http.ResponseWriter, r *http.Request) {
	data, _ := mobileHTML.ReadFile("mobile.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
```

Add route in `main()` after `mux.HandleFunc("GET /", handleIndex)`:

```go
mux.HandleFunc("GET /mobile", handleMobile)
```

Add `-openrouter-key` flag in `main()` after the `devMode` flag:

```go
openrouterKey := flag.String("openrouter-key", "", "OpenRouter API key for voice recognition (or set OPENROUTER_API_KEY env)")
```

Add global variable to hold the resolved key (near other global vars):

```go
var openrouterAPIKey string
```

After `flag.Parse()`, resolve the key:

```go
openrouterAPIKey = *openrouterKey
if openrouterAPIKey == "" {
	openrouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /root/Phosphor && go test -run TestHandleMobile -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go mobile.html
git commit -m "feat: add /mobile route and OpenRouter API key flag"
```

---

### Task 2: Backend — `/api/voice` endpoint

**Files:**
- Modify: `main.go` (add handler and route)
- Modify: `main_test.go` (add tests)

- [ ] **Step 1: Write failing tests for `/api/voice`**

Add to `main_test.go`:

```go
// --- Voice ---

func TestHandleVoice(t *testing.T) {
	t.Run("no api key returns 501", func(t *testing.T) {
		ts := setupTestServer(t)
		openrouterAPIKey = ""

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("audio", "recording.webm")
		fw.Write([]byte("fake audio data"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/voice", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("expected 501, got %d", resp.StatusCode)
		}
	})

	t.Run("no audio file returns 400", func(t *testing.T) {
		ts := setupTestServer(t)
		openrouterAPIKey = "test-key"

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/voice", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("audio too large returns 413", func(t *testing.T) {
		ts := setupTestServer(t)
		openrouterAPIKey = "test-key"

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("audio", "big.webm")
		// Write 11MB of data
		fw.Write(make([]byte, 11*1024*1024))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/voice", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", resp.StatusCode)
		}
	})
}
```

Also add `handleVoice` route to `setupTestServer`:

```go
mux.HandleFunc("POST /api/voice", handleVoice)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /root/Phosphor && go test -run TestHandleVoice -v`
Expected: compilation error — `handleVoice` undefined

- [ ] **Step 3: Implement `handleVoice`**

Add to `main.go` (after `handleTerminalSessionRename`):

```go
const voiceMaxBytes = 10 * 1024 * 1024 // 10MB

func handleVoice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if openrouterAPIKey == "" {
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]string{"error": "未配置 OpenRouter API Key"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, voiceMaxBytes+1024) // extra for form overhead
	if err := r.ParseMultipartForm(voiceMaxBytes); err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(map[string]string{"error": "音频文件过大，最大 10MB"})
		return
	}

	file, _, err := r.FormFile("audio")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "缺少音频文件"})
		return
	}
	defer file.Close()

	audioData, err := io.ReadAll(io.LimitReader(file, voiceMaxBytes+1))
	if err != nil || int64(len(audioData)) > voiceMaxBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(map[string]string{"error": "音频文件过大，最大 10MB"})
		return
	}

	prompt := r.FormValue("prompt")
	if prompt == "" {
		prompt = "请将这段语音准确转录为文字。这是编程相关的语音命令，请保持原意，修正明显的口误。只返回转录文字，不要添加任何解释。"
	}

	audioBase64 := base64.StdEncoding.EncodeToString(audioData)

	reqBody := map[string]interface{}{
		"model": "google/gemini-2.5-flash",
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "input_audio",
						"input_audio": map[string]string{
							"data":   audioBase64,
							"format": "webm",
						},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "请求序列化失败"})
		return
	}

	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "创建请求失败"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+openrouterAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "OpenRouter 请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "解析响应失败"})
		return
	}

	if result.Error != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "OpenRouter 错误: " + result.Error.Message})
		return
	}

	if len(result.Choices) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "未返回识别结果"})
		return
	}

	text := strings.TrimSpace(result.Choices[0].Message.Content)
	json.NewEncoder(w).Encode(map[string]string{"text": text})
}
```

Add `"encoding/base64"` and `"bytes"` to the imports in `main.go` (bytes is already imported, add base64).

Add route in `main()`:

```go
mux.HandleFunc("POST /api/voice", handleVoice)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /root/Phosphor && go test -run TestHandleVoice -v`
Expected: all 3 subtests PASS

- [ ] **Step 5: Run all existing tests to ensure no regressions**

Run: `cd /root/Phosphor && go test -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: add /api/voice endpoint for speech recognition via OpenRouter"
```

---

### Task 3: Frontend — Mobile page with full-screen terminal

**Files:**
- Modify: `mobile.html` (replace minimal placeholder with full implementation)

This is the largest task. The `mobile.html` page contains all HTML, CSS, and JS inline (matching the project's single-file pattern).

- [ ] **Step 1: Write the complete `mobile.html`**

Replace the placeholder `mobile.html` with the full implementation. The file structure:

```
mobile.html
├── <head>
│   ├── Meta tags (viewport, charset)
│   ├── xterm.js + FitAddon CSS/JS links
│   └── <style> — all CSS (~300 lines)
├── <body>
│   ├── #top-bar — session name, status, +, ☰
│   ├── #terminal-container — xterm.js mount
│   ├── #toolbar — bottom fixed toolbar
│   │   ├── .key-row-collapsed — ESC ↑ ↓ ← → ⏎ ▲
│   │   ├── .key-row-expanded — full keys + Ctrl combos + slash commands + quick replies
│   │   └── .input-row — text input + 🎤 button
│   ├── #menu-overlay + #menu-panel — right-slide menu
│   └── <script> — all JS (~400 lines)
│       ├── Terminal setup (xterm.js + WebSocket)
│       ├── Key button handlers (sendKey, sticky Ctrl)
│       ├── Slash command logic (NO_ARGS_COMMANDS set)
│       ├── Voice recording (MediaRecorder + push-to-talk)
│       ├── Menu panel (sessions, files, settings)
│       └── Input box submit logic
```

Write the complete file content:

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
<title>Phosphor Mobile</title>
<link rel="stylesheet" href="/static/xterm/xterm.css">
<script src="/static/xterm/xterm.js"></script>
<script src="/static/xterm/xterm-addon-fit.js"></script>
<style>
/* === Reset & Base === */
* { margin: 0; padding: 0; box-sizing: border-box; -webkit-tap-highlight-color: transparent; }
:root {
  --bg: #0a0e14; --panel: #161b22; --border: #30363d;
  --green: #00ff41; --blue: #58a6ff; --orange: #f0883e;
  --red: #da3633; --text: #c9d1d9; --text-dim: #8b949e;
  --key-bg: #21262d; --safe-bottom: env(safe-area-inset-bottom, 0px);
}
html, body { height: 100%; overflow: hidden; background: var(--bg); color: var(--text);
  font-family: 'Inter', -apple-system, sans-serif; font-size: 14px; touch-action: manipulation; }

/* === Layout === */
#app { display: flex; flex-direction: column; height: 100%; height: 100dvh; }
#top-bar { background: var(--panel); padding: 8px 12px; display: flex; align-items: center;
  justify-content: space-between; border-bottom: 1px solid var(--border); flex-shrink: 0; z-index: 10; }
#terminal-container { flex: 1; overflow: hidden; }
#toolbar { background: var(--panel); border-top: 1px solid var(--border); padding: 8px 8px;
  padding-bottom: calc(8px + var(--safe-bottom)); flex-shrink: 0; z-index: 10; }

/* === Top Bar === */
.top-left { display: flex; align-items: center; gap: 8px; }
.top-logo { color: var(--green); font-size: 16px; font-weight: bold; }
.session-name { color: var(--text); font-size: 13px; font-family: 'JetBrains Mono', monospace; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); display: inline-block; }
.status-dot.disconnected { background: #d29922; animation: pulse 1.5s infinite; }
.top-right { display: flex; gap: 14px; align-items: center; }
.top-btn { color: var(--text-dim); font-size: 18px; cursor: pointer; padding: 4px; background: none; border: none; }
.top-btn:active { color: var(--text); }

/* === Toolbar Keys === */
.key-row { display: flex; gap: 6px; justify-content: center; margin-bottom: 8px; flex-wrap: wrap; }
.key-btn { background: var(--key-bg); color: var(--text); padding: 8px 12px; border-radius: 6px;
  font-size: 12px; font-family: 'JetBrains Mono', monospace; border: 1px solid var(--border);
  cursor: pointer; user-select: none; white-space: nowrap; }
.key-btn:active { background: #2d333b; }
.key-btn.sticky-active { background: #1f3a1f; color: var(--green); border-color: var(--green); }
.key-btn.expand-btn { background: #0d419d; color: var(--blue); border-color: #1f6feb; }
.key-section-label { color: var(--text-dim); font-size: 9px; text-transform: uppercase; padding-left: 4px;
  margin-bottom: 4px; margin-top: 6px; width: 100%; }
.cmd-btn { background: #1a1e24; color: var(--green); border-color: #238636; font-size: 11px; padding: 7px 10px; }
.cmd-btn:active { background: #238636; }
.reply-btn { background: #1a1e24; color: var(--orange); border-color: #d29922; font-size: 11px; padding: 7px 12px; }
.reply-btn:active { background: #d29922; color: #000; }
.expanded-section { display: none; }
.expanded-section.show { display: block; }

/* === Input Row === */
.input-row { display: flex; gap: 8px; align-items: center; }
#cmd-input { flex: 1; background: #0d1117; border: 1px solid var(--border); border-radius: 20px;
  padding: 10px 16px; color: var(--text); font-size: 13px; font-family: 'JetBrains Mono', monospace;
  outline: none; }
#cmd-input::placeholder { color: var(--text-dim); }
#cmd-input:focus { border-color: var(--blue); }
#mic-btn { background: var(--red); min-width: 42px; height: 42px; border-radius: 50%; border: none;
  display: flex; align-items: center; justify-content: center; font-size: 18px; cursor: pointer;
  transition: transform 0.15s; position: relative; }
#mic-btn:active, #mic-btn.recording { transform: scale(1.2); }
#mic-btn.recording { animation: pulse-red 1s infinite; }
#mic-btn.disabled { background: #484f58; cursor: not-allowed; }
.mic-status { position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%);
  background: var(--panel); color: var(--text); font-size: 11px; padding: 4px 10px;
  border-radius: 4px; white-space: nowrap; margin-bottom: 8px; border: 1px solid var(--border); }

/* === Menu === */
#menu-overlay { display: none; position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 90; }
#menu-overlay.show { display: block; }
#menu-panel { position: fixed; top: 0; right: -300px; width: 280px; height: 100%; background: var(--panel);
  z-index: 100; transition: right 0.25s ease; border-left: 1px solid var(--border); overflow-y: auto; }
#menu-panel.show { right: 0; }
.menu-section { padding: 16px; border-bottom: 1px solid var(--border); }
.menu-section h3 { color: var(--green); font-size: 13px; margin-bottom: 10px; text-transform: uppercase; }
.menu-item { padding: 10px; border-radius: 6px; cursor: pointer; color: var(--text); font-size: 13px;
  display: flex; justify-content: space-between; align-items: center; }
.menu-item:active { background: var(--key-bg); }
.menu-item.active { background: #1f3a1f; border: 1px solid var(--green); }
.menu-item .delete-btn { color: var(--red); font-size: 16px; padding: 2px 6px; }
.file-item { font-family: 'JetBrains Mono', monospace; font-size: 12px; }
.file-item .icon { margin-right: 6px; }
.settings-input { width: 100%; background: #0d1117; border: 1px solid var(--border); border-radius: 6px;
  padding: 8px 10px; color: var(--text); font-size: 12px; font-family: 'JetBrains Mono', monospace; margin-top: 6px; }
.settings-input:focus { outline: none; border-color: var(--blue); }
.settings-label { color: var(--text-dim); font-size: 11px; margin-top: 10px; display: block; }

/* === File Viewer === */
#file-viewer { display: none; position: fixed; inset: 0; background: var(--bg); z-index: 110; flex-direction: column; }
#file-viewer.show { display: flex; }
#file-viewer-header { background: var(--panel); padding: 10px 14px; display: flex; align-items: center;
  justify-content: space-between; border-bottom: 1px solid var(--border); }
#file-viewer-content { flex: 1; overflow: auto; padding: 12px; font-family: 'JetBrains Mono', monospace;
  font-size: 12px; white-space: pre-wrap; color: var(--text); }

/* === Animations === */
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
@keyframes pulse-red { 0%, 100% { box-shadow: 0 0 0 0 rgba(218,54,51,0.6); } 50% { box-shadow: 0 0 0 12px rgba(218,54,51,0); } }
</style>
</head>
<body>
<div id="app">
  <!-- Top Bar -->
  <div id="top-bar">
    <div class="top-left">
      <span class="top-logo">⚡</span>
      <span class="session-name" id="session-name">终端 1</span>
      <span class="status-dot" id="status-dot"></span>
    </div>
    <div class="top-right">
      <button class="top-btn" id="btn-new-session" title="新建终端">＋</button>
      <button class="top-btn" id="btn-menu" title="菜单">☰</button>
    </div>
  </div>

  <!-- Terminal -->
  <div id="terminal-container"></div>

  <!-- Toolbar -->
  <div id="toolbar">
    <!-- Collapsed keys (default) -->
    <div class="key-row" id="keys-collapsed">
      <button class="key-btn" data-key="escape">ESC</button>
      <button class="key-btn" data-key="up">↑</button>
      <button class="key-btn" data-key="down">↓</button>
      <button class="key-btn" data-key="left">←</button>
      <button class="key-btn" data-key="right">→</button>
      <button class="key-btn" data-key="enter">⏎</button>
      <button class="key-btn expand-btn" id="btn-expand">▲</button>
    </div>

    <!-- Expanded section -->
    <div class="expanded-section" id="keys-expanded">
      <!-- Basic keys -->
      <div class="key-row">
        <button class="key-btn" data-key="escape">ESC</button>
        <button class="key-btn" data-key="ctrl" id="btn-ctrl">Ctrl</button>
        <button class="key-btn" data-key="enter">⏎</button>
        <button class="key-btn" data-key="up">↑</button>
        <button class="key-btn" data-key="down">↓</button>
        <button class="key-btn" data-key="left">←</button>
        <button class="key-btn" data-key="right">→</button>
      </div>
      <!-- Ctrl combos -->
      <div class="key-row">
        <button class="key-btn" data-combo="ctrl+c">Ctrl+C</button>
        <button class="key-btn" data-combo="ctrl+z">Ctrl+Z</button>
        <button class="key-btn" data-combo="ctrl+l">Ctrl+L</button>
        <button class="key-btn" data-combo="ctrl+d">Ctrl+D</button>
      </div>
      <!-- Slash commands -->
      <div class="key-section-label">CLAUDE CODE 命令</div>
      <div class="key-row">
        <button class="key-btn cmd-btn" data-cmd="/using-superpowers">/using-superpowers</button>
        <button class="key-btn cmd-btn" data-cmd="/compact">/compact</button>
        <button class="key-btn cmd-btn" data-cmd="/clear">/clear</button>
        <button class="key-btn cmd-btn" data-cmd="/cost">/cost</button>
        <button class="key-btn cmd-btn" data-cmd="/help">/help</button>
        <button class="key-btn cmd-btn" data-cmd="/commit">/commit</button>
        <button class="key-btn cmd-btn" data-cmd="/pr">/pr</button>
        <button class="key-btn cmd-btn" data-cmd="/review">/review</button>
        <button class="key-btn cmd-btn" data-cmd="/fast">/fast</button>
      </div>
      <!-- Quick replies -->
      <div class="key-section-label">快捷回复</div>
      <div class="key-row">
        <button class="key-btn reply-btn" data-reply="y">y</button>
        <button class="key-btn reply-btn" data-reply="n">n</button>
        <button class="key-btn reply-btn" data-reply="yes!">yes!</button>
      </div>
      <!-- Collapse button -->
      <div class="key-row">
        <button class="key-btn expand-btn" id="btn-collapse">▼</button>
      </div>
    </div>

    <!-- Input row -->
    <div class="input-row">
      <input type="text" id="cmd-input" placeholder="输入命令..." autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false">
      <button id="mic-btn">🎤</button>
    </div>
  </div>
</div>

<!-- Menu Overlay -->
<div id="menu-overlay"></div>
<div id="menu-panel">
  <div class="menu-section">
    <h3>终端会话</h3>
    <div id="session-list"></div>
  </div>
  <div class="menu-section">
    <h3>文件浏览</h3>
    <div id="file-list"></div>
  </div>
  <div class="menu-section">
    <h3>设置</h3>
    <label class="settings-label">OpenRouter API Key</label>
    <input type="password" class="settings-input" id="settings-apikey" placeholder="sk-or-...">
    <label class="settings-label">语音 Prompt</label>
    <input type="text" class="settings-input" id="settings-prompt" placeholder="默认: 编程相关语音命令">
  </div>
</div>

<!-- File Viewer -->
<div id="file-viewer">
  <div id="file-viewer-header">
    <span id="file-viewer-name" style="color:var(--blue);font-size:13px;font-family:'JetBrains Mono',monospace;"></span>
    <button class="top-btn" id="btn-close-viewer">✕</button>
  </div>
  <div id="file-viewer-content"></div>
</div>

<script>
// === State ===
let currentSessionId = null;
let ws = null;
let term = null;
let fit = null;
let ctrlActive = false;
let mediaRecorder = null;
let audioChunks = [];
let isRecording = false;
let toolbarExpanded = localStorage.getItem('toolbarExpanded') === 'true';

const NO_ARGS_COMMANDS = new Set(['/clear', '/cost', '/help', '/fast', '/compact']);

// === Terminal Setup ===
function initTerminal() {
  term = new Terminal({
    cursorBlink: true,
    fontSize: 12,
    fontFamily: "'JetBrains Mono', monospace",
    theme: { background: '#0a0e14', foreground: '#c9d1d9', cursor: '#00ff41' },
    scrollback: 5000,
  });
  fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('terminal-container'));
  fit.fit();

  term.onData(function(data) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(new TextEncoder().encode(data));
    }
  });

  window.addEventListener('resize', function() { fit.fit(); sendResize(); });
  new ResizeObserver(function() { fit.fit(); sendResize(); }).observe(document.getElementById('terminal-container'));
}

function sendResize() {
  if (ws && ws.readyState === WebSocket.OPEN && term) {
    ws.send('resize:' + JSON.stringify({ cols: term.cols, rows: term.rows }));
  }
}

// === WebSocket ===
function connectSession(sessionId) {
  if (ws) { ws.onclose = null; ws.close(); }
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = proto + '//' + location.host + '/api/terminal' + (sessionId ? '?session=' + sessionId : '');
  ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';

  ws.onopen = function() {
    setStatus(true);
    sendResize();
  };

  ws.onmessage = function(e) {
    if (typeof e.data === 'string') {
      if (e.data.startsWith('session:')) {
        currentSessionId = e.data.substring(8);
        localStorage.setItem('mobileSessionId', currentSessionId);
        return;
      }
      term.write(e.data);
    } else if (e.data instanceof ArrayBuffer) {
      term.write(new Uint8Array(e.data));
    }
  };

  ws.onclose = function() {
    setStatus(false);
    // Auto-reconnect after 2s
    setTimeout(function() {
      if (currentSessionId) { connectSession(currentSessionId); }
    }, 2000);
  };
}

function setStatus(connected) {
  const dot = document.getElementById('status-dot');
  dot.className = connected ? 'status-dot' : 'status-dot disconnected';
}

// === Key Handlers ===
const KEY_MAP = {
  'escape': '\x1b',
  'enter': '\r',
  'up': '\x1b[A',
  'down': '\x1b[B',
  'right': '\x1b[C',
  'left': '\x1b[D',
};

const COMBO_MAP = {
  'ctrl+c': '\x03',
  'ctrl+z': '\x1a',
  'ctrl+l': '\x0c',
  'ctrl+d': '\x04',
};

function sendToTerminal(data) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(new TextEncoder().encode(data));
  }
}

function handleKeyBtn(e) {
  const key = e.currentTarget.dataset.key;
  if (key === 'ctrl') {
    ctrlActive = !ctrlActive;
    document.getElementById('btn-ctrl').classList.toggle('sticky-active', ctrlActive);
    return;
  }
  if (ctrlActive && key && key.length === 1) {
    // Ctrl + letter
    const code = key.charCodeAt(0) - 96; // a=1, z=26
    sendToTerminal(String.fromCharCode(code));
    ctrlActive = false;
    document.getElementById('btn-ctrl').classList.remove('sticky-active');
    return;
  }
  const seq = KEY_MAP[key];
  if (seq) { sendToTerminal(seq); }
}

function handleCombo(e) {
  const combo = e.currentTarget.dataset.combo;
  const seq = COMBO_MAP[combo];
  if (seq) { sendToTerminal(seq); }
}

function handleSlashCmd(e) {
  const cmd = e.currentTarget.dataset.cmd;
  if (NO_ARGS_COMMANDS.has(cmd)) {
    sendToTerminal(cmd + '\r');
  } else {
    const input = document.getElementById('cmd-input');
    input.value = cmd + ' ';
    input.focus();
  }
}

function handleQuickReply(e) {
  sendToTerminal(e.currentTarget.dataset.reply + '\r');
}

// === Input Box ===
document.getElementById('cmd-input').addEventListener('keydown', function(e) {
  if (e.key === 'Enter') {
    e.preventDefault();
    const text = this.value;
    if (text) {
      sendToTerminal(text + '\r');
      this.value = '';
    }
  }
});

// === Toolbar Expand/Collapse ===
function updateToolbarState() {
  document.getElementById('keys-collapsed').style.display = toolbarExpanded ? 'none' : 'flex';
  document.getElementById('keys-expanded').classList.toggle('show', toolbarExpanded);
  localStorage.setItem('toolbarExpanded', toolbarExpanded);
  // Refit terminal after layout change
  setTimeout(function() { if (fit) fit.fit(); sendResize(); }, 50);
}

document.getElementById('btn-expand').addEventListener('click', function() {
  toolbarExpanded = true; updateToolbarState();
});
document.getElementById('btn-collapse').addEventListener('click', function() {
  toolbarExpanded = false; updateToolbarState();
});

// === Voice Recording ===
const micBtn = document.getElementById('mic-btn');
let micStatus = null;

function showMicStatus(text) {
  if (!micStatus) {
    micStatus = document.createElement('div');
    micStatus.className = 'mic-status';
    micBtn.appendChild(micStatus);
  }
  micStatus.textContent = text;
  micStatus.style.display = 'block';
}

function hideMicStatus() {
  if (micStatus) micStatus.style.display = 'none';
}

async function checkVoiceAvailable() {
  try {
    const resp = await fetch('/api/voice', { method: 'POST' });
    if (resp.status === 501) {
      micBtn.classList.add('disabled');
      return false;
    }
  } catch(e) {}
  return true;
}

micBtn.addEventListener('touchstart', function(e) {
  e.preventDefault();
  if (micBtn.classList.contains('disabled')) return;
  startRecording();
});
micBtn.addEventListener('touchend', function(e) {
  e.preventDefault();
  if (isRecording) stopRecording();
});
micBtn.addEventListener('mousedown', function(e) {
  if (micBtn.classList.contains('disabled')) return;
  startRecording();
});
micBtn.addEventListener('mouseup', function() {
  if (isRecording) stopRecording();
});

async function startRecording() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    audioChunks = [];
    mediaRecorder = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' });
    mediaRecorder.ondataavailable = function(e) {
      if (e.data.size > 0) audioChunks.push(e.data);
    };
    mediaRecorder.start();
    isRecording = true;
    micBtn.classList.add('recording');
    showMicStatus('正在录音...');
  } catch(err) {
    showMicStatus('麦克风权限被拒绝');
    setTimeout(hideMicStatus, 2000);
  }
}

async function stopRecording() {
  if (!mediaRecorder) return;
  isRecording = false;
  micBtn.classList.remove('recording');

  mediaRecorder.onstop = async function() {
    const blob = new Blob(audioChunks, { type: 'audio/webm' });
    mediaRecorder.stream.getTracks().forEach(t => t.stop());
    showMicStatus('识别中...');

    const formData = new FormData();
    formData.append('audio', blob, 'recording.webm');
    const customPrompt = document.getElementById('settings-prompt').value;
    if (customPrompt) formData.append('prompt', customPrompt);

    try {
      const resp = await fetch('/api/voice', { method: 'POST', body: formData });
      const data = await resp.json();
      if (data.text) {
        const input = document.getElementById('cmd-input');
        input.value = input.value + data.text;
        input.focus();
      } else if (data.error) {
        showMicStatus('错误: ' + data.error);
        setTimeout(hideMicStatus, 3000);
        return;
      }
    } catch(err) {
      showMicStatus('网络错误');
      setTimeout(hideMicStatus, 2000);
      return;
    }
    hideMicStatus();
  };
  mediaRecorder.stop();
}

// === Menu ===
const menuOverlay = document.getElementById('menu-overlay');
const menuPanel = document.getElementById('menu-panel');

document.getElementById('btn-menu').addEventListener('click', function() {
  menuOverlay.classList.add('show');
  menuPanel.classList.add('show');
  loadSessions();
  loadFiles('');
});

menuOverlay.addEventListener('click', closeMenu);

function closeMenu() {
  menuOverlay.classList.remove('show');
  menuPanel.classList.remove('show');
}

// --- Sessions ---
async function loadSessions() {
  try {
    const resp = await fetch('/api/terminal/sessions');
    const sessions = await resp.json();
    const list = document.getElementById('session-list');
    list.innerHTML = '';
    sessions.forEach(function(s) {
      const item = document.createElement('div');
      item.className = 'menu-item' + (s.id === currentSessionId ? ' active' : '');
      item.innerHTML = '<span>' + escapeHtml(s.name) + (s.connected ? ' <span style="color:var(--green)">●</span>' : '') + '</span>'
        + '<span class="delete-btn" data-id="' + s.id + '">✕</span>';
      item.addEventListener('click', function(e) {
        if (e.target.classList.contains('delete-btn')) {
          deleteSession(e.target.dataset.id);
          return;
        }
        switchSession(s.id, s.name);
      });
      list.appendChild(item);
    });
  } catch(e) {}
}

function switchSession(id, name) {
  term.clear();
  connectSession(id);
  document.getElementById('session-name').textContent = name;
  closeMenu();
}

async function deleteSession(id) {
  try {
    await fetch('/api/terminal/sessions?id=' + id, { method: 'DELETE' });
    loadSessions();
    if (id === currentSessionId) {
      currentSessionId = null;
      connectSession(null); // creates new
    }
  } catch(e) {}
}

document.getElementById('btn-new-session').addEventListener('click', function() {
  currentSessionId = null;
  term.clear();
  connectSession(null);
});

// --- Files ---
let currentFilePath = '';

async function loadFiles(path) {
  try {
    const resp = await fetch('/api/files?path=' + encodeURIComponent(path));
    const files = await resp.json();
    const list = document.getElementById('file-list');
    list.innerHTML = '';

    if (path) {
      const backItem = document.createElement('div');
      backItem.className = 'menu-item file-item';
      backItem.innerHTML = '<span><span class="icon">📁</span>..</span>';
      backItem.addEventListener('click', function() {
        const parent = path.split('/').slice(0, -1).join('/');
        loadFiles(parent);
      });
      list.appendChild(backItem);
    }

    files.forEach(function(f) {
      const item = document.createElement('div');
      item.className = 'menu-item file-item';
      const icon = f.is_dir ? '📁' : '📄';
      item.innerHTML = '<span><span class="icon">' + icon + '</span>' + escapeHtml(f.name) + '</span>';
      item.addEventListener('click', function() {
        if (f.is_dir) {
          loadFiles(f.path);
        } else {
          viewFile(f.path, f.name);
        }
      });
      list.appendChild(item);
    });
    currentFilePath = path;
  } catch(e) {}
}

async function viewFile(path, name) {
  try {
    const resp = await fetch('/api/read?path=' + encodeURIComponent(path));
    if (!resp.ok) return;
    const data = await resp.json();
    document.getElementById('file-viewer-name').textContent = name;
    document.getElementById('file-viewer-content').textContent = data.content;
    document.getElementById('file-viewer').classList.add('show');
    closeMenu();
  } catch(e) {}
}

document.getElementById('btn-close-viewer').addEventListener('click', function() {
  document.getElementById('file-viewer').classList.remove('show');
});

// === Settings persistence ===
const settingsApiKey = document.getElementById('settings-apikey');
const settingsPrompt = document.getElementById('settings-prompt');

settingsApiKey.value = localStorage.getItem('openrouterKey') || '';
settingsPrompt.value = localStorage.getItem('voicePrompt') || '';

settingsApiKey.addEventListener('change', function() {
  localStorage.setItem('openrouterKey', this.value);
});
settingsPrompt.addEventListener('change', function() {
  localStorage.setItem('voicePrompt', this.value);
});

// === Helpers ===
function escapeHtml(text) {
  const d = document.createElement('div');
  d.textContent = text;
  return d.innerHTML;
}

// === Init ===
(async function() {
  initTerminal();

  // Bind key buttons
  document.querySelectorAll('[data-key]').forEach(function(btn) {
    btn.addEventListener('click', handleKeyBtn);
  });
  document.querySelectorAll('[data-combo]').forEach(function(btn) {
    btn.addEventListener('click', handleCombo);
  });
  document.querySelectorAll('[data-cmd]').forEach(function(btn) {
    btn.addEventListener('click', handleSlashCmd);
  });
  document.querySelectorAll('[data-reply]').forEach(function(btn) {
    btn.addEventListener('click', handleQuickReply);
  });

  updateToolbarState();

  // Restore or create session
  const savedSession = localStorage.getItem('mobileSessionId');
  if (savedSession) {
    connectSession(savedSession);
  } else {
    connectSession(null);
  }

  // Check voice availability
  // (deferred — don't block init)
  setTimeout(checkVoiceAvailable, 1000);
})();
</script>
</body>
</html>
```

- [ ] **Step 2: Verify the build compiles**

Run: `cd /root/Phosphor && go build -o phosphor .`
Expected: compiles without errors

- [ ] **Step 3: Manual smoke test**

Run: `cd /root/Phosphor && ./phosphor -dir /tmp -port 8099 &`

Open on phone or browser dev tools mobile mode: `http://<host>:8099/mobile`

Verify:
- Page loads with dark theme
- Terminal connects and shows shell prompt
- Key buttons send correct sequences
- Toolbar expands/collapses
- Menu opens/closes
- Input box + Enter sends text to terminal

Kill: `kill %1`

- [ ] **Step 4: Commit**

```bash
git add mobile.html
git commit -m "feat: implement mobile page with terminal, toolbar, voice, and menu"
```

---

### Task 4: Backend — Wire up `go:embed` and restart args for OpenRouter key

**Files:**
- Modify: `main.go:1042-1063` (restart command args)

The restart command needs to pass the `-openrouter-key` flag so the key persists across restarts.

- [ ] **Step 1: Update restart args in `main()`**

In the `POST /api/restart` handler, after `restartArgs` is built, add the openrouter key:

```go
if openrouterAPIKey != "" {
	restartArgs = append(restartArgs, "-openrouter-key", openrouterAPIKey)
}
```

- [ ] **Step 2: Verify build**

Run: `cd /root/Phosphor && go build -o phosphor .`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "fix: pass openrouter-key through server restart"
```

---

### Task 5: Integration testing — Voice endpoint with mock

**Files:**
- Modify: `main_test.go`

We already have unit tests for error paths. Add a test that verifies the happy path structure (mocking the external API isn't practical in unit tests, but we can test that a valid request with a key reaches the point of making the external call).

- [ ] **Step 1: Add integration-style test**

Add to `main_test.go`:

```go
func TestHandleVoiceValidRequest(t *testing.T) {
	ts := setupTestServer(t)
	openrouterAPIKey = "test-key-not-real"

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("audio", "recording.webm")
	fw.Write([]byte("fake audio content"))
	w.WriteField("prompt", "test prompt")
	w.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/voice", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// With a fake key, OpenRouter will return an auth error (not 501 or 400)
	// This verifies the request parsing succeeded and the handler tried to call OpenRouter
	if resp.StatusCode == http.StatusNotImplemented || resp.StatusCode == http.StatusBadRequest {
		t.Fatalf("should not be 501 or 400 with valid audio and key, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	// Should have either text or error from OpenRouter
	if result["text"] == "" && result["error"] == "" {
		t.Fatal("expected either text or error in response")
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `cd /root/Phosphor && go test -v -timeout 60s`
Expected: all tests PASS

- [ ] **Step 3: Commit**

```bash
git add main_test.go
git commit -m "test: add voice endpoint integration test"
```

---

### Task 6: Final verification and cleanup

**Files:**
- All modified files

- [ ] **Step 1: Full build**

Run: `cd /root/Phosphor && go build -ldflags "-s -w" -o phosphor .`
Expected: clean build, single binary

- [ ] **Step 2: Run full test suite**

Run: `cd /root/Phosphor && go test -v`
Expected: all tests pass

- [ ] **Step 3: Verify mobile page in browser**

Run: `cd /root/Phosphor && ./phosphor -dir /root -port 8099 &`

Test checklist (in browser mobile emulation):
- [ ] `/mobile` loads correctly
- [ ] Terminal connects and is interactive
- [ ] ESC, arrow keys, Enter send correct sequences
- [ ] Toolbar expand/collapse works, state persists
- [ ] Ctrl sticky key works (Ctrl → then C = Ctrl+C)
- [ ] Slash commands: /clear sends directly, /using-superpowers fills input box
- [ ] Quick reply y/n/yes! sends correctly
- [ ] Menu opens, shows sessions, shows files
- [ ] File viewer opens and closes
- [ ] Input box submits on Enter
- [ ] 🎤 button shows disabled state when no API key
- [ ] Desktop `/` page is completely unchanged

Kill: `kill %1`

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: mobile page polish and final adjustments"
```
