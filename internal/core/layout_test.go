package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这些夹具显式关掉 Red。理由：这里只测配置发现和冻结计划，不重放分类失败。
const sharedBody = `{
  "schema": 1,
  "environments": ["development", "integration", "staging", "production"],
  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "red_gate": "off", "promote_threshold": 2},
  "checks": [
    {"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true},
    {"name": "lint", "layer": "static", "gate": "self_verification", "argv": ["./checks/lint.sh"], "required": false}
  ],
  "requirements": []
}`

func canonPath(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(got)
}

func workspace(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	ws, ok := m["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("status 没有标明工作区和配置来源: %#v", m)
	}
	return ws
}

func setupSharedRepo(t *testing.T) (main, a, b string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "仓库 根")
	main = filepath.Join(base, "main")
	a = filepath.Join(base, "功能 A")
	b = filepath.Join(base, "功能 B")
	writeFile(t, filepath.Join(main, "checks", "unit.sh"), "#!/bin/sh\necho \"marker=$(cat marker.txt)\"\npwd\n")
	writeFile(t, filepath.Join(main, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(main, "marker.txt"), "MAIN\n")
	writeFile(t, filepath.Join(main, "README"), "shared\n")
	gitInit(t, main)
	// 共享正文留在主工作区且不进提交，链接工作区里不会各自带一份。
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), sharedBody)
	runGit(t, main, "worktree", "add", "-b", "feature-a", a)
	runGit(t, main, "worktree", "add", "-b", "feature-b", b)
	writeFile(t, filepath.Join(a, "marker.txt"), "A\n")
	writeFile(t, filepath.Join(b, "marker.txt"), "B\n")
	return main, a, b
}

