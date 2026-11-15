package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func fingerprint(root string, envName string) Fingerprint {
	top, err := gitOut(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: "当前目录不是 git 工作区。v0.1 不在未知候选上把检查记为通过。"}
	}
	top = strings.TrimSpace(top)
	absTop, _ := filepath.Abs(top)
	absRoot, _ := filepath.Abs(root)
	if absTop != absRoot {
		return Fingerprint{Kind: "not-root", Note: "lauren.project.json 所在目录不是 git 根。v0.1 不把父仓库当成这个项目的候选。"}
	}
	head, err := gitOut(root, "rev-parse", "HEAD")
	if err != nil {
		head = "NO-COMMIT"
	}
	head = strings.TrimSpace(head)
	status, err := gitOut(root, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: "无法读取 git 状态：" + err.Error()}
	}
	var files []string
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
		if skipPath(path) {
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
	cfg, _ := os.ReadFile(filepath.Join(root, "lauren.project.json"))
	h := sha256.New()
	h.Write([]byte("head " + head + "\n"))
	h.Write([]byte("env " + envName + "\n"))
	h.Write([]byte("config "))
	h.Write(cfg)
	h.Write([]byte("\n"))
	for _, f := range files {
		h.Write([]byte(f))
		h.Write([]byte("\n"))
	}
	return Fingerprint{
		Kind:  "ok",
		Hash:  hex.EncodeToString(h.Sum(nil)),
		Head:  head,
		Dirty: dirty,
	}
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
