package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	modeLegacy    = "legacy"
	modeShared    = "shared"
	modeSymlink   = "symlink"
	modeReference = "reference"

	sharedConfigRel = ".lauren/project/lauren.project.json"
)

// Layout 把当前 Worktree、共享配置来源和 runtime 分开。
// 候选、检查 cwd、变更指针和锁都用 Worktree，不跟配置文件所在目录绑在一起。
type Layout struct {
	Worktree     string
	Runtime      string
	ConfigPath   string
	ConfigMode   string
	MainWorktree string
	ResourceDir  string
	Entry        string
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func samePath(a, b string) bool {
	ca, ea := canonical(a)
	cb, eb := canonical(b)
	if ea != nil || eb != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ca == cb
}

func gitToplevel(dir string) (string, error) {
	out, err := gitOut(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return canonical(strings.TrimSpace(out))
}

func gitCommon(dir string) (string, error) {
	out, err := gitOut(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return canonical(p)
}

func sameRepo(a, b string) bool {
	ca, ea := gitCommon(a)
	cb, eb := gitCommon(b)
	if ea != nil || eb != nil {
		return false
	}
	return ca == cb
}

func worktreeList(dir string) ([]string, error) {
	data, err := gitBytes(dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, rec := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(rec, []byte("worktree ")) {
			paths = append(paths, string(rec[len("worktree "):]))
		}
	}
	return paths, nil
}

// mainCheckout 用 git worktree list 的第一项，且它必须是真正的检出。
// 不取 git common dir 的父目录：separate git-dir 时那个父目录不是主工作区。
func mainCheckout(from string) (string, error) {
	paths, err := worktreeList(from)
	if err != nil || len(paths) == 0 {
		return "", fmt.Errorf("无法定位主工作区：列不出工作区。请在本 Worktree 的 lauren.project.json 里写 shared_config。不要把 git 目录的父目录当成主工作区")
	}
	inside, err := gitOut(paths[0], "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return "", fmt.Errorf("无法定位主工作区：git 没有给出主检出目录。请在本 Worktree 的 lauren.project.json 里写 shared_config。不要把 git 目录的父目录当成主工作区")
	}
	top, err := gitOut(paths[0], "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("无法定位主工作区：%w", err)
	}
	return canonical(strings.TrimSpace(top))
}

func locate(start string) (Layout, Project, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return Layout{}, Project{}, err
	}
	top, gitErr := gitToplevel(abs)
	if gitErr != nil {
		return locateWithoutGit(abs)
	}
	return locateInWorktree(abs, top)
}

func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func locateWithoutGit(start string) (Layout, Project, error) {
	dir := start
	for {
		entry := filepath.Join(dir, "lauren.project.json")
		fi, err := os.Lstat(entry)
		if err == nil {
			if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
				return Layout{}, Project{}, fmt.Errorf("未初始化：当前目录不是 git 工作区，不能解析共享配置引用")
			}
			proj, rerr := readProjectFile(entry)
			if rerr != nil {
				return Layout{}, Project{}, rerr
			}
			root := dir
			if c, cerr := canonical(dir); cerr == nil {
				root = c
			}
			return layoutFor(root, entry, modeLegacy, "", ""), proj, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return Layout{}, Project{}, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Layout{}, Project{}, fmt.Errorf("未初始化：当前目录不是 git 工作区，也没有 lauren 配置")
		}
		if hasGitDir(parent) {
			return Layout{}, Project{}, fmt.Errorf("未初始化：当前目录不是 git 工作区，向上遇到 Git 仓库边界，没有 lauren 配置")
		}
		dir = parent
	}
}

func locateInWorktree(start, top string) (Layout, Project, error) {
	main, mainErr := mainCheckout(top)
	entryPath := filepath.Join(top, "lauren.project.json")
	fi, err := os.Lstat(entryPath)
	if err != nil && !os.IsNotExist(err) {
		return Layout{}, Project{}, err
	}
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return loadSymlink(top, main, entryPath)
	}
	if err == nil && fi.Mode().IsRegular() {
		body, rerr := os.ReadFile(entryPath)
		if rerr != nil {
			return Layout{}, Project{}, rerr
		}
		if isConfigRef(body) {
			return loadReference(top, main, entryPath, body)
		}
		shared, sharedErr := sharedFile(top, main, mainErr)
		if sharedErr == nil && !samePath(shared, entryPath) {
			if _, statErr := os.Stat(shared); statErr == nil {
				return Layout{}, Project{}, fmt.Errorf("配置冲突：工作区根的 lauren.project.json 与 .lauren/project/lauren.project.json 同时存在。只保留一种。Worktree 不能覆盖共享 required checks")
			}
		}
		proj, perr := readProjectFile(entryPath)
		if perr != nil {
			return Layout{}, Project{}, perr
		}
		return layoutFor(top, entryPath, modeLegacy, main, ""), proj, nil
	}

	shared, sharedErr := sharedFile(top, main, mainErr)
	if sharedErr != nil {
		return Layout{}, Project{}, sharedErr
	}
	if _, statErr := os.Stat(shared); statErr == nil {
		proj, perr := readProjectFile(shared)
		if perr != nil {
			return Layout{}, Project{}, perr
		}
		return layoutFor(top, shared, modeShared, main, ""), proj, nil
	}
	if main != "" && !samePath(main, top) {
		legacy := filepath.Join(main, "lauren.project.json")
		if fi, lerr := os.Lstat(legacy); lerr == nil && fi.Mode().IsRegular() {
			return Layout{}, Project{}, fmt.Errorf("未初始化：当前 Worktree 没有配置入口。主工作区仍是旧的单层 lauren.project.json，没有 .lauren/project/。请显式写 shared_config，不要把主工作区当成这个 Worktree 的运行根")
		}
	}
	if nested, ok := configAboveWorktreeRoot(start, top); ok {
		return Layout{}, Project{}, fmt.Errorf("发现了 %s，但它不在 worktree 根 %s", nested, top)
	}
	if note := missingSharedConfig(main); note != "" {
		return Layout{}, Project{}, fmt.Errorf("%s", note)
	}
	return Layout{}, Project{}, fmt.Errorf("未初始化：当前 Worktree 没有 lauren 配置")
}

