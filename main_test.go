package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadAndChecksum(t *testing.T) {
	data := strings.Repeat("file data\n", 20000)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("ETag", "\"v1\""); fmt.Fprint(w, data) }))
	defer s.Close()
	m := newManager(t.TempDir())
	j, e := m.add(s.URL+"/file.bin", false, time.Time{}, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.transfer(context.Background(), j); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(m.dir, j.Name))
	if e != nil || string(b) != data {
		t.Fatal("content mismatch", e)
	}
	h := sha256.Sum256(b)
	if j.SHA256 != hex.EncodeToString(h[:]) || j.Done != int64(len(data)) {
		t.Fatal("invalid checksum or progress")
	}
}
func TestResume(t *testing.T) {
	data := "0123456789abcdef"
	for _, supports := range []bool{true, false} {
		t.Run(fmt.Sprint(supports), func(t *testing.T) {
			seen := false
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", "\"v1\"")
				if r.Header.Get("Range") == "bytes=5-" && r.Header.Get("If-Range") == "\"v1\"" {
					seen = true
				}
				if supports {
					w.Header().Set("Content-Range", "bytes 5-15/16")
					w.WriteHeader(206)
					fmt.Fprint(w, data[5:])
				} else {
					fmt.Fprint(w, data)
				}
			}))
			defer s.Close()
			m := newManager(t.TempDir())
			j, _ := m.add(s.URL+"/file", false, time.Time{}, 0)
			j.ETag = "\"v1\""
			os.WriteFile(filepath.Join(m.dir, j.Name)+".part", []byte(data[:5]), 0600)
			if e := m.download(context.Background(), j); e != nil {
				t.Fatal(e)
			}
			b, _ := os.ReadFile(filepath.Join(m.dir, j.Name))
			if !seen || string(b) != data {
				t.Fatal("resume/restart failed")
			}
		})
	}
}
func TestRejectBadRange(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 99-100/101")
		w.WriteHeader(206)
		fmt.Fprint(w, "XX")
	}))
	defer s.Close()
	m := newManager(t.TempDir())
	j, _ := m.add(s.URL, false, time.Time{}, 0)
	if e := m.download(context.Background(), j); e == nil {
		t.Fatal("accepted bad range")
	}
}
func TestTruncatedAndHTTPError(t *testing.T) {
	for _, status := range []int{200, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "100")
				w.WriteHeader(status)
				fmt.Fprint(w, "short")
			}))
			defer s.Close()
			m := newManager(t.TempDir())
			j, _ := m.add(s.URL, false, time.Time{}, 0)
			if e := m.download(context.Background(), j); e == nil {
				t.Fatal("accepted incomplete/error response")
			}
		})
	}
}
func TestAPIAuthenticationAndControls(t *testing.T) {
	m := newManager(t.TempDir())
	h := m.handler()
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:17843"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if call("GET", "/api/tools", "", "wrong").Code != 401 {
		t.Fatal("tool status must require authentication")
	}
	tools := call("GET", "/api/tools", "", m.token)
	if tools.Code != 200 || !strings.Contains(tools.Body.String(), `"ytdlp":`) || !strings.Contains(tools.Body.String(), `"ffmpeg":`) {
		t.Fatal("missing tool availability response")
	}
	if call("POST", "/api/tools", `{}`, m.token).Code != 405 {
		t.Fatal("tool endpoint accepted mutation")
	}
	if call("GET", "/api/jobs", "", "wrong").Code != 401 {
		t.Fatal("unauthorized")
	}
	if call("POST", "/api/jobs", `{"url":"file:///etc/passwd"}`, m.token).Code != 400 {
		t.Fatal("unsafe URL")
	}
	if call("POST", "/api/jobs", `{"url":"https://example.com/a"}`, m.token).Code != 200 {
		t.Fatal("add")
	}
	j := m.jobs[0]
	for _, a := range []string{"pause", "resume", "cancel"} {
		if call("POST", "/api/action", fmt.Sprintf(`{"id":%q,"action":%q}`, j.ID, a), m.token).Code != 200 {
			t.Fatal(a)
		}
	}
	if j.Status != "cancelled" {
		t.Fatal(j.Status)
	}
	if call("GET", "/", "", "").Code != 200 {
		t.Fatal("UI")
	}
	r := httptest.NewRequest("GET", "http://attacker.example:17843/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
}
func TestPersistenceAndSchedule(t *testing.T) {
	m := newManager(t.TempDir())
	j, _ := m.add("https://example.com/a", false, time.Now().Add(time.Hour), 0)
	m.tick()
	if m.active {
		t.Fatal("started scheduled job early")
	}
	j.Status = "running"
	m.saveLocked()
	n := newManager(m.dir)
	if e := n.load(); e != nil {
		t.Fatal(e)
	}
	if len(n.jobs) != 1 || n.jobs[0].Status != "paused" {
		t.Fatal("restart recovery")
	}
}
func TestCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	m := newManager(t.TempDir())
	j, _ := m.add(s.URL, false, time.Time{}, 0)
	ctx, c := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer c()
	if e := m.transfer(ctx, j); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestVideoAndURLValidation(t *testing.T) {
	m := newManager(t.TempDir())
	for _, u := range []string{"ftp://example.com/a", "https://user:pass@example.com/a", "javascript:alert(1)", "https://"} {
		if _, e := m.add(u, false, time.Time{}, 0); e == nil {
			t.Fatal("accepted", u)
		}
	}
	if _, e := m.add("https://example.com/video", true, time.Time{}, 0); e == nil {
		t.Fatal("accepted non YouTube video")
	}
	j, e := m.add("https://www.youtube.com/watch?v=example", true, time.Time{}, 0)
	if e != nil || !j.Video {
		t.Fatal("YouTube queue", e)
	}
}

func TestRetryAndThrottle(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, strings.Repeat("a", 1024))
	}))
	defer s.Close()
	m := newManager(t.TempDir())
	j, _ := m.add(s.URL, false, time.Time{}, 4096)
	start := time.Now()
	if e := m.transfer(context.Background(), j); e != nil {
		t.Fatal(e)
	}
	if calls != 2 || time.Since(start) < 1200*time.Millisecond {
		t.Fatal("retry or throttle did not execute")
	}
}
func TestQueueExecutes(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "queued file") }))
	defer s.Close()
	m := newManager(t.TempDir())
	j, _ := m.add(s.URL, false, time.Time{}, 0)
	m.tick()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		state := j.Status
		m.mu.Unlock()
		if state == "completed" {
			return
		}
		if state == "failed" {
			t.Fatal("queue transfer failed")
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queue did not complete")
}
