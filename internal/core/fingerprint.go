package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// fingerprintVersion 写进哈希原文。算法从按行切 git status 换成 NUL 记录后，旧指纹不再等于新指纹。
const fingerprintVersion = "v2"

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
	tree, err := gitBytes(root, "ls-tree", "-r", "-z", "HEAD")
	if err != nil && !strings.Contains(err.Error(), "exit status") {
		return Fingerprint{Kind: "unknown", Note: "无法读取 git 树：" + err.Error()}
	}
	treeFiles, err := parseTreeZ(tree, proj)
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: err.Error()}
	}
	files = append(files, treeFiles...)
	status, err := gitBytes(root, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: "无法读取 git 状态：" + err.Error()}
	}
	entries, err := parsePorcelainZ(status)
	if err != nil {
		return Fingerprint{Kind: "unknown", Note: err.Error()}
	}
	dirty := false
	for _, ent := range entries {
		path := filepath.ToSlash(ent.path)
		if ignoredCandidate(path, proj) || path == "lauren.project.json" {
			continue
		}
		dirty = true
		token, ferr := hashCandidate(root, path)
		if ferr != nil {
			if errors.Is(ferr, os.ErrNotExist) {
				files = append(files, path+" deleted")
				continue
			}
			return Fingerprint{Kind: "unknown", Note: "无法读取候选文件 " + path + "：" + ferr.Error()}
		}
		files = append(files, path+" "+token)
	}
	sort.Strings(files)
	h := sha256.New()
	h.Write([]byte("fingerprint " + fingerprintVersion + "\n"))
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
		CheckTimeoutSeconds int64         `json:"check_timeout_seconds"`
		Checks              []CheckSpec   `json:"checks"`
		Requirements        []Requirement `json:"requirements"`
	}{
		CheckTimeoutSeconds: proj.projectTimeoutSeconds(),
		Checks:              proj.Checks,
		Requirements:        proj.Requirements,
	}
	b, _ := json.Marshal(payload)
	return b
}

func catalogHash(proj Project) string {
	sum := sha256.Sum256(catalogBytes(proj))
	return hex.EncodeToString(sum[:])
}

func ignoredCandidate(path string, proj Project) bool {
	if skipPath(path) {
		return true
	}
	p := strings.TrimPrefix(filepath.ToSlash(path), "./")
	dir := proj.reportsDir()
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func skipPath(path string) bool {
	p := strings.TrimPrefix(path, "./")
	return p == ".lauren/runtime" || strings.HasPrefix(p, ".lauren/runtime/") || p == ".lauren/problems" || strings.HasPrefix(p, ".lauren/problems/") || p == ".lauren" || strings.HasPrefix(p, ".git/")
}

type porcelainEntry struct {
	xy   string
	path string
	orig string
}

func parsePorcelainZ(data []byte) ([]porcelainEntry, error) {
	if len(data) == 0 {
		return nil, nil
	}
	parts := bytes.Split(data, []byte{0})
	var out []porcelainEntry
	for i := 0; i < len(parts); {
		rec := parts[i]
		if len(rec) == 0 {
			i++
			continue
		}
		if len(rec) < 4 || rec[2] != ' ' {
			return nil, fmt.Errorf("无法解析 git status 的 NUL 记录")
		}
		xy := string(rec[:2])
		path := string(rec[3:])
		i++
		orig := ""
		if strings.ContainsAny(xy, "RC") {
			if i >= len(parts) || len(parts[i]) == 0 {
				return nil, fmt.Errorf("git status 的重命名或复制记录缺少路径")
			}
			orig = string(parts[i])
			i++
		}
		out = append(out, porcelainEntry{xy: xy, path: path, orig: orig})
	}
	return out, nil
}

func parseTreeZ(data []byte, proj Project) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var files []string
	for _, rec := range bytes.Split(data, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		tab := bytes.IndexByte(rec, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("无法解析 git ls-tree 的 NUL 记录")
		}
		path := filepath.ToSlash(string(rec[tab+1:]))
		if ignoredCandidate(path, proj) || path == "lauren.project.json" {
			continue
		}
		files = append(files, "blob "+string(rec))
	}
	return files, nil
}

func hashCandidate(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(full)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256([]byte("symlink\n" + target))
		return "symlink " + hex.EncodeToString(sum[:]), nil
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s 不是普通文件", rel)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "file " + hex.EncodeToString(sum[:]), nil
}

func gitOut(dir string, args ...string) (string, error) {
	b, err := gitBytes(dir, args...)
	return string(b), err
}

func gitBytes(dir string, args ...string) ([]byte, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return c.Output()
}