func TestSharedConfigAcrossWorktrees(t *testing.T) {
	main, a, b := setupSharedRepo(t)
	sub := filepath.Join(a, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	st := must(t, sub, ExitOK, "status")
	ws := workspace(t, st)
	if canonPath(t, ws["worktree"].(string)) != canonPath(t, a) {
		t.Fatalf("子目录应发现工作区 A，得到 %v", ws["worktree"])
	}
	if canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
		t.Fatalf("应使用主工作区的一份共享配置: %#v", ws)
	}
	if ws["config_mode"] != "shared" || ws["entry"] != nil {
		t.Fatalf("没有本地入口时应自动发现共享配置: %#v", ws)
	}
	if filepath.Clean(ws["runtime"].(string)) != filepath.Join(canonPath(t, a), ".lauren", "runtime") {
		t.Fatalf("runtime 应留在当前工作区: %v", ws["runtime"])
	}

	must(t, a, ExitOK, "change", "create", "--goal", "A")
	must(t, b, ExitOK, "change", "create", "--goal", "B")
	_, m, errb := runAt(t, a, "change", "create", "--goal", "第二个")
	if !strings.Contains(errText(m, errb), "这个工作区已有进行中的变更") {
		t.Fatalf("同一工作区不能有两个进行中变更: %s", errText(m, errb))
	}
	if _, err := os.Stat(filepath.Join(a, ".lauren", "runtime", "lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, ".lauren", "runtime", "lock")); err != nil {
		t.Fatal(err)
	}

	ra := must(t, a, ExitOK, "check", "--name", "unit")
	rb := must(t, b, ExitOK, "check", "--name", "unit")
	ca := namedCheck(t, ra, "unit")
	cb := namedCheck(t, rb, "unit")
	if canonPath(t, ca["cwd"].(string)) != canonPath(t, a) || canonPath(t, cb["cwd"].(string)) != canonPath(t, b) {
		t.Fatalf("检查 cwd 应是各自工作区: %#v %#v", ca["cwd"], cb["cwd"])
	}
	logA, err := os.ReadFile(ca["log_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	logB, err := os.ReadFile(cb["log_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logA), "marker=A") || strings.Contains(string(logA), "marker=MAIN") || !strings.Contains(string(logB), "marker=B") {
		t.Fatalf("不能把主工作区当成 A/B 的执行目录:\nA:%s\nB:%s", logA, logB)
	}
	if strings.Contains(ca["log_path"].(string), filepath.Clean(main)) || strings.Contains(cb["log_path"].(string), filepath.Clean(b)+string(filepath.Separator)+".lauren") && strings.Contains(ca["log_path"].(string), filepath.Clean(b)) {
		t.Fatalf("日志应写在各自 runtime: %s %s", ca["log_path"], cb["log_path"])
	}
	hashA := must(t, a, ExitOK, "status")["candidate"].(map[string]any)["hash"]
	writeFile(t, filepath.Join(main, "only-main.txt"), "main-only\n")
	if must(t, a, ExitOK, "status")["candidate"].(map[string]any)["hash"] != hashA {
		t.Fatal("主工作区的文件变化不能改变 A 的候选")
	}
	writeFile(t, filepath.Join(a, "only-a.txt"), "a-only\n")
	if must(t, a, ExitOK, "status")["candidate"].(map[string]any)["hash"] == hashA {
		t.Fatal("A 自己的文件变化应改变 A 的候选")
	}

	// B 用软链接入口，仍然指向同一份正文。
	target, err := filepath.Rel(b, filepath.Join(main, ".lauren", "project", "lauren.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(b, "lauren.project.json")); err != nil {
		t.Fatal(err)
	}
	ws = workspace(t, must(t, b, ExitOK, "status"))
	if ws["config_mode"] != "symlink" || canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
		t.Fatalf("软链接入口应指向共享正文: %#v", ws)
	}
}

func TestLegacyAndExplicitReference(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), sharedBody)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "README"), "legacy\n")
	gitInit(t, dir)
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := workspace(t, must(t, sub, ExitOK, "status"))
	if ws["config_mode"] != "legacy" || canonPath(t, ws["worktree"].(string)) != canonPath(t, dir) {
		t.Fatalf("旧的根目录配置应继续可用: %#v", ws)
	}
	legacyMain := filepath.Join(t.TempDir(), "legacy-main")
	if err := os.MkdirAll(legacyMain, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(legacyMain, "README"), "legacy\n")
	gitInit(t, legacyMain)
	writeFile(t, filepath.Join(legacyMain, "lauren.project.json"), sharedBody)
	legacyLinked := filepath.Join(filepath.Dir(legacyMain), "legacy-linked")
	runGit(t, legacyMain, "worktree", "add", "-b", "feature", legacyLinked)
	_, m, errb := runAt(t, legacyLinked, "status")
	if !strings.Contains(errText(m, errb), "未初始化") {
		t.Fatalf("未跟踪的旧单层配置不应自动变成其他 Worktree 的运行根: %s", errText(m, errb))
	}

	main, _, _ := setupSharedRepo(t)
	base := filepath.Dir(main)
	refWT := filepath.Join(base, "引用")
	runGit(t, main, "worktree", "add", "-b", "feature-ref", refWT)
	rel, err := filepath.Rel(refWT, filepath.Join(main, ".lauren", "project", "lauren.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(refWT, "lauren.project.json"), `{"schema":1,"shared_config":"`+rel+`"}`+"\n")
	ws = workspace(t, must(t, refWT, ExitOK, "status"))
	if ws["config_mode"] != "reference" || canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
		t.Fatalf("引用文件应载入共享正文而不是深合并: %#v", ws)
	}
	writeFile(t, filepath.Join(refWT, "lauren.project.json"), `{"schema":1,"shared_config":"`+rel+`","checks":[]}`+"\n")
	_, m, errb = runAt(t, refWT, "status")
	if !strings.Contains(errText(m, errb), "不能覆盖") {
		t.Fatalf("Worktree 入口不能删掉或覆盖共享 checks: %s", errText(m, errb))
	}
}

