package core

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	initBegin = "# lauren-init-begin"
	initEnd   = "# lauren-init-end"
)

type worktreeInitMeta struct {
	Schema       int      `json:"schema"`
	Kind         string   `json:"kind"`
	Worktree     string   `json:"worktree"`
	SharedConfig string   `json:"shared_config"`
	Managed      []string `json:"managed"`
	Note         string   `json:"note"`
}

func (a *app) cmdInitWorktree(args []string) int {
	fs := flag.NewFlagSet("init-worktree", flag.ContinueOnError)
	fs.SetOutput(a.err)
	preview := fs.Bool("preview", false, "只列出会改的路径")
	undo := fs.Bool("undo", false, "只撤销受管入口")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *preview && *undo {
		return a.fail(ExitUsage, "不能同时预览和撤销")
	}
	start := a.root
	if fs.NArg() > 0 {
		start = fs.Arg(0)
	}
	if strings.TrimSpace(start) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return a.fail(ExitInternal, err.Error())
		}
		start = wd
	}
	top, err := gitToplevel(start)
	if err != nil {
		return a.fail(ExitUsage, "目标路径不是 git 工作区")
	}
	if *undo {
		return a.undoWorktreeInit(top)
	}
	plan, err := planWorktreeInit(top)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if *preview {
		if a.json {
			return a.emit(ExitOK, map[string]any{"ok": true, "command": "init-worktree --preview", "paths": plan.managed})
		}
		for _, rel := range plan.managed {
			fmt.Fprintln(a.out, rel)
		}
		fmt.Fprintln(a.out, "info/exclude")
		return ExitOK
	}
	if err := applyWorktreeInit(plan); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if a.json {
		return a.emit(ExitOK, map[string]any{
			"ok": true, "command": "init-worktree", "worktree": plan.worktree,
			"entry": plan.entry, "config_mode": "symlink",
		})
	}
	fmt.Fprintf(a.out, "已接入 %s\n配置入口: %s\n", plan.worktree, plan.entry)
	return ExitOK
}

type plannedInit struct {
	worktree string
	shared   string
	entry    string
	links    []linkPlan
	managed  []string
	exclude  []string
}

type linkPlan struct {
	rel    string
	target string
}

func planWorktreeInit(top string) (plannedInit, error) {
	shared, err := sharedConfigForInit(top)
	if err != nil {
		return plannedInit{}, err
	}
	if !sameRepo(top, filepath.Dir(shared)) {
		return plannedInit{}, fmt.Errorf("配置在当前 Git 仓库之外：%s", shared)
	}
	skillDir, plugin := sharedResourcePaths(shared)
	links := []linkPlan{
		{rel: "lauren.project.json", target: shared},
		{rel: filepath.Join("skills", "lauren-workflow"), target: skillDir},
		{rel: filepath.Join(".agents", "skills", "lauren-workflow"), target: skillDir},
		{rel: filepath.Join(".claude", "skills", "lauren-workflow"), target: skillDir},
		{rel: filepath.Join(".claude-plugin", "plugin.json"), target: plugin},
	}
	var managed []string
	for _, link := range links {
		if err := checkLink(top, link); err != nil {
			return plannedInit{}, err
		}
		managed = append(managed, filepath.ToSlash(link.rel))
	}
	exclude := []string{
		".lauren/runtime/",
		".lauren/reports/",
		".lauren/init.json",
		"lauren.project.json",
		"skills/lauren-workflow",
		".agents/skills/lauren-workflow",
		".claude/skills/lauren-workflow",
		".claude-plugin/plugin.json",
	}
	return plannedInit{
		worktree: top, shared: shared, entry: filepath.Join(top, "lauren.project.json"),
		links: links, managed: managed, exclude: exclude,
	}, nil
}

func sharedConfigForInit(top string) (string, error) {
	entry := filepath.Join(top, "lauren.project.json")
	fi, err := os.Lstat(entry)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if _, ev := filepath.EvalSymlinks(entry); ev != nil {
				return "", fmt.Errorf("配置入口断了：%s", entry)
			}
		} else if fi.Mode().IsRegular() {
			if ok, target := matchingReference(top, entry); ok {
				return target, nil
			}
			return "", fmt.Errorf("这是已有的接入文件，请人工迁移，不会删除或隐藏")
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	lay, _, locErr := locate(top)
	if locErr != nil {
		return "", locErr
	}
	if lay.ConfigMode == modeLegacy {
		return "", fmt.Errorf("这是已有的接入文件，请人工迁移，不会删除或隐藏")
	}
	return lay.ConfigPath, nil
}

