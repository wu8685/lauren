package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func fingerprint(root string, proj Project) Fingerprint {
	top, err := gitOut(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: "当前目录不是 git 工作区。未知候选上不能把检查记为通过。"}
	}
	top = strings.TrimSpace(top)
	absTop, _ := filepath.Abs(top)
	absRoot, _ := filepath.Abs(root)
	if absTop != absRoot {
		return Fingerprint{Kind: "not-root", Note: "lauren.project.json 所在目录不是 git 根。不把父仓库当成这个项目的候选。"}
	}
	head, err := gitOut(root, "rev-parse", "HEAD")
	if err != nil {
		head = "NO-COMMIT"
	}
	head = strings.TrimSpace(head)
	var files []string
	if tree, err := gitOut(root, "ls-tree", "-r", "HEAD"); err == nil {
		for _, line := range strings.Split(tree, "\n") {
			tab := strings.Index(line, "\t")
			if tab < 0 {
				continue
			}
			path := filepath.ToSlash(line[tab+1:])
			if skipPath(path) || path == "lauren.project.json" {
				continue
			}
			files = append(files, "blob "+line)
		}
	}
	status, err := gitOut(root, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: "无法读取 git 状态：" + err.Error()}
	}
	dirty := false
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		path = filepath.ToSlash(strings.TrimSpace(path))
		if skipPath(path) || path == "lauren.project.json" {
			continue
		}
		dirty = true
		sum, ferr := hashFile(filepath.Join(root, filepath.FromSlash(path)))
		if ferr != nil {
			files = append(files, path+" deleted")
			continue
		}
		files = append(files, path+" "+sum)
	}
	sort.Strings(files)
	h := sha256.New()
	h.Write([]byte("catalog "))
	h.Write(catalogBytes(proj))
	h.Write([]byte("\n"))
	for _, f := range files {
		h.Write([]byte(f))
		h.Write([]byte("\n"))
	}
	return Fingerprint{Kind: "ok", Hash: hex.EncodeToString(h.Sum(nil)), Head: head, Dirty: dirty}
}

func catalogBytes(proj Project) []byte {
	payload := struct {
		Checks       []CheckSpec   `json:"checks"`
		Requirements []Requirement `json:"requirements"`
	}{Checks: proj.Checks, Requirements: proj.Requirements}
	b, _ := json.Marshal(payload)
	return b
}

func catalogHash(proj Project) string {
	sum := sha256.Sum256(catalogBytes(proj))
	return hex.EncodeToString(sum[:])
}

func skipPath(path string) bool {
	p := strings.TrimPrefix(path, "./")
	return p == ".lauren/runtime" || strings.HasPrefix(p, ".lauren/runtime/") || p == ".lauren" || strings.HasPrefix(p, ".git/")
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func gitOut(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	b, err := c.Output()
	if err != nil {
		return "", err
	}
	return string(b), nil
}