func configAboveWorktreeRoot(start, top string) (string, bool) {
	dir := start
	if c, err := canonical(start); err == nil {
		dir = c
	}
	for {
		if samePath(dir, top) {
			return "", false
		}
		entry := filepath.Join(dir, "lauren.project.json")
		if _, err := os.Lstat(entry); err == nil {
			return entry, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func missingSharedConfig(main string) string {
	if main == "" {
		return ""
	}
	lauren := filepath.Join(main, ".lauren")
	st, err := os.Stat(lauren)
	if err != nil || !st.IsDir() {
		return ""
	}
	shared := filepath.Join(lauren, "project", "lauren.project.json")
	if _, err := os.Stat(shared); err == nil {
		return ""
	}
	return fmt.Sprintf("主工作区 %s 下没有 .lauren/project/lauren.project.json", main)
}

func sharedFile(top, main string, mainErr error) (string, error) {
	local := filepath.Join(top, filepath.FromSlash(sharedConfigRel))
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	if mainErr != nil {
		return "", mainErr
	}
	if main == "" || samePath(main, top) {
		return local, nil
	}
	return filepath.Join(main, filepath.FromSlash(sharedConfigRel)), nil
}

func loadSymlink(top, main, entryPath string) (Layout, Project, error) {
	target, err := filepath.EvalSymlinks(entryPath)
	if err != nil {
		return Layout{}, Project{}, fmt.Errorf("配置入口断了：%s", entryPath)
	}
	target = filepath.Clean(target)
	info, err := os.Stat(target)
	if err != nil {
		return Layout{}, Project{}, fmt.Errorf("配置入口断了：%s", entryPath)
	}
	if info.IsDir() {
		target = filepath.Join(target, "lauren.project.json")
		if _, err := os.Stat(target); err != nil {
			return Layout{}, Project{}, fmt.Errorf("共享配置不存在：%s", target)
		}
	}
	if !sameRepo(top, filepath.Dir(target)) {
		return Layout{}, Project{}, fmt.Errorf("配置在当前 Git 仓库之外：%s", target)
	}
	proj, err := readProjectFile(target)
	if err != nil {
		return Layout{}, Project{}, err
	}
	return layoutFor(top, target, modeSymlink, main, entryPath), proj, nil
}

func loadReference(top, main, entryPath string, body []byte) (Layout, Project, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var ref struct {
		Schema       int    `json:"schema"`
		SharedConfig string `json:"shared_config"`
	}
	if err := dec.Decode(&ref); err != nil {
		return Layout{}, Project{}, fmt.Errorf("Worktree 入口不能覆盖共享配置：%w", err)
	}
	if strings.TrimSpace(ref.SharedConfig) == "" {
		return Layout{}, Project{}, fmt.Errorf("共享配置不存在：引用没有 shared_config")
	}
	target := ref.SharedConfig
	if !filepath.IsAbs(target) {
		target = filepath.Join(top, target)
	}
	info, err := os.Stat(target)
	if err != nil {
		return Layout{}, Project{}, fmt.Errorf("共享配置不存在：%s", target)
	}
	if info.IsDir() {
		target = filepath.Join(target, "lauren.project.json")
		info, err = os.Stat(target)
		if err != nil {
			return Layout{}, Project{}, fmt.Errorf("共享配置不存在：%s", target)
		}
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return Layout{}, Project{}, fmt.Errorf("共享配置不存在：%s", target)
	}
	if !sameRepo(top, filepath.Dir(target)) {
		return Layout{}, Project{}, fmt.Errorf("配置在当前 Git 仓库之外：%s", target)
	}
	proj, err := readProjectFile(target)
	if err != nil {
		return Layout{}, Project{}, err
	}
	canon, cerr := canonical(target)
	if cerr == nil {
		target = canon
	}
	return layoutFor(top, target, modeReference, main, entryPath), proj, nil
}

func isConfigRef(body []byte) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	_, ok := raw["shared_config"]
	return ok
}

func layoutFor(top, configPath, mode, main, entry string) Layout {
	resource := top
	dir := filepath.Dir(configPath)
	if filepath.Base(dir) == "project" && filepath.Base(filepath.Dir(dir)) == ".lauren" {
		resource = dir
	}
	if main == "" {
		main = top
	}
	return Layout{
		Worktree: top, Runtime: filepath.Join(top, ".lauren", "runtime"),
		ConfigPath: configPath, ConfigMode: mode, MainWorktree: main,
		ResourceDir: resource, Entry: entry,
	}
}

func readProjectFile(path string) (Project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Project{}, fmt.Errorf("读取 lauren.project.json：%w", err)
	}
	var p Project
	if err := json.Unmarshal(b, &p); err != nil {
		return Project{}, fmt.Errorf("lauren.project.json 无法解析：%w", err)
	}
	if p.Schema == 0 {
		p.Schema = 1
	}
	if err := p.validate(); err != nil {
		return Project{}, err
	}
	return p, nil
}
