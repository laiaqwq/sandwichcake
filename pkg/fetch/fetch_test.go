package fetch

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/laiaqwq/sandwichcake/internal/testutil"
)

func TestParse(t *testing.T) {
	ep := Endpoints{HF: "https://hf-mirror.com"}
	sum := strings.Repeat("ab", 32)
	cases := []struct {
		in   string
		want Spec
		err  string
	}{
		{in: "hf://Qwen/Qwen3-8B", want: Spec{Provider: HF, Repo: "Qwen/Qwen3-8B", Rev: "main"}},
		{in: "hf://Qwen/Qwen3-8B@v2", want: Spec{Provider: HF, Repo: "Qwen/Qwen3-8B", Rev: "v2"}},
		{in: "modelscope://qwen/Qwen3-8B", want: Spec{Provider: ModelScope, Repo: "qwen/Qwen3-8B", Rev: "master"}},
		{in: "https://huggingface.co/deepseek-ai/DeepSeek-V4.1-Flash-1017/tree/main", want: Spec{Provider: HF, Repo: "deepseek-ai/DeepSeek-V4.1-Flash-1017", Rev: "main"}},
		{in: "https://huggingface.co/org/m/tree/main/", want: Spec{Provider: HF, Repo: "org/m", Rev: "main"}},
		{in: "https://huggingface.co/org/m", want: Spec{Provider: HF, Repo: "org/m", Rev: "main"}},
		{in: "https://huggingface.co/org/m/tree/refs%2Fpr%2F1/sub/dir", want: Spec{Provider: HF, Repo: "org/m", Rev: "refs/pr/1", Subdir: "sub/dir"}},
		{in: "https://huggingface.co/org/m/blob/main/model.safetensors", want: Spec{Provider: HF, Repo: "org/m", Rev: "main", File: "model.safetensors"}},
		{in: "https://hf-mirror.com/org/m/resolve/abc/a/b.gguf", want: Spec{Provider: HF, Repo: "org/m", Rev: "abc", File: "a/b.gguf"}},
		{in: "https://modelscope.cn/models/qwen/Qwen3-8B/files?Revision=v1", want: Spec{Provider: ModelScope, Repo: "qwen/Qwen3-8B", Rev: "v1"}},
		{in: "https://www.modelscope.cn/models/qwen/Qwen3-8B/resolve/master/config.json", want: Spec{Provider: ModelScope, Repo: "qwen/Qwen3-8B", Rev: "master", File: "config.json"}},
		{in: "https://example.com/x/model.gguf?sig=1#sha256=" + sum, want: Spec{Provider: HTTP, URL: "https://example.com/x/model.gguf?sig=1", SHA256: sum}},
		{in: "https://huggingface.co/datasets/org/d", err: "only Hugging Face models"},
		{in: "https://huggingface.co/org", err: "not a model URL"},
		{in: "https://huggingface.co/org/m/commits/main", err: "cannot parse"},
		{in: "https://example.com/x/model.gguf#md5=1", err: "fragment"},
		{in: "https://example.com/", err: "does not name a file"},
		{in: "hf://org", err: "invalid"},
		{in: "hf://org/../x", err: "invalid"},
		{in: "ftp://x/y", err: "unsupported"},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in, ep)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err %v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s:\n got %+v\nwant %+v", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalAndName(t *testing.T) {
	s, _ := Parse("https://user:pw@example.com/a/m.gguf?X-Signature=secret", Endpoints{})
	if c := s.Canonical(); c != "https://example.com/a/m.gguf" || s.Name() != "m.gguf" {
		t.Fatalf("canonical %q name %q", c, s.Name())
	}
	s, _ = Parse("https://huggingface.co/org/m/tree/dev", Endpoints{})
	if s.Canonical() != "hf://org/m@dev" || s.Name() != "org--m" {
		t.Fatalf("%q %q", s.Canonical(), s.Name())
	}
}

func hubFiles() []testutil.HubFile {
	return []testutil.HubFile{
		{Path: "config.json", Data: []byte(`{"a":1}`)},
		{Path: "model-00001.safetensors", Data: bytes.Repeat([]byte("w"), 300), LFS: true},
		{Path: "sub/tok.json", Data: []byte("tok")},
		{Path: "sub/deep/x.bin", Data: bytes.Repeat([]byte("x"), 100), LFS: true},
	}
}

func fastClient(h *testutil.Hub, o Options) *Client {
	if o.Endpoints == (Endpoints{}) {
		o.Endpoints = Endpoints{HF: h.API.URL, ModelScope: "https://modelscope.invalid"}
	}
	o.Backoff = func(int) time.Duration { return time.Millisecond }
	return New(o)
}