func TestConfigDiagnostics(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "lauren.project.json"), strings.Replace(sharedBody, "unit", "local", 1))
		writeFile(t, filepath.Join(dir, ".lauren", "project", "lauren.project.json"), sharedBody)
		writeFile(t, filepath.Join(dir, "README"), "x\n")
		gitInit(t, dir)
		_, m, errb := runAt(t, dir, "status")
		if !strings.Contains(errText(m, errb), "配置冲突") {
			t.Fatalf("本地和共享同时存在时应拒绝，而不是静默选一份: %s", errText(m, errb))
		}
	})

	t.Run("missingAndBroken", func(t *testing.T) {
		main, a, _ := setupSharedRepo(t)
		missing := filepath.Join(filepath.Dir(main), "gone.json")
		writeFile(t, filepath.Join(a, "lauren.project.json"), `{"schema":1,"shared_config":"`+missing+`"}`+"\n")
		_, m, errb := runAt(t, a, "status")
		if !strings.Contains(errText(m, errb), "共享配置不存在") || strings.Contains(errText(m, errb), "marker") {
			t.Fatalf("引用丢失不能回退到自动发现: %s", errText(m, errb))
		}
		os.Remove(filepath.Join(a, "lauren.project.json"))
		if err := os.Symlink("missing.json", filepath.Join(a, "lauren.project.json")); err != nil {
			t.Fatal(err)
		}
		_, m, errb = runAt(t, a, "status")
		if !strings.Contains(errText(m, errb), "配置入口断了") {
			t.Fatalf("断链应有诊断: %s", errText(m, errb))
		}
	})

	t.Run("outsideAndAncestor", func(t *testing.T) {
		main, a, _ := setupSharedRepo(t)
		other := t.TempDir()
		writeFile(t, filepath.Join(other, "lauren.project.json"), strings.Replace(sharedBody, `"name": "unit"`, `"name": "other-repo"`, 1))
		writeFile(t, filepath.Join(other, "README"), "o\n")
		gitInit(t, other)
		writeFile(t, filepath.Join(a, "lauren.project.json"), `{"schema":1,"shared_config":"`+filepath.Join(other, "lauren.project.json")+`"}`+"\n")
		_, m, errb := runAt(t, a, "status")
		if !strings.Contains(errText(m, errb), "当前 Git 仓库之外") || strings.Contains(errText(m, errb), "other-repo") {
			t.Fatalf("不能读取仓库外的配置: %s", errText(m, errb))
		}

		outer := t.TempDir()
		writeFile(t, filepath.Join(outer, "lauren.project.json"), strings.Replace(sharedBody, `"name": "unit"`, `"name": "outer-only"`, 1))
		writeFile(t, filepath.Join(outer, "README"), "outer\n")
		gitInit(t, outer)
		inner := filepath.Join(outer, "inner")
		if err := os.MkdirAll(inner, 0o755); err != nil {
			t.Fatal(err)
		}
		runGit(t, inner, "init", "-b", "main")
		_, m, errb = runAt(t, inner, "status")
		text := errText(m, errb)
		if !strings.Contains(text, "未初始化") || strings.Contains(text, "outer-only") {
			t.Fatalf("不能读祖先仓库的配置: %s", text)
		}
		_ = main
	})

	t.Run("separateGitDirNeedsExplicitSource", func(t *testing.T) {
		base := filepath.Join(t.TempDir(), "分离 git")
		gitDir := filepath.Join(base, "repo.git")
		main := filepath.Join(base, "main wt")
		linked := filepath.Join(base, "linked")
		if err := os.MkdirAll(main, 0o755); err != nil {
			t.Fatal(err)
		}
		runGit(t, main, "init", "--separate-git-dir="+gitDir, "-b", "main")
		writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), sharedBody)
		writeFile(t, filepath.Join(main, "checks", "unit.sh"), "#!/bin/sh\necho ok\n")
		writeFile(t, filepath.Join(main, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
		writeFile(t, filepath.Join(main, "README"), "sep\n")
		runGit(t, main, "add", "README", "checks")
		runGit(t, main, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "init")
		writeFile(t, filepath.Join(base, ".lauren", "project", "lauren.project.json"), strings.Replace(sharedBody, `"name": "unit"`, `"name": "parent-decoy"`, 1))
		runGit(t, main, "worktree", "add", "-b", "feature", linked)
		_, m, errb := runAt(t, linked, "status")
		text := errText(m, errb)
		if !strings.Contains(text, "无法定位主工作区") || strings.Contains(text, "parent-decoy") {
			t.Fatalf("separate git-dir 不能把 git 目录的父目录当成主工作区: %s", text)
		}
		ws := workspace(t, must(t, main, ExitOK, "status"))
		if ws["config_mode"] != "shared" || canonPath(t, ws["worktree"].(string)) != canonPath(t, main) {
			t.Fatalf("主检出自身仍应找到自己的共享配置: %#v", ws)
		}
		rel, err := filepath.Rel(linked, filepath.Join(main, ".lauren", "project", "lauren.project.json"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(linked, "lauren.project.json"), `{"schema":1,"shared_config":"`+rel+`"}`+"\n")
		ws = workspace(t, must(t, linked, ExitOK, "status"))
		if canonPath(t, ws["worktree"].(string)) != canonPath(t, linked) || canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
			t.Fatalf("显式注册后应使用真正的主检出配置: %#v", ws)
		}
	})

	t.Run("noConfig", func(t *testing.T) {
		dir := t.TempDir()
		runGit(t, dir, "init", "-b", "main")
		_, m, errb := runAt(t, dir, "status")
		if !strings.Contains(errText(m, errb), "未初始化") {
			t.Fatalf("没有配置时应识别为未初始化: %s", errText(m, errb))
		}
	})
}

