package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const resourcePrefix = "resource:"

type ResourceStamp struct {
	Path   string `json:"path"`
	Link   string `json:"link,omitempty"`
	SHA256 string `json:"sha256"`
}

func resolveCheckCommand(worktree, resourceDir string, argv []string) (exe string, args []string, files []string, toolPath, note string, err error) {
	if len(argv) == 0 || strings.Contains(argv[0], "..") {
		return "", nil, nil, "", "", fmt.Errorf("检查命令为空或路径越界")
	}
	raw := argv[0]
	var path string
	switch {
	case strings.HasPrefix(raw, resourcePrefix):
		path, err = cleanUnder(resourceDir, strings.TrimPrefix(raw, resourcePrefix))
		if err != nil {
			return "", nil, nil, "", "", err
		}
	case filepath.IsAbs(raw):
		path = raw
	case strings.Contains(raw, "/"):
		path, err = cleanUnder(worktree, raw)
		if err != nil {
			return "", nil, nil, "", "", err
		}
	default:
		tool, lerr := exec.LookPath(raw)
		if lerr != nil {
			return "", nil, nil, "", "", fmt.Errorf("找不到外部程序 %s", raw)
		}
		return tool, append([]string{tool}, argv[1:]...), nil, tool, "未对 PATH 上的外部程序做内容摘要：" + raw + " -> " + tool, nil
	}
	if _, err := os.Lstat(path); err != nil {
		return "", nil, nil, "", "", fmt.Errorf("资源不存在：%s", path)
	}
	return path, append([]string{path}, argv[1:]...), []string{path}, "", "", nil
}

func resourceFiles(worktree, resourceDir string, spec CheckSpec, commandFile string) ([]string, error) {
	var files []string
	if commandFile != "" {
		files = append(files, commandFile)
	}
	for _, raw := range spec.Resources {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if strings.Contains(raw, "..") {
			return nil, fmt.Errorf("资源路径越界：%s", raw)
		}
		var path string
		var err error
		switch {
		case strings.HasPrefix(raw, resourcePrefix):
			path, err = cleanUnder(resourceDir, strings.TrimPrefix(raw, resourcePrefix))
		case filepath.IsAbs(raw):
			path = raw
		default:
			path, err = cleanUnder(worktree, raw)
		}
		if err != nil {
			return nil, err
		}
		files = append(files, path)
	}
	return files, nil
}

func cleanUnder(root, rel string) (string, error) {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("资源路径越界：%s", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("资源路径越界：%s", rel)
	}
	return full, nil
}

func stampResources(paths []string) ([]ResourceStamp, error) {
	seen := map[string]bool{}
	var uniq []string
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		uniq = append(uniq, p)
	}
	sort.Strings(uniq)
	var out []ResourceStamp
	for _, p := range uniq {
		st, err := stampOne(p)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func stampOne(path string) (ResourceStamp, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return ResourceStamp{}, fmt.Errorf("资源不存在：%s", path)
	}
	if fi.IsDir() {
		return ResourceStamp{}, fmt.Errorf("暂不支持目录，请列出文件：%s", path)
	}
	st := ResourceStamp{Path: path}
	if fi.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			return ResourceStamp{}, fmt.Errorf("资源不存在：%s", path)
		}
		st.Link = link
		target, err := os.Stat(path)
		if err != nil {
			return ResourceStamp{}, fmt.Errorf("资源不存在：%s", path)
		}
		if target.IsDir() {
			return ResourceStamp{}, fmt.Errorf("暂不支持目录，请列出文件：%s", path)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ResourceStamp{}, fmt.Errorf("资源不存在：%s", path)
	}
	sum := sha256.Sum256(b)
	st.SHA256 = hex.EncodeToString(sum[:])
	return st, nil
}

func digestStamps(stamps []ResourceStamp) string {
	if len(stamps) == 0 {
		return ""
	}
	h := sha256.New()
	h.Write([]byte("resource v1\n"))
	for _, st := range stamps {
		fmt.Fprintf(h, "path %s\nlink %s\nsha256 %s\n", st.Path, st.Link, st.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func resourceDriftReason(run CheckRun) string {
	if len(run.ResourceStamps) == 0 {
		return ""
	}
	for _, old := range run.ResourceStamps {
		cur, err := stampOne(old.Path)
		if err != nil {
			return err.Error()
		}
		if old.Link != cur.Link {
			return "资源软链接目标变了：" + old.Path
		}
		if old.SHA256 != cur.SHA256 {
			return "资源内容变了：" + old.Path
		}
	}
	return ""
}

func stampsDiffer(before, after []ResourceStamp) string {
	if len(before) != len(after) {
		return "执行期间资源变了"
	}
	for i := range before {
		if before[i].Path != after[i].Path {
			return "执行期间资源变了：" + before[i].Path
		}
		if before[i].Link != after[i].Link || before[i].SHA256 != after[i].SHA256 {
			return "执行期间资源变了：" + before[i].Path
		}
	}
	return ""
}
