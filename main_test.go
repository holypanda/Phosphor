package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestServer creates a temp directory, sets global state, and returns a test server.
func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	tmp := t.TempDir()
	resolved, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatal(err)
	}
	rootDir = resolved
	authPassword = ""
	sessionMu.Lock()
	sessions = map[string]time.Time{}
	sessionMu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /mobile", handleMobile)
	mux.HandleFunc("GET /api/files", handleFiles)
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("POST /api/upload-folder", handleFolderUpload)
	mux.HandleFunc("GET /api/download", handleDownload)
	mux.HandleFunc("POST /api/delete", handleDelete)
	mux.HandleFunc("POST /api/rename", handleRename)
	mux.HandleFunc("POST /api/mkdir", handleMkdir)
	mux.HandleFunc("GET /api/read", handleRead)
	mux.HandleFunc("POST /api/save", handleSave)
	mux.HandleFunc("POST /api/login", handleLogin)
	mux.HandleFunc("POST /api/logout", handleLogout)
	mux.HandleFunc("GET /api/auth-check", handleAuthCheck)

	handler := logMiddleware(authMiddleware(mux))
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

// loginHelper logs in with the given password and returns cookies.
func loginHelper(t *testing.T, serverURL, password string) []*http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"password":%q}`, password)
	resp, err := http.Post(serverURL+"/api/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: status %d", resp.StatusCode)
	}
	return resp.Cookies()
}

// doRequest is a helper to make requests with optional cookies.
func doRequest(t *testing.T, method, url string, body io.Reader, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil && method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// --- Auth Tests ---

func TestAuthFlow(t *testing.T) {
	t.Run("no password mode", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/files", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("login success", func(t *testing.T) {
		ts := setupTestServer(t)
		authPassword = "secret123"
		cookies := loginHelper(t, ts.URL, "secret123")
		found := false
		for _, c := range cookies {
			if c.Name == sessionCookieName {
				found = true
			}
		}
		if !found {
			t.Fatal("session cookie not set")
		}
	})

	t.Run("login failure", func(t *testing.T) {
		ts := setupTestServer(t)
		authPassword = "secret123"
		body := `{"password":"wrong"}`
		resp, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("unauthorized access", func(t *testing.T) {
		ts := setupTestServer(t)
		authPassword = "secret123"
		resp := doRequest(t, "GET", ts.URL+"/api/files", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authorized access", func(t *testing.T) {
		ts := setupTestServer(t)
		authPassword = "secret123"
		cookies := loginHelper(t, ts.URL, "secret123")
		resp := doRequest(t, "GET", ts.URL+"/api/files", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("logout", func(t *testing.T) {
		ts := setupTestServer(t)
		authPassword = "secret123"
		cookies := loginHelper(t, ts.URL, "secret123")

		// Logout
		resp := doRequest(t, "POST", ts.URL+"/api/logout", nil, cookies)
		resp.Body.Close()

		// After logout, access should fail
		resp = doRequest(t, "GET", ts.URL+"/api/files", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 after logout, got %d", resp.StatusCode)
		}
	})

	t.Run("auth-check", func(t *testing.T) {
		ts := setupTestServer(t)

		// No password: need_auth=false
		resp := doRequest(t, "GET", ts.URL+"/api/auth-check", nil, nil)
		var result map[string]bool
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if result["need_auth"] {
			t.Fatal("expected need_auth=false when no password set")
		}

		// With password, not authenticated
		authPassword = "secret123"
		resp = doRequest(t, "GET", ts.URL+"/api/auth-check", nil, nil)
		result = map[string]bool{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if !result["need_auth"] {
			t.Fatal("expected need_auth=true")
		}
		if result["authenticated"] {
			t.Fatal("expected authenticated=false")
		}

		// With password, authenticated
		cookies := loginHelper(t, ts.URL, "secret123")
		resp = doRequest(t, "GET", ts.URL+"/api/auth-check", nil, cookies)
		result = map[string]bool{}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if !result["authenticated"] {
			t.Fatal("expected authenticated=true")
		}
	})
}

// --- Index ---

func TestHandleIndex(t *testing.T) {
	ts := setupTestServer(t)
	resp := doRequest(t, "GET", ts.URL+"/", nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

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

// --- Files ---

func TestHandleFiles(t *testing.T) {
	t.Run("list root", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("hi"), 0644)
		os.Mkdir(filepath.Join(rootDir, "subdir"), 0755)

		resp := doRequest(t, "GET", ts.URL+"/api/files", nil, nil)
		defer resp.Body.Close()
		var files []FileInfo
		json.NewDecoder(resp.Body).Decode(&files)
		if len(files) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(files))
		}
	})

	t.Run("list subdir", func(t *testing.T) {
		ts := setupTestServer(t)
		os.MkdirAll(filepath.Join(rootDir, "sub"), 0755)
		os.WriteFile(filepath.Join(rootDir, "sub", "a.txt"), []byte("a"), 0644)
		os.WriteFile(filepath.Join(rootDir, "outside.txt"), []byte("out"), 0644)

		resp := doRequest(t, "GET", ts.URL+"/api/files?path=sub", nil, nil)
		defer resp.Body.Close()
		var files []FileInfo
		json.NewDecoder(resp.Body).Decode(&files)
		if len(files) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(files))
		}
		if files[0].Name != "a.txt" {
			t.Fatalf("expected a.txt, got %s", files[0].Name)
		}
	})

	t.Run("path not found", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/files?path=nonexistent", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/files?path=../../etc", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// --- Upload ---

func TestHandleUpload(t *testing.T) {
	t.Run("normal upload", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("files", "test.txt")
		fw.Write([]byte("hello world"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
		}
		data, _ := os.ReadFile(filepath.Join(rootDir, "test.txt"))
		if string(data) != "hello world" {
			t.Fatalf("file content mismatch: %s", data)
		}
	})

	t.Run("no files", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload", &buf)
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

	t.Run("path traversal", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("files", "../../evil.txt")
		fw.Write([]byte("bad"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload?path=../", &buf)
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
}

// --- Folder Upload ---

func TestHandleFolderUpload(t *testing.T) {
	t.Run("normal upload", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.WriteField("relativePath", "folder/subfolder/test.txt")
		fw, _ := w.CreateFormFile("file", "test.txt")
		fw.Write([]byte("folder content"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload-folder", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
		}
		data, _ := os.ReadFile(filepath.Join(rootDir, "folder", "subfolder", "test.txt"))
		if string(data) != "folder content" {
			t.Fatalf("file content mismatch: %s", data)
		}
	})

	t.Run("missing relativePath", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("file", "test.txt")
		fw.Write([]byte("content"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload-folder", &buf)
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

	t.Run("path traversal", func(t *testing.T) {
		ts := setupTestServer(t)
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		w.WriteField("relativePath", "../../evil/test.txt")
		fw, _ := w.CreateFormFile("file", "test.txt")
		fw.Write([]byte("bad"))
		w.Close()

		req, _ := http.NewRequest("POST", ts.URL+"/api/upload-folder", &buf)
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
}

// --- Download ---

func TestHandleDownload(t *testing.T) {
	t.Run("download file", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "file.txt"), []byte("download me"), 0644)

		resp := doRequest(t, "GET", ts.URL+"/api/download?path=file.txt", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "download me" {
			t.Fatalf("content mismatch: %s", body)
		}
		cd := resp.Header.Get("Content-Disposition")
		if !strings.Contains(cd, "file.txt") {
			t.Fatalf("Content-Disposition missing filename: %s", cd)
		}
	})

	t.Run("download directory as zip", func(t *testing.T) {
		ts := setupTestServer(t)
		os.MkdirAll(filepath.Join(rootDir, "mydir"), 0755)
		os.WriteFile(filepath.Join(rootDir, "mydir", "a.txt"), []byte("aaa"), 0644)
		os.WriteFile(filepath.Join(rootDir, "mydir", "b.txt"), []byte("bbb"), 0644)

		resp := doRequest(t, "GET", ts.URL+"/api/download?path=mydir", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if ct != "application/zip" {
			t.Fatalf("expected application/zip, got %s", ct)
		}
		cd := resp.Header.Get("Content-Disposition")
		if !strings.Contains(cd, ".zip") {
			t.Fatalf("Content-Disposition should contain .zip: %s", cd)
		}

		// Verify zip contents
		body, _ := io.ReadAll(resp.Body)
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("zip read error: %v", err)
		}
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		if !names["a.txt"] || !names["b.txt"] {
			t.Fatalf("zip missing expected files, got: %v", names)
		}
	})

	t.Run("download directory with subdirs and empty dirs", func(t *testing.T) {
		ts := setupTestServer(t)
		os.MkdirAll(filepath.Join(rootDir, "top", "sub"), 0755)
		os.MkdirAll(filepath.Join(rootDir, "top", "empty"), 0755)
		os.WriteFile(filepath.Join(rootDir, "top", "root.txt"), []byte("root"), 0644)
		os.WriteFile(filepath.Join(rootDir, "top", "sub", "nested.txt"), []byte("nested"), 0644)

		resp := doRequest(t, "GET", ts.URL+"/api/download?path=top", nil, nil)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("zip read error: %v", err)
		}
		names := map[string]bool{}
		for _, f := range zr.File {
			names[f.Name] = true
		}
		if !names["root.txt"] {
			t.Fatal("missing root.txt")
		}
		if !names["sub/nested.txt"] {
			t.Fatal("missing sub/nested.txt")
		}
		if !names["sub/"] {
			t.Fatal("missing sub/ directory entry")
		}
		if !names["empty/"] {
			t.Fatal("missing empty/ directory entry")
		}
	})

	t.Run("path not found", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/download?path=nope.txt", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/download", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// --- Delete ---

func TestHandleDelete(t *testing.T) {
	t.Run("delete file", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "del.txt"), []byte("bye"), 0644)

		resp := doRequest(t, "POST", ts.URL+"/api/delete", strings.NewReader(`{"path":"del.txt"}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if _, err := os.Stat(filepath.Join(rootDir, "del.txt")); !os.IsNotExist(err) {
			t.Fatal("file should be deleted")
		}
	})

	t.Run("delete directory", func(t *testing.T) {
		ts := setupTestServer(t)
		os.MkdirAll(filepath.Join(rootDir, "deldir", "child"), 0755)
		os.WriteFile(filepath.Join(rootDir, "deldir", "child", "f.txt"), []byte("x"), 0644)

		resp := doRequest(t, "POST", ts.URL+"/api/delete", strings.NewReader(`{"path":"deldir"}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if _, err := os.Stat(filepath.Join(rootDir, "deldir")); !os.IsNotExist(err) {
			t.Fatal("directory should be deleted")
		}
	})

	t.Run("delete root", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "POST", ts.URL+"/api/delete", strings.NewReader(`{"path":""}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("delete not found", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "POST", ts.URL+"/api/delete", strings.NewReader(`{"path":"ghost.txt"}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// --- Rename ---

func TestHandleRename(t *testing.T) {
	t.Run("rename file", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "old.txt"), []byte("data"), 0644)

		body := `{"old_path":"old.txt","new_path":"new.txt"}`
		resp := doRequest(t, "POST", ts.URL+"/api/rename", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if _, err := os.Stat(filepath.Join(rootDir, "old.txt")); !os.IsNotExist(err) {
			t.Fatal("old file should not exist")
		}
		data, _ := os.ReadFile(filepath.Join(rootDir, "new.txt"))
		if string(data) != "data" {
			t.Fatalf("content mismatch: %s", data)
		}
	})

	t.Run("rename root", func(t *testing.T) {
		ts := setupTestServer(t)
		body := `{"old_path":"","new_path":"something"}`
		resp := doRequest(t, "POST", ts.URL+"/api/rename", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "safe.txt"), []byte("data"), 0644)
		body := `{"old_path":"safe.txt","new_path":"../../evil.txt"}`
		resp := doRequest(t, "POST", ts.URL+"/api/rename", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// --- Mkdir ---

func TestHandleMkdir(t *testing.T) {
	t.Run("create directory", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "POST", ts.URL+"/api/mkdir", strings.NewReader(`{"path":"newdir"}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		info, err := os.Stat(filepath.Join(rootDir, "newdir"))
		if err != nil || !info.IsDir() {
			t.Fatal("directory should exist")
		}
	})

	t.Run("already exists", func(t *testing.T) {
		ts := setupTestServer(t)
		os.Mkdir(filepath.Join(rootDir, "exists"), 0755)
		resp := doRequest(t, "POST", ts.URL+"/api/mkdir", strings.NewReader(`{"path":"exists"}`), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", resp.StatusCode)
		}
	})
}

// --- Read ---

func TestHandleRead(t *testing.T) {
	t.Run("read file", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "readme.txt"), []byte("hello content"), 0644)

		resp := doRequest(t, "GET", ts.URL+"/api/read?path=readme.txt", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		if result["content"] != "hello content" {
			t.Fatalf("content mismatch: %s", result["content"])
		}
		if result["name"] != "readme.txt" {
			t.Fatalf("name mismatch: %s", result["name"])
		}
	})

	t.Run("read directory", func(t *testing.T) {
		ts := setupTestServer(t)
		os.Mkdir(filepath.Join(rootDir, "adir"), 0755)
		resp := doRequest(t, "GET", ts.URL+"/api/read?path=adir", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/read", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		ts := setupTestServer(t)
		resp := doRequest(t, "GET", ts.URL+"/api/read?path=nope.txt", nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// --- Save ---

func TestHandleSave(t *testing.T) {
	t.Run("save file", func(t *testing.T) {
		ts := setupTestServer(t)
		os.WriteFile(filepath.Join(rootDir, "save.txt"), []byte("old"), 0644)

		body := `{"path":"save.txt","content":"new content"}`
		resp := doRequest(t, "POST", ts.URL+"/api/save", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		data, _ := os.ReadFile(filepath.Join(rootDir, "save.txt"))
		if string(data) != "new content" {
			t.Fatalf("content mismatch: %s", data)
		}
	})

	t.Run("save to directory", func(t *testing.T) {
		ts := setupTestServer(t)
		os.Mkdir(filepath.Join(rootDir, "savedir"), 0755)
		body := `{"path":"savedir","content":"data"}`
		resp := doRequest(t, "POST", ts.URL+"/api/save", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("save not found", func(t *testing.T) {
		ts := setupTestServer(t)
		body := `{"path":"ghost.txt","content":"data"}`
		resp := doRequest(t, "POST", ts.URL+"/api/save", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("save to root", func(t *testing.T) {
		ts := setupTestServer(t)
		body := `{"path":"","content":"data"}`
		resp := doRequest(t, "POST", ts.URL+"/api/save", strings.NewReader(body), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// --- SafePath ---

func TestSafePath(t *testing.T) {
	tmp := t.TempDir()
	root, _ := filepath.EvalSymlinks(tmp)

	t.Run("normal path", func(t *testing.T) {
		os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0644)
		p, err := safePath(root, "file.txt")
		if err != nil {
			t.Fatal(err)
		}
		if p != filepath.Join(root, "file.txt") {
			t.Fatalf("expected %s, got %s", filepath.Join(root, "file.txt"), p)
		}
	})

	t.Run("empty path", func(t *testing.T) {
		p, err := safePath(root, "")
		if err != nil {
			t.Fatal(err)
		}
		if p != root {
			t.Fatalf("expected root %s, got %s", root, p)
		}
	})

	t.Run("path traversal", func(t *testing.T) {
		_, err := safePath(root, "../../../etc/passwd")
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		_, err := safePath(root, "/etc/passwd")
		if err == nil {
			t.Fatal("expected error for absolute path")
		}
	})
}