func matchingReference(top, entry string) (bool, string) {
	b, err := os.ReadFile(entry)
	if err != nil || !isConfigRef(b) {
		return false, ""
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var ref struct {
		Schema       int    `json:"schema"`
		SharedConfig string `json:"shared_config"`
	}
	if err := dec.Decode(&ref); err != nil || strings.TrimSpace(ref.SharedConfig) == "" {
		return false, ""
	}
	target := ref.SharedConfig
	if !filepath.IsAbs(target) {
		target = filepath.Join(top, target)
	}
	if st, err := os.Stat(target); err != nil || !st.Mode().IsRegular() {
		return false, ""
	}
	canon, err := canonical(target)
	if err != nil {
		return false, ""
	}
	return true, canon
}

func sharedResourcePaths(sharedFile string) (string, string) {
	projectDir := filepath.Dir(sharedFile)
	return filepath.Join(projectDir, "skills", "lauren-workflow"), filepath.Join(projectDir, "plugin.json")
}

func ensureSharedSkill(sharedFile string) error {
	skillDir, plugin := sharedResourcePaths(sharedFile)
	if st, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil || st.IsDir() {
		src, serr := bundledSkillDir()
		if serr != nil {
			return serr
		}
		if err := copyTree(src, skillDir); err != nil {
			return fmt.Errorf("无法写入共享 Skill：%w", err)
		}
	}
	if _, err := os.Stat(plugin); err != nil {
		srcDir, serr := bundledSkillDir()
		if serr != nil {
			return serr
		}
		pluginSrc := filepath.Join(filepath.Dir(filepath.Dir(srcDir)), ".claude-plugin", "plugin.json")
		if err := copyFile(pluginSrc, plugin); err != nil {
			return fmt.Errorf("无法写入共享插件清单：%w", err)
		}
	}
	return nil
}

func bundledSkillDir() (string, error) {
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for {
			cand := filepath.Join(dir, "skills", "lauren-workflow", "SKILL.md")
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return filepath.Join(dir, "skills", "lauren-workflow"), nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("找不到 lauren 自带的 Skill。请在 lauren 仓库里执行，或把 Skill 放到共享目录 .lauren/project/skills/lauren-workflow/")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func checkLink(top string, link linkPlan) error {
	path := filepath.Join(top, link.rel)
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if link.rel == "lauren.project.json" && fi.Mode()&os.ModeSymlink != 0 {
		if _, ev := filepath.EvalSymlinks(path); ev != nil {
			return fmt.Errorf("配置入口断了：%s", path)
		}
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		rel, err := filepath.Rel(filepath.Dir(path), link.target)
		if err == nil {
			if got, rerr := os.Readlink(path); rerr == nil && got == rel {
				return nil
			}
		}
		if resolved, ev := filepath.EvalSymlinks(path); ev == nil && samePath(resolved, link.target) {
			return nil
		}
		return fmt.Errorf("不能覆盖已有入口 %s", filepath.ToSlash(link.rel))
	}
	if link.rel == "lauren.project.json" {
		return fmt.Errorf("这是已有的接入文件，请人工迁移，不会删除或隐藏")
	}
	return fmt.Errorf("已有同名文件 %s，请人工迁移，不会删除或隐藏", filepath.ToSlash(link.rel))
}

func applyWorktreeInit(plan plannedInit) error {
	if err := ensureSharedSkill(plan.shared); err != nil {
		return err
	}
	for _, link := range plan.links {
		if err := ensureSymlink(plan.worktree, link); err != nil {
			return err
		}
	}
	if err := ensureExclude(plan.worktree, plan.exclude); err != nil {
		return err
	}
	meta := worktreeInitMeta{
		Schema: 1, Kind: "worktree-init", Worktree: plan.worktree, SharedConfig: plan.shared,
		Managed: plan.managed,
		Note:    "只记录接入，不是测试通过、身份验证或开发完成",
	}
	return writeAtomicJSON(filepath.Join(plan.worktree, ".lauren", "init.json"), meta)
}

func ensureSymlink(top string, link linkPlan) error {
	path := filepath.Join(top, link.rel)
	rel, err := filepath.Rel(filepath.Dir(path), link.target)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if got, rerr := os.Readlink(path); rerr == nil && got == rel {
				return nil
			}
			if resolved, ev := filepath.EvalSymlinks(path); ev == nil && samePath(resolved, link.target) {
				return nil
			}
		}
		return fmt.Errorf("不能覆盖已有入口 %s", filepath.ToSlash(link.rel))
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("无法写入 %s：%w", path, err)
	}
	if err := os.Symlink(rel, path); err != nil {
		return fmt.Errorf("无法写入 %s：%w", path, err)
	}
	return nil
}

func gitExcludeFile(worktree string) (string, error) {
	out, err := gitOut(worktree, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	return p, nil
}

func ensureExclude(worktree string, lines []string) error {
	path, err := gitExcludeFile(worktree)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("无法写入 %s：%w", path, err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	block := initBegin + "\n" + strings.Join(lines, "\n") + "\n" + initEnd + "\n"
	next := upsertExcludeBlock(string(existing), block)
	if next == string(existing) {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}

func upsertExcludeBlock(existing, block string) string {
	start := strings.Index(existing, initBegin)
	if start < 0 {
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			existing += "\n"
		}
		return existing + block
	}
	rest := existing[start:]
	endRel := strings.Index(rest, initEnd)
	endAbs := len(existing)
	if endRel >= 0 {
		endAbs = start + endRel + len(initEnd)
		if endAbs < len(existing) && existing[endAbs] == '\n' {
			endAbs++
		}
	}
	return existing[:start] + block + existing[endAbs:]
}

func (a *app) undoWorktreeInit(top string) int {
	metaPath := filepath.Join(top, ".lauren", "init.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return a.fail(ExitUsage, "没有可撤销的初始化记录")
	}
	var meta worktreeInitMeta
	if err := json.Unmarshal(b, &meta); err != nil || meta.Kind != "worktree-init" {
		return a.fail(ExitUsage, "初始化记录无法识别，不会删除其他文件")
	}
	for _, rel := range meta.Managed {
		path := filepath.Join(top, filepath.FromSlash(rel))
		fi, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return a.fail(ExitUsage, err.Error())
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return a.fail(ExitUsage, "不会删除非受管文件 "+rel)
		}
		if err := os.Remove(path); err != nil {
			return a.fail(ExitUsage, "无法去掉入口 "+rel)
		}
	}
	if err := removeExcludeBlock(top); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return a.fail(ExitUsage, err.Error())
	}
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "init-worktree --undo", "worktree": top})
	}
	fmt.Fprintln(a.out, "已撤销受管入口")
	return ExitOK
}

func removeExcludeBlock(worktree string) error {
	path, err := gitExcludeFile(worktree)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	next := upsertExcludeBlock(string(existing), "")
	if next == string(existing) {
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}
