package fetch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// HFToken returns the Hugging Face token from HF_TOKEN,
// HUGGING_FACE_HUB_TOKEN, or the file written by `huggingface-cli login`
// ($HF_TOKEN_PATH, else $HF_HOME/token, else
// $XDG_CACHE_HOME/huggingface/token or ~/.cache/huggingface/token).
func HFToken() string {
	for _, k := range []string{"HF_TOKEN", "HUGGING_FACE_HUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	p := os.Getenv("HF_TOKEN_PATH")
	if p == "" {
		home := os.Getenv("HF_HOME")
		if home == "" {
			cache := os.Getenv("XDG_CACHE_HOME")
			if cache == "" {
				h, err := os.UserHomeDir()
				if err != nil {
					return ""
				}
				cache = filepath.Join(h, ".cache")
			}
			home = filepath.Join(cache, "huggingface")
		}
		p = filepath.Join(home, "token")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ModelScopeToken returns MODELSCOPE_API_TOKEN.
func ModelScopeToken() string {
	return strings.TrimSpace(os.Getenv("MODELSCOPE_API_TOKEN"))
}

// netrcLookup returns login/password for host from $NETRC or ~/.netrc.
func netrcLookup(host string) (string, string, bool) {
	p := os.Getenv("NETRC")
	if p == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", "", false
		}
		p = filepath.Join(h, ".netrc")
	}
	f, err := os.Open(p)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	var toks []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		toks = append(toks, strings.Fields(line)...)
	}
	type entry struct{ login, password string }
	var match, def *entry
	var cur *entry
	for i := 0; i < len(toks); i++ {
		next := func() string {
			if i+1 < len(toks) {
				i++
				return toks[i]
			}
			return ""
		}
		switch toks[i] {
		case "machine":
			cur = &entry{}
			if strings.EqualFold(next(), host) && match == nil {
				match = cur
			}
		case "default":
			cur = &entry{}
			if def == nil {
				def = cur
			}
		case "login":
			if cur != nil {
				cur.login = next()
			}
		case "password":
			if cur != nil {
				cur.password = next()
			}
		case "account", "macdef":
			next()
		}
	}
	if match == nil {
		match = def
	}
	if match == nil || (match.login == "" && match.password == "") {
		return "", "", false
	}
	return match.login, match.password, true
}
