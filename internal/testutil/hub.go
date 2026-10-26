package testutil

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// HubFile is one file of a fake model repo.
type HubFile struct {
	Path string
	Data []byte
	LFS  bool // listed with an LFS sha256 (else only a git blob sha1)
}

// Hub fakes the Hugging Face and ModelScope APIs for one model repo. File
// content is served by a separate CDN server (another host:port), reached
// via 302 like the real services, with Range support.
type Hub struct {
	API, CDN *httptest.Server
	Repo     string // org/name
	Commit   string
	Files    []HubFile
	PageSize int // tree API page size (0 = no pagination)

	mu sync.Mutex
	// BreakAfter, when > 0, aborts CDN responses after that many body
	// bytes, Breaks times in total (simulates flaky downloads).
	BreakAfter int64
	Breaks     int
	// CorruptPath serves wrong bytes for this file.
	CorruptPath string
	// APIAuth / CDNAuth record Authorization headers seen.
	APIAuth, CDNAuth []string
	CDNRequests      int
}

// NewHub starts a fake hub serving files for repo.
func NewHub(t *testing.T, repo string, files []HubFile) *Hub {
	t.Helper()
	h := &Hub{Repo: repo, Commit: strings.Repeat("c0ffee", 6) + "abcd", Files: files}
	h.CDN = httptest.NewServer(http.HandlerFunc(h.serveCDN))
	h.API = httptest.NewServer(http.HandlerFunc(h.serveAPI))
	t.Cleanup(h.API.Close)
	t.Cleanup(h.CDN.Close)
	return h
}

// SHA256 returns the hex sha256 of data.
func SHA256(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func gitSHA1(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (h *Hub) file(p string) *HubFile {
	for i := range h.Files {
		if h.Files[i].Path == p {
			return &h.Files[i]
		}
	}
	return nil
}

func (h *Hub) serveAPI(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.APIAuth = append(h.APIAuth, r.Header.Get("Authorization"))
	h.mu.Unlock()
	p := r.URL.EscapedPath()
	hfAPI := "/api/models/" + h.Repo + "/"
	msAPI := "/api/v1/models/" + h.Repo + "/"
	switch {
	case strings.HasPrefix(p, hfAPI+"revision/"):
		json.NewEncoder(w).Encode(map[string]string{"sha": h.Commit})
	case strings.HasPrefix(p, hfAPI+"tree/"+h.Commit):
		sub, _ := url.PathUnescape(strings.TrimPrefix(strings.TrimPrefix(p, hfAPI+"tree/"+h.Commit), "/"))
		var items []map[string]any
		for _, f := range h.Files {
			if sub != "" && !strings.HasPrefix(f.Path, sub+"/") {
				continue
			}
			it := map[string]any{"type": "file", "path": f.Path, "size": len(f.Data), "oid": gitSHA1(f.Data)}
			if f.LFS {
				it["oid"] = strings.Repeat("0", 40)
				it["lfs"] = map[string]any{"oid": SHA256(f.Data), "size": len(f.Data)}
			}
			items = append(items, it)
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
		if h.PageSize > 0 && start+h.PageSize < len(items) {
			q := r.URL.Query()
			q.Set("cursor", strconv.Itoa(start+h.PageSize))
			w.Header().Set("Link", fmt.Sprintf(`<%s?%s>; rel="next"`, r.URL.Path, q.Encode()))
			items = items[start : start+h.PageSize]
		} else if h.PageSize > 0 {
			items = items[start:]
		}
		json.NewEncoder(w).Encode(items)
	case strings.HasPrefix(p, "/"+h.Repo+"/resolve/"+h.Commit+"/"):
		fp, _ := url.PathUnescape(strings.TrimPrefix(p, "/"+h.Repo+"/resolve/"+h.Commit+"/"))
		h.redirect(w, r, fp)
	case p == msAPI+"repo/files":
		var files []map[string]any
		for _, f := range h.Files {
			files = append(files, map[string]any{"Path": f.Path, "Name": f.Path, "Size": len(f.Data), "Sha256": SHA256(f.Data), "Type": "blob"})
		}
		json.NewEncoder(w).Encode(map[string]any{"Code": 200, "Data": map[string]any{"Files": files}})
	case p == msAPI+"repo":
		h.redirect(w, r, r.URL.Query().Get("FilePath"))
	default:
		http.NotFound(w, r)
	}
}

func (h *Hub) redirect(w http.ResponseWriter, r *http.Request, fp string) {
	if h.file(fp) == nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, h.CDN.URL+"/blob/"+url.PathEscape(fp)+"?sig=secret", http.StatusFound)
}

type breakingWriter struct {
	http.ResponseWriter
	left int64
}

func (b *breakingWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		b.ResponseWriter.Write(p[:b.left])
		b.ResponseWriter.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
	b.left -= int64(len(p))
	return b.ResponseWriter.Write(p)
}

func (h *Hub) serveCDN(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.CDNAuth = append(h.CDNAuth, r.Header.Get("Authorization"))
	h.CDNRequests++
	brk := h.BreakAfter > 0 && h.Breaks > 0
	if brk {
		h.Breaks--
	}
	h.mu.Unlock()
	fp, _ := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/blob/"))
	f := h.file(fp)
	if f == nil {
		http.NotFound(w, r)
		return
	}
	data := f.Data
	if fp == h.CorruptPath {
		data = bytes.ToUpper(data)
	}
	if brk {
		w = &breakingWriter{ResponseWriter: w, left: h.BreakAfter}
	}
	http.ServeContent(w, r, fp, time.Unix(0, 0), bytes.NewReader(data))
}

// UploadStatus wraps a registry handler and answers GET upload-status
// requests (which ggcr's fake registry lacks) from the Range headers of
// accepted PATCHes, so resumable uploads can be tested.
type UploadStatus struct {
	Inner http.Handler
	mu    sync.Mutex
	sizes map[string]int64
	// FailPatch, when > 0, makes that many PATCHes fail with 500 after the
	// inner registry accepted them (lost-ack simulation).
	FailPatch int
}

func (u *UploadStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isUpload := strings.Contains(r.URL.Path, "/blobs/uploads/") && !strings.HasSuffix(r.URL.Path, "/blobs/uploads/")
	switch {
	case isUpload && r.Method == http.MethodGet:
		u.mu.Lock()
		n, ok := u.sizes[r.URL.Path]
		u.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", r.URL.Path)
		w.Header().Set("Range", fmt.Sprintf("0-%d", n-1))
		w.WriteHeader(http.StatusNoContent)
	case isUpload && r.Method == http.MethodPatch:
		rec := httptest.NewRecorder()
		u.Inner.ServeHTTP(rec, r)
		if rec.Code == http.StatusAccepted {
			if _, end, ok := strings.Cut(rec.Header().Get("Range"), "-"); ok {
				n, _ := strconv.ParseInt(end, 10, 64)
				u.mu.Lock()
				if u.sizes == nil {
					u.sizes = map[string]int64{}
				}
				u.sizes[r.URL.Path] = n + 1
				fail := u.FailPatch > 0
				if fail {
					u.FailPatch--
				}
				u.mu.Unlock()
				if fail {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
	default:
		u.Inner.ServeHTTP(w, r)
	}
}