func TestSharedConfigKeepsFrozenPlan(t *testing.T) {
	main, a, b := setupSharedRepo(t)
	writeFile(t, filepath.Join(a, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	must(t, main, ExitOK, "change", "create", "--goal", "主工作区")
	must(t, main, ExitOK, "check", "--name", "unit")
	body := strings.Replace(sharedBody, `"promote_threshold": 2`, `"promote_threshold": 3`, 1)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), body)
	if namedCheck(t, must(t, main, ExitOK, "status"), "unit")["effective_status"] != "pass" {
		t.Fatal("只改共享配置不能把主工作区已有通过变成过期")
	}

	must(t, a, ExitOK, "change", "create", "--goal", "在途")
	must(t, a, ExitOK, "check", "--name", "unit")
	must(t, a, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	if must(t, a, ExitOK, "change", "advance")["stage"] != StagePlan {
		t.Fatal("应离开明确需求，计划从此锁定")
	}
	edited := strings.Replace(sharedBody, `"./checks/unit.sh"`, `"./checks/gone.sh"`, 1)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), edited)
	ran := must(t, a, ExitOK, "check", "--name", "unit")
	if namedCheck(t, ran, "unit")["status"] != "pass" || namedCheck(t, must(t, a, ExitOK, "status"), "unit")["effective_status"] != "pass" {
		t.Fatal("共享 checks 变更不能改写在途冻结计划")
	}
	_, m, errb := runAt(t, a, "change", "plan", "skip", "--name", "lint", "--reason", "太晚")
	if !strings.Contains(errText(m, errb), "不能直接改计划") {
		t.Fatalf("过了明确需求仍要所有者确认: %s", errText(m, errb))
	}
	if must(t, a, ExitOK, "change", "plan", "propose", "--skip", "lint=文档")["plan_pending"] == nil {
		t.Fatal("应留下待确认的计划变更")
	}
	_, m, errb = runAt(t, a, "change", "plan", "confirm")
	if !strings.Contains(errText(m, errb), "owner-confirmed") {
		t.Fatalf("确认必须是所有者: %s", errText(m, errb))
	}
	must(t, a, ExitOK, "change", "plan", "confirm", "--owner-confirmed")
	if namedCheck(t, must(t, a, ExitOK, "status"), "unit")["effective_status"] == "pass" {
		t.Fatal("所有者确认后才作废证据，不能在改共享配置时悄悄作废或升级")
	}

	must(t, b, ExitOK, "change", "create", "--goal", "新变更")
	code, _, _ := runAt(t, b, "check", "--name", "unit")
	if code == ExitOK {
		t.Fatal("新变更应冻住更新后的命令，而不是继续跑旧的 unit.sh")
	}
}

func TestPathAlias(t *testing.T) {
	realBase := filepath.Join(t.TempDir(), "真实 路径")
	main := filepath.Join(realBase, "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "别名 路径")
	if err := os.Symlink(main, alias); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(main, "lauren.project.json"), sharedBody)
	writeFile(t, filepath.Join(main, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(main, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(main, "README"), "alias\n")
	gitInit(t, alias)
	sub := filepath.Join(alias, "子目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	viaAlias := workspace(t, must(t, sub, ExitOK, "status"))
	viaReal := workspace(t, must(t, filepath.Join(main, "子目录"), ExitOK, "status"))
	if viaAlias["worktree"] != viaReal["worktree"] || canonPath(t, viaAlias["worktree"].(string)) != canonPath(t, main) {
		t.Fatalf("路径别名不能看成两个工作区: alias=%#v real=%#v", viaAlias, viaReal)
	}
}