func TestListHFAndAuth(t *testing.T) {
	h := testutil.NewHub(t, "org/m", hubFiles())
	h.PageSize = 1
	c := fastClient(h, Options{HFToken: "hf_secret"})
	l, err := c.List(context.Background(), Spec{Provider: HF, Repo: "org/m", Rev: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Commit != h.Commit || len(l.Files) != 4 {
		t.Fatalf("listing %+v", l)
	}
	for _, f := range l.Files {
		switch f.Path {
		case "model-00001.safetensors":
			if f.SHA256 != testutil.SHA256(h.Files[1].Data) || f.GitSHA1 != "" || f.Size != 300 {
				t.Fatalf("lfs file %+v", f)
			}
		case "config.json":
			if f.SHA256 != "" || f.GitSHA1 == "" {
				t.Fatalf("git file %+v", f)
			}
		}
		rc, err := c.Open(context.Background(), f, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || int64(len(data)) != f.Size {
			t.Fatalf("%s: %d bytes %v", f.Path, len(data), err)
		}
	}
	for _, a := range h.APIAuth {
		if a != "Bearer hf_secret" {
			t.Fatalf("API auth %q", a)
		}
	}
	for _, a := range h.CDNAuth {
		if a != "" {
			t.Fatalf("token leaked to CDN host: %q", a)
		}
	}

	sub, err := c.List(context.Background(), Spec{Provider: HF, Repo: "org/m", Rev: "main", Subdir: "sub"})
	if err != nil || len(sub.Files) != 2 || sub.Files[0].Path != "sub/tok.json" {
		t.Fatalf("subdir %+v %v", sub, err)
	}
	one, err := c.List(context.Background(), Spec{Provider: HF, Repo: "org/m", Rev: "main", File: "sub/deep/x.bin"})
	if err != nil || len(one.Files) != 1 {
		t.Fatalf("file %+v %v", one, err)
	}
}

func TestListModelScope(t *testing.T) {
	h := testutil.NewHub(t, "org/m", hubFiles())
	c := fastClient(h, Options{ModelScopeToken: "ms_secret", Endpoints: Endpoints{HF: "https://hf.invalid", ModelScope: h.API.URL}})
	l, err := c.List(context.Background(), Spec{Provider: ModelScope, Repo: "org/m", Rev: "master"})
	if err != nil || len(l.Files) != 4 {
		t.Fatalf("%+v %v", l, err)
	}
	for _, f := range l.Files {
		if f.SHA256 == "" {
			t.Fatalf("no sha256: %+v", f)
		}
	}
	rc, err := c.Open(context.Background(), l.Files[0], 2)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != `a":1}` {
		t.Fatalf("ranged read %q", data)
	}
	if h.APIAuth[0] != "Bearer ms_secret" {
		t.Fatalf("auth %q", h.APIAuth[0])
	}
}

func TestOpenRetriesAndParallel(t *testing.T) {
	data := make([]byte, 10_000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	h := testutil.NewHub(t, "org/m", []testutil.HubFile{{Path: "big.bin", Data: data, LFS: true}})
	f := File{Path: "big.bin", Size: int64(len(data)), URL: h.API.URL + "/org/m/resolve/" + h.Commit + "/big.bin"}
	for _, o := range []Options{{Connections: 1}, {Connections: 3, SegmentSize: 1000}} {
		h.BreakAfter, h.Breaks = 700, 4
		c := fastClient(h, o)
		rc, err := c.Open(context.Background(), f, 123)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !bytes.Equal(got, data[123:]) {
			t.Fatalf("connections=%d: %d bytes, %v", o.Connections, len(got), err)
		}
	}
	h.BreakAfter, h.Breaks = 10, 100
	c := fastClient(h, Options{Connections: 1, Retries: 2})
	rc, _ := c.Open(context.Background(), f, 0)
	defer rc.Close()
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("progressing reads must keep retrying: %v", err)
	}
}

func TestListHTTP(t *testing.T) {
	h := testutil.NewHub(t, "org/m", []testutil.HubFile{{Path: "m.gguf", Data: []byte("gguf-data")}})
	c := fastClient(h, Options{})
	s, err := Parse(h.CDN.URL+"/blob/m.gguf", Endpoints{})
	if err != nil {
		t.Fatal(err)
	}
	l, err := c.List(context.Background(), s)
	if err != nil || len(l.Files) != 1 || l.Files[0].Size != 9 || l.Files[0].Path != "m.gguf" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestStatusHints(t *testing.T) {
	h := testutil.NewHub(t, "org/m", hubFiles())
	c := fastClient(h, Options{})
	_, err := c.List(context.Background(), Spec{Provider: HF, Repo: "org/missing", Rev: "main"})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err %v", err)
	}
}

func TestTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HF_HOME", "")
	t.Setenv("HF_TOKEN_PATH", "")
	t.Setenv("HF_TOKEN", "")
	t.Setenv("HUGGING_FACE_HUB_TOKEN", "")
	if HFToken() != "" {
		t.Fatal("expected no token")
	}
	p := filepath.Join(home, ".cache", "huggingface", "token")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("hf_file\n"), 0o600)
	if got := HFToken(); got != "hf_file" {
		t.Fatalf("token file: %q", got)
	}
	t.Setenv("HF_TOKEN", "hf_env")
	if got := HFToken(); got != "hf_env" {
		t.Fatalf("env: %q", got)
	}

	netrc := filepath.Join(home, ".netrc")
	os.WriteFile(netrc, []byte("machine a.example.com login u1 password p1\ndefault login du password dp\n"), 0o600)
	if u, p, ok := netrcLookup("a.example.com"); !ok || u != "u1" || p != "p1" {
		t.Fatalf("netrc machine: %q %q %v", u, p, ok)
	}
	if u, _, ok := netrcLookup("other"); !ok || u != "du" {
		t.Fatalf("netrc default: %q %v", u, ok)
	}
}
