package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillbundle "github.com/wu8685/lauren/skills/lauren-workflow"
)

func gitPorcelain(t *testing.T, dir string) string {
	t.Helper()
	out, err := gitOut(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInitWorktreeBlankAndIdempotent(t *testing.T) {
	main, a, b := setupSharedRepo(t)
	os.Remove(filepath.Join(a, "marker.txt"))
	os.Remove(filepath.Join(b, "marker.txt"))
	before := gitPorcelain(t, a)

	must(t, a, ExitOK, "init-worktree")
	st := must(t, a, ExitOK, "status")
	ws := workspace(t, st)
	mode, _ := ws["config_mode"].(string)
	if mode != "symlink" && mode != "reference" {
		t.Fatalf("init 后应为 symlink 或 reference: %#v", ws)
	}
	if ws["entry"] == nil || ws["entry"] == "" {
		t.Fatalf("init 后应带 entry: %#v", ws)
	}
	if canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
		t.Fatalf("应仍指向唯一共享正文: %#v", ws)
	}
	entry := filepath.Join(a, "lauren.project.json")
	fi, err := os.Lstat(entry)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		body, _ := os.ReadFile(entry)
		if strings.Contains(string(body), "checks") {
			t.Fatalf("入口里不能放 checks: %s", body)
		}
	}
	must(t, a, ExitOK, "skills", "check")
	if strings.Contains(gitPorcelain(t, a), "lauren.project.json") || strings.Contains(gitPorcelain(t, a), "skills/lauren-workflow") {
		t.Fatalf("新入口应被本地排除，不应出现在 git status:\n%s", gitPorcelain(t, a))
	}
	if gitPorcelain(t, a) != before && strings.TrimSpace(before) == "" && strings.TrimSpace(gitPorcelain(t, a)) != "" {
		t.Fatalf("已跟踪文件不应被改动:\n%s", gitPorcelain(t, a))
	}

	exc, err := gitOut(a, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		t.Fatal(err)
	}
	excPath := strings.TrimSpace(exc)
	if !filepath.IsAbs(excPath) {
		excPath = filepath.Join(a, excPath)
	}
	first, err := os.ReadFile(excPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(first), "lauren-init-begin") != 1 {
		t.Fatalf("排除区块应只有一块:\n%s", first)
	}
	must(t, a, ExitOK, "init-worktree")
	must(t, a, ExitOK, "init-worktree")
	second, err := os.ReadFile(excPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) || strings.Count(string(second), "lauren-init-begin") != 1 {
		t.Fatalf("重复 init 不应再写排除规则:\n%s", second)
	}
	if _, err := os.Lstat(filepath.Join(b, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("初始化 A 不应给 B 建立入口")
	}
	if _, err := os.Lstat(filepath.Join(main, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("初始化链接工作区不应改主工作区根入口")
	}
}

func TestInitWorktreeKeepsFrozenChange(t *testing.T) {
	main, a, b := setupSharedRepo(t)
	ma := must(t, a, ExitOK, "change", "create", "--goal", "A 的变更")
	mb := must(t, b, ExitOK, "change", "create", "--goal", "B 的变更")
	idA := ma["id"].(string)
	idB := mb["id"].(string)
	writeFile(t, filepath.Join(a, ".lauren", "runtime", "changes", idA, "evidence.txt"), "只属于 A\n")
	must(t, a, ExitOK, "init-worktree")
	st := must(t, a, ExitOK, "status")
	if st["id"] != idA || st["goal"] != "A 的变更" {
		t.Fatalf("再初始化不能换掉进行中的变更: %#v", st)
	}
	raw, err := os.ReadFile(filepath.Join(a, ".lauren", "runtime", "changes", idA, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "./checks/unit.sh") {
		t.Fatalf("冻结计划应保留: %s", raw)
	}
	ev, err := os.ReadFile(filepath.Join(a, ".lauren", "runtime", "changes", idA, "evidence.txt"))
	if err != nil || string(ev) != "只属于 A\n" {
		t.Fatalf("不能动已有证据: %v %s", err, ev)
	}
	stB := must(t, b, ExitOK, "status")
	if stB["id"] != idB || stB["goal"] != "B 的变更" {
		t.Fatalf("不能克隆或改掉另一个工作区的状态: %#v", stB)
	}
	_ = main
}

func TestInitWorktreeRefusesUnmanaged(t *testing.T) {
	t.Run("existingFile", func(t *testing.T) {
		_, a, _ := setupSharedRepo(t)
		writeFile(t, filepath.Join(a, "lauren.project.json"), "{\"schema\":1,\"checks\":[]}\n")
		_, m, errb := runAt(t, a, "init-worktree")
		text := errText(m, errb)
		if !strings.Contains(text, "人工迁移") {
			t.Fatalf("已有接入文件不能覆盖: %s", text)
		}
		body, err := os.ReadFile(filepath.Join(a, "lauren.project.json"))
		if err != nil || !strings.Contains(string(body), "checks") {
			t.Fatalf("原文件应保持不动: %v %s", err, body)
		}
	})

	t.Run("brokenLink", func(t *testing.T) {
		_, a, _ := setupSharedRepo(t)
		if err := os.Symlink("missing.json", filepath.Join(a, "lauren.project.json")); err != nil {
			t.Fatal(err)
		}
		_, m, errb := runAt(t, a, "init-worktree")
		if !strings.Contains(errText(m, errb), "配置入口断了") {
			t.Fatalf("断链应沿用已有诊断: %s", errText(m, errb))
		}
	})

	t.Run("missingShared", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "README"), "x\n")
		gitInit(t, dir)
		_, m, errb := runAt(t, dir, "init-worktree")
		text := errText(m, errb)
		if !strings.Contains(text, "未初始化") && !strings.Contains(text, "没有 .lauren/project/lauren.project.json") && !strings.Contains(text, "无法定位主工作区") {
			t.Fatalf("没有共享配置时应给出明确诊断: %s", text)
		}
	})

	t.Run("wrongRepo", func(t *testing.T) {
		other := t.TempDir()
		writeFile(t, filepath.Join(other, "README"), "o\n")
		gitInit(t, other)
		_, m, errb := runAt(t, other, "init-worktree")
		text := errText(m, errb)
		if !strings.Contains(text, "未初始化") && !strings.Contains(text, "没有 .lauren/project/lauren.project.json") && !strings.Contains(text, "无法定位主工作区") && !strings.Contains(text, "不属于") {
			t.Fatalf("别的仓库不能初始化成功: %s", text)
		}
	})

	t.Run("readOnlyThenRetry", func(t *testing.T) {
		_, a, _ := setupSharedRepo(t)
		if err := os.Chmod(a, 0o555); err != nil {
			t.Fatal(err)
		}
		_, m, errb := runAt(t, a, "init-worktree")
		if errText(m, errb) == "" {
			t.Fatal("只读目录应失败")
		}
		if _, err := os.Stat(filepath.Join(a, ".lauren", "init.json")); err == nil {
			t.Fatal("失败不能留下成功标记")
		}
		if err := os.Chmod(a, 0o755); err != nil {
			t.Fatal(err)
		}
		must(t, a, ExitOK, "init-worktree")
		if _, err := os.Stat(filepath.Join(a, ".lauren", "init.json")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUndoKeepsOtherWorktreeExcluded(t *testing.T) {
	_, a, b := setupSharedRepo(t)
	os.Remove(filepath.Join(b, "marker.txt"))
	must(t, a, ExitOK, "init-worktree")
	must(t, b, ExitOK, "init-worktree")
	writeFile(t, filepath.Join(b, "svc", "lauren.project.json"), "{}\n")
	if gitIgnored(t, b, "svc/lauren.project.json") {
		t.Fatal("排除规则不应藏起子目录里的 lauren.project.json")
	}
	if !gitIgnored(t, b, "lauren.project.json") {
		t.Fatal("工作区根的入口应被排除")
	}
	must(t, b, ExitOK, "change", "create", "--goal", "B 保持")
	before := must(t, b, ExitOK, "status")["candidate"].(map[string]any)
	must(t, a, ExitOK, "init-worktree", "--undo")
	if !gitIgnored(t, b, "lauren.project.json") {
		t.Fatal("撤销 A 后，B 的入口不应变成未跟踪文件")
	}
	if gitIgnored(t, b, "svc/lauren.project.json") {
		t.Fatal("撤销后仍不应藏起子目录里的同名文件")
	}
	after := must(t, b, ExitOK, "status")["candidate"].(map[string]any)
	if before["hash"] != after["hash"] || before["dirty"] != after["dirty"] {
		t.Fatalf("撤销 A 不应改变 B 的候选: before=%#v after=%#v", before, after)
	}
}

func gitIgnored(t *testing.T, dir, rel string) bool {
	t.Helper()
	_, err := gitOut(dir, "check-ignore", "-q", "--", rel)
	return err == nil
}

func TestInitWorktreeEmbeddedSkillOutsideRepo(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoSkill := wd
	for {
		if _, err := os.Stat(filepath.Join(repoSkill, "skills", "lauren-workflow", "SKILL.md")); err == nil {
			break
		}
		parent := filepath.Dir(repoSkill)
		if parent == repoSkill {
			t.Fatal("找不到仓库里的 Skill")
		}
		repoSkill = parent
	}
	diskSkill, err := os.ReadFile(filepath.Join(repoSkill, "skills", "lauren-workflow", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	diskPlugin, err := os.ReadFile(filepath.Join(repoSkill, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(diskSkill) != string(skillbundle.SkillMD) || string(diskPlugin) != string(skillbundle.PluginJSON) {
		t.Fatal("打进二进制的 Skill 或插件清单和仓库文件不一致")
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	main, a, _ := setupSharedRepo(t)
	os.Remove(filepath.Join(a, "marker.txt"))
	must(t, a, ExitOK, "init-worktree")
	must(t, a, ExitOK, "skills", "check")
	got, err := os.ReadFile(filepath.Join(main, ".lauren", "project", "skills", "lauren-workflow", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(skillbundle.SkillMD) {
		t.Fatal("仓库外执行也应写入二进制里的 Skill")
	}
}

func TestInitWorktreePreviewUndoAndMetadata(t *testing.T) {
	_, a, _ := setupSharedRepo(t)
	code, m, errb := runAt(t, a, "init-worktree", "--preview")
	if code != ExitOK {
		t.Fatalf("预览应成功: %s", errText(m, errb))
	}
	if _, err := os.Lstat(filepath.Join(a, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("预览不能创建入口")
	}
	must(t, a, ExitOK, "init-worktree")
	meta, err := os.ReadFile(filepath.Join(a, ".lauren", "init.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), `"verified"`) || strings.Contains(string(meta), `"status": "pass"`) || !strings.Contains(string(meta), "不是测试通过") {
		t.Fatalf("元数据不能当成通过或完成: %s", meta)
	}
	ch := must(t, a, ExitOK, "change", "create", "--goal", "保留")
	id := ch["id"].(string)
	must(t, a, ExitOK, "init-worktree", "--undo")
	if _, err := os.Lstat(filepath.Join(a, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("撤销应只去掉受管入口")
	}
	if _, err := os.Stat(filepath.Join(a, ".lauren", "runtime", "changes", id, "state.json")); err != nil {
		t.Fatal("撤销不能删除已有变更")
	}
}
