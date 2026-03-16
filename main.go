package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

//go:embed index.html
var indexHTML embed.FS

type FileInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	IsDir    bool      `json:"is_dir"`
	Modified time.Time `json:"modified"`
}

var rootDir string

// Auth
var (
	authPassword string
	sessions     = map[string]time.Time{}
	sessionMu    sync.RWMutex
)

const sessionCookieName = "fs_session"
const sessionMaxAge = 7 * 24 * time.Hour

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func createSession() (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	sessionMu.Lock()
	sessions[token] = time.Now()
	sessionMu.Unlock()
	return token, nil
}

func validSession(token string) bool {
	sessionMu.RLock()
	created, ok := sessions[token]
	sessionMu.RUnlock()
	if !ok {
		return false
	}
	if time.Since(created) > sessionMaxAge {
		sessionMu.Lock()
		delete(sessions, token)
		sessionMu.Unlock()
		return false
	}
	return true
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authPassword == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/auth-check" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && validSession(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" {
			serveLoginPage(w)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(authPassword)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "wrong password"})
		return
	}
	token, err := createSession()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		sessionMu.Lock()
		delete(sessions, cookie.Value)
		sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	needAuth := authPassword != ""
	authenticated := false
	if needAuth {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			authenticated = validSession(cookie.Value)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{
		"need_auth":     needAuth,
		"authenticated": authenticated,
	})
}

func serveLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(loginPageHTML))
}

const loginPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Login - File Server</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: 'Fira Code', 'JetBrains Mono', 'Cascadia Code', 'Consolas', monospace; background: #0a0e14; display: flex; align-items: center; justify-content: center; min-height: 100vh; }
.card { background: #131820; border-radius: 8px; border: 1px solid #1e2a3a; box-shadow: 0 2px 8px rgba(0,0,0,0.3); padding: 2rem; width: 100%; max-width: 360px; }
h1 { font-size: 1.25rem; margin-bottom: 1.5rem; text-align: center; color: #00ff41; }
input { width: 100%; padding: 0.6rem 0.75rem; background: #0d1117; border: 1px solid #1e2a3a; border-radius: 4px; font-size: 0.95rem; margin-bottom: 1rem; outline: none; color: #c5cdd8; font-family: inherit; }
input:focus { border-color: #00ff41; }
button { width: 100%; padding: 0.6rem; background: #00ff41; color: #0a0e14; border: none; border-radius: 4px; font-size: 0.95rem; cursor: pointer; font-weight: 600; font-family: inherit; }
button:hover { background: #00cc33; }
.error { color: #ef4444; font-size: 0.85rem; margin-bottom: 0.75rem; display: none; text-align: center; }
</style>
</head>
<body>
<div class="card">
<h1>File Server</h1>
<div class="error" id="err"></div>
<input type="password" id="pw" placeholder="Password" autofocus>
<button onclick="doLogin()">Login</button>
</div>
<script>
document.getElementById('pw').addEventListener('keydown', function(e) {
  if (e.key === 'Enter') doLogin();
});
async function doLogin() {
  const pw = document.getElementById('pw').value;
  const errEl = document.getElementById('err');
  errEl.style.display = 'none';
  try {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({password: pw})
    });
    if (res.ok) {
      window.location.href = '/';
    } else {
      errEl.textContent = 'Wrong password';
      errEl.style.display = 'block';
    }
  } catch(e) {
    errEl.textContent = 'Connection error';
    errEl.style.display = 'block';
  }
}
</script>
</body>
</html>`

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// safePath validates and resolves a requested path to ensure it stays within rootDir.
func safePath(root, requested string) (string, error) {
	if requested == "" {
		return root, nil
	}
	cleaned := filepath.Clean(requested)
	joined := filepath.Join(root, cleaned)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}
	if !strings.HasPrefix(abs, root) {
		return "", fmt.Errorf("path traversal denied")
	}
	// Resolve symlinks to prevent escape
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File may not exist yet (upload target), check parent
		parent := filepath.Dir(abs)
		realParent, err2 := filepath.EvalSymlinks(parent)
		if err2 != nil {
			return "", fmt.Errorf("invalid path")
		}
		if !strings.HasPrefix(realParent, root) {
			return "", fmt.Errorf("path traversal denied")
		}
		return filepath.Join(realParent, filepath.Base(abs)), nil
	}
	if !strings.HasPrefix(real, root) {
		return "", fmt.Errorf("path traversal denied")
	}
	return real, nil
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.URL, time.Since(start).Round(time.Millisecond))
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := indexHTML.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleFiles(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	absPath, err := safePath(rootDir, reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		relPath := reqPath
		if relPath == "" {
			relPath = e.Name()
		} else {
			relPath = relPath + "/" + e.Name()
		}
		files = append(files, FileInfo{
			Name:     e.Name(),
			Path:     relPath,
			Size:     fi.Size(),
			IsDir:    e.IsDir(),
			Modified: fi.ModTime(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	absDir, err := safePath(rootDir, reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}
	fhs := r.MultipartForm.File["files"]
	if len(fhs) == 0 {
		http.Error(w, "no files", http.StatusBadRequest)
		return
	}
	for _, fh := range fhs {
		destPath, err := safePath(rootDir, filepath.Join(reqPath, fh.Filename))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Ensure parent directory matches expected directory
		if filepath.Dir(destPath) != absDir {
			http.Error(w, "invalid filename", http.StatusBadRequest)
			return
		}
		src, err := fh.Open()
		if err != nil {
			http.Error(w, "open error", http.StatusInternalServerError)
			return
		}
		dst, err := os.Create(destPath)
		if err != nil {
			src.Close()
			http.Error(w, "create error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}
		log.Printf("uploaded: %s (%d bytes)", destPath, fh.Size)
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "uploaded %d file(s)", len(fhs))
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	absPath, err := safePath(rootDir, reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot download directory", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(absPath)))
	http.ServeFile(w, r, absPath)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	absPath, err := safePath(rootDir, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if absPath == rootDir {
		http.Error(w, "cannot delete root directory", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		err = os.RemoveAll(absPath)
	} else {
		err = os.Remove(absPath)
	}
	if err != nil {
		http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("deleted: %s", absPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	oldAbs, err := safePath(rootDir, req.OldPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newAbs, err := safePath(rootDir, req.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if oldAbs == rootDir || newAbs == rootDir {
		http.Error(w, "cannot rename root directory", http.StatusBadRequest)
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		http.Error(w, "rename failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("renamed: %s -> %s", oldAbs, newAbs)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	absPath, err := safePath(rootDir, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Mkdir(absPath, 0755); err != nil {
		http.Error(w, "mkdir failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("mkdir: %s", absPath)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleRead(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	absPath, err := safePath(rootDir, reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot read directory", http.StatusBadRequest)
		return
	}
	if info.Size() > 10<<20 {
		http.Error(w, "file too large (>10MB)", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"content": string(data),
		"name":    filepath.Base(absPath),
	})
}

func handleSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	absPath, err := safePath(rootDir, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if absPath == rootDir {
		http.Error(w, "cannot write to root directory", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		http.Error(w, "cannot write to directory", http.StatusBadRequest)
		return
	}
	perm := info.Mode().Perm()
	if err := os.WriteFile(absPath, []byte(req.Content), perm); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("saved: %s (%d bytes)", absPath, len(req.Content))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}

	cmd := exec.Command("/bin/bash")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("pty start error: %v", err)
		conn.Close()
		return
	}

	var once sync.Once
	cleanup := func() {
		conn.Close()
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
			cmd.Process.Wait()
		}
	}

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				once.Do(cleanup)
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				once.Do(cleanup)
				return
			}
		}
	}()

	// WebSocket → PTY
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			once.Do(cleanup)
			return
		}
		if msgType == websocket.TextMessage && strings.HasPrefix(string(msg), "resize:") {
			var size struct {
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if err := json.Unmarshal(msg[7:], &size); err == nil {
				pty.Setsize(ptmx, &pty.Winsize{Cols: size.Cols, Rows: size.Rows})
			}
			continue
		}
		if _, err := ptmx.Write(msg); err != nil {
			once.Do(cleanup)
			return
		}
	}
}

func main() {
	dir := flag.String("dir", ".", "root directory to serve")
	port := flag.Int("port", 8080, "port to listen on")
	password := flag.String("password", "", "access password (empty = no auth)")
	flag.Parse()

	authPassword = *password

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("invalid dir: %v", err)
	}
	// Resolve symlinks for rootDir so prefix checks work correctly
	absDir, err = filepath.EvalSymlinks(absDir)
	if err != nil {
		log.Fatalf("invalid dir: %v", err)
	}
	rootDir = absDir

	info, err := os.Stat(rootDir)
	if err != nil || !info.IsDir() {
		log.Fatalf("directory does not exist: %s", rootDir)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /api/files", handleFiles)
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("GET /api/download", handleDownload)
	mux.HandleFunc("POST /api/delete", handleDelete)
	mux.HandleFunc("POST /api/rename", handleRename)
	mux.HandleFunc("POST /api/mkdir", handleMkdir)
	mux.HandleFunc("GET /api/read", handleRead)
	mux.HandleFunc("POST /api/save", handleSave)
	mux.HandleFunc("GET /api/terminal", handleTerminal)
	mux.HandleFunc("POST /api/login", handleLogin)
	mux.HandleFunc("POST /api/logout", handleLogout)
	mux.HandleFunc("GET /api/auth-check", handleAuthCheck)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("serving %s on http://0.0.0.0%s", rootDir, addr)
	if err := http.ListenAndServe(addr, logMiddleware(authMiddleware(mux))); err != nil {
		log.Fatal(err)
	}
}
