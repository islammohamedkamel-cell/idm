package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var assets embed.FS

type Job struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Error   string    `json:"error,omitempty"`
	Done    int64     `json:"done"`
	Total   int64     `json:"total"`
	SHA256  string    `json:"sha256,omitempty"`
	ETag    string    `json:"etag,omitempty"`
	Video   bool      `json:"video"`
	StartAt time.Time `json:"startAt"`
	Limit   int64     `json:"limit"`
	cancel  context.CancelFunc
}
type Manager struct {
	mu     sync.Mutex
	jobs   []*Job
	dir    string
	client *http.Client
	token  string
	active bool
}

func newManager(dir string) *Manager {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return &Manager{dir: dir, token: hex.EncodeToString(b), client: &http.Client{Timeout: 0}}
}
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}
func (m *Manager) saveLocked() error {
	b, e := json.MarshalIndent(m.jobs, "", "  ")
	if e != nil {
		return e
	}
	p := filepath.Join(m.dir, "jobs.json")
	if e = os.WriteFile(p+".tmp", b, 0600); e != nil {
		return e
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(p)
	}
	return os.Rename(p+".tmp", p)
}
func (m *Manager) load() error {
	b, e := os.ReadFile(filepath.Join(m.dir, "jobs.json"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &m.jobs); e != nil {
		return e
	}
	for _, j := range m.jobs {
		if j.Status == "running" {
			j.Status = "paused"
		}
	}
	return nil
}
func (m *Manager) add(raw string, video bool, at time.Time, limit int64) (*Job, error) {
	if !validURL(raw) || limit < 0 {
		return nil, errors.New("enter a valid HTTP(S) URL and nonnegative speed limit")
	}
	u, _ := url.Parse(raw)
	if video && !(u.Hostname() == "youtu.be" || u.Hostname() == "youtube.com" || strings.HasSuffix(u.Hostname(), ".youtube.com")) {
		return nil, errors.New("video mode supports YouTube URLs only")
	}
	b := make([]byte, 8)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	id := hex.EncodeToString(b)
	name := filepath.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		name = "download.bin"
	}
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	if len(name) > 140 {
		name = name[:140]
	}
	name = id + "-" + name
	j := &Job{ID: id, URL: raw, Name: name, Video: video, Status: "queued", StartAt: at, Limit: limit}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = append(m.jobs, j)
	if e := m.saveLocked(); e != nil {
		m.jobs = m.jobs[:len(m.jobs)-1]
		return nil, e
	}
	return j, nil
}
func (m *Manager) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		return
	}
	for _, j := range m.jobs {
		if j.Status == "queued" && !time.Now().Before(j.StartAt) {
			ctx, c := context.WithCancel(context.Background())
			j.cancel = c
			j.Status = "running"
			j.Error = ""
			m.active = true
			go m.run(ctx, j)
			return
		}
	}
}
func (m *Manager) run(ctx context.Context, j *Job) {
	e := m.transfer(ctx, j)
	m.mu.Lock()
	defer m.mu.Unlock()
	j.cancel = nil
	m.active = false
	if j.Status != "paused" && j.Status != "cancelled" {
		if e != nil {
			j.Status = "failed"
			j.Error = e.Error()
		} else {
			j.Status = "completed"
		}
	}
	if err := m.saveLocked(); err != nil {
		log.Print(err)
	}
}
func (m *Manager) transfer(ctx context.Context, j *Job) error {
	if j.Video {
		path, e := exec.LookPath("yt-dlp")
		if e != nil {
			return errors.New("install yt-dlp and FFmpeg on PATH for YouTube downloads")
		}
		cmd := exec.CommandContext(ctx, path, "--no-playlist", "--no-overwrites", "--restrict-filenames", "--max-filesize", "20G", "-P", m.dir, "-o", j.ID+"-%(title).180B.%(ext)s", "--", j.URL)
		out, e := cmd.CombinedOutput()
		if e != nil {
			if len(out) > 2000 {
				out = out[len(out)-2000:]
			}
			return fmt.Errorf("yt-dlp: %v: %s", e, out)
		}
		return nil
	}
	var e error
	for attempt := 0; attempt < 3; attempt++ {
		e = m.download(ctx, j)
		if e == nil || ctx.Err() != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return e
}
func (m *Manager) download(ctx context.Context, j *Job) error {
	p := filepath.Join(m.dir, j.Name)
	part := p + ".part"
	var offset int64
	if st, e := os.Stat(part); e == nil {
		offset = st.Size()
	}
	m.mu.Lock()
	etag := j.ETag
	m.mu.Unlock()
	if etag == "" {
		offset = 0
	}
	req, e := http.NewRequestWithContext(ctx, "GET", j.URL, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		req.Header.Set("If-Range", etag)
	}
	resp, e := m.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if resp.StatusCode == 206 {
		var start, end int64
		if _, e = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); e != nil || start != offset || end < start || total <= end {
			return errors.New("invalid partial response")
		}
	} else {
		offset = 0
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, e := os.OpenFile(part, flags, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Seek(offset, 0); e != nil {
		return e
	}
	m.mu.Lock()
	j.Done = offset
	j.Total = total
	j.ETag = resp.Header.Get("ETag")
	limit := j.Limit
	m.mu.Unlock()
	buf := make([]byte, 64*1024)
	started := time.Now()
	var copied int64
	for {
		n, re := resp.Body.Read(buf)
		if n > 0 {
			w, we := f.Write(buf[:n])
			if we != nil {
				return we
			}
			if w != n {
				return io.ErrShortWrite
			}
			copied += int64(n)
			m.mu.Lock()
			j.Done += int64(n)
			m.mu.Unlock()
			if limit > 0 {
				delay := time.Duration(float64(copied)/float64(limit)*float64(time.Second)) - time.Since(started)
				if delay > 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(delay):
					}
				}
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if total >= 0 && offset+copied != total {
		return io.ErrUnexpectedEOF
	}
	if e = f.Close(); e != nil {
		return e
	}
	file, e := os.Open(part)
	if e != nil {
		return e
	}
	h := sha256.New()
	_, e = io.Copy(h, file)
	file.Close()
	if e != nil {
		return e
	}
	if e = os.Rename(part, p); e != nil {
		return e
	}
	m.mu.Lock()
	j.SHA256 = hex.EncodeToString(h.Sum(nil))
	m.mu.Unlock()
	return nil
}
func (m *Manager) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := assets.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, ytErr := exec.LookPath("yt-dlp")
		_, ffErr := exec.LookPath("ffmpeg")
		json.NewEncoder(w).Encode(map[string]bool{"ytdlp": ytErr == nil, "ffmpeg": ffErr == nil})
	})
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			m.mu.Lock()
			defer m.mu.Unlock()
			json.NewEncoder(w).Encode(m.jobs)
			return
		}
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var in struct {
			URL     string    `json:"url"`
			Video   bool      `json:"video"`
			StartAt time.Time `json:"startAt"`
			Limit   int64     `json:"limit"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		j, e := m.add(in.URL, in.Video, in.StartAt, in.Limit)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": j.ID})
	})
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var in struct{ ID, Action string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, j := range m.jobs {
			if j.ID != in.ID {
				continue
			}
			switch in.Action {
			case "pause", "cancel":
				if j.Status == "completed" {
					http.Error(w, "already completed", 409)
					return
				}
				if j.cancel != nil {
					j.cancel()
				}
				if in.Action == "pause" {
					j.Status = "paused"
				} else {
					j.Status = "cancelled"
				}
			case "resume":
				if j.cancel != nil || j.Status == "completed" {
					http.Error(w, "job is active or complete", 409)
					return
				}
				j.Status = "queued"
				j.StartAt = time.Time{}
			default:
				http.Error(w, "unknown action", 400)
				return
			}
			if e := m.saveLocked(); e != nil {
				http.Error(w, e.Error(), 500)
			}
			return
		}
		http.NotFound(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		host, _, e := net.SplitHostPort(r.Host)
		if e != nil || host != "127.0.0.1" {
			http.Error(w, "invalid host", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			origin := r.Header.Get("Origin")
			if strings.HasPrefix(origin, "chrome-extension://") || strings.HasPrefix(origin, "moz-extension://") {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+m.token {
				http.Error(w, "unauthorized", 401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
		}
		mux.ServeHTTP(w, r)
	})
}
func main() {
	dir := os.Getenv("IDM_DOWNLOAD_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Downloads", "OpenDownload")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		log.Fatal(e)
	}
	m := newManager(dir)
	if e := m.load(); e != nil {
		log.Fatal("Cannot load queue: ", e)
	}
	port := os.Getenv("IDM_PORT")
	if port == "" {
		port = "17843"
	}
	if _, e := strconv.Atoi(port); e != nil {
		log.Fatal(e)
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:"+port)
	if e != nil {
		log.Fatal(e)
	}
	link := "http://" + ln.Addr().String() + "/#" + m.token
	fmt.Println("OpenDownload — " + link)
	fmt.Println("Files: " + dir)
	if os.Getenv("IDM_NO_BROWSER") == "" {
		switch runtime.GOOS {
		case "windows":
			exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Start()
		case "darwin":
			exec.Command("open", link).Start()
		default:
			exec.Command("xdg-open", link).Start()
		}
	}
	go func() {
		for range time.Tick(250 * time.Millisecond) {
			m.tick()
		}
	}()
	srv := &http.Server{Handler: m.handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := srv.Serve(ln); e != http.ErrServerClosed {
			log.Print(e)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	<-signals
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.cancel != nil {
			j.Status = "paused"
			j.cancel()
		}
	}
	m.saveLocked()
	m.mu.Unlock()
	srv.Close()
}
