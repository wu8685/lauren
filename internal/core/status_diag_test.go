package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusWorkspaceAndMissingEntry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), sharedBody)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "README"), "legacy\n")
	gitInit(t, dir)
	st := must(t, dir, ExitOK, "status")
	ws := workspace(t, st)
	if ws["config_mode"] != "legacy" || canonPath(t, ws["worktree"].(string)) != canonPath(t, dir) {
		t.Fatalf("旧单层项目应仍能看出工作区: %#v", ws)
	}
	if canonPath(t, ws["worktree"].(string)) == canonPath(t, filepath.Dir(ws["config_path"].(string))) && ws["config_mode"] == "shared" {
		t.Fatal("旧配置不应被说成共享来源")
	}

	main, a, _ := setupSharedRepo(t)
	st = must(t, a, ExitOK, "status")
	ws = workspace(t, st)
	if ws["config_mode"] != "shared" || ws["entry"] != nil {
		t.Fatalf("未初始化的链接工作区应是 shared 且没有 entry: %#v", ws)
	}
	if canonPath(t, ws["worktree"].(string)) == canonPath(t, main) {
		t.Fatal("当前工作区不应和主工作区混成同一个根")
	}
	text := humanStatus(t, a)
	if !strings.Contains(text, "init-worktree") {
		t.Fatalf("没有本地入口时应提示初始化:\n%s", text)
	}
}

func TestStatusSeparatesFrozenPlan(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	must(t, a, ExitOK, "change", "create", "--goal", "冻结展示")
	proj := filepath.Join(main, ".lauren", "project", "lauren.project.json")
	before, err := os.ReadFile(proj)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(a, ".lauren", "runtime", "changes", must(t, a, ExitOK, "status")["id"].(string), "state.json")
	stateBefore, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(sharedResourceProject, "resource:checks/show.sh", "resource:checks/other.sh", 1)
	writeFile(t, proj, edited)
	st := must(t, a, ExitOK, "status")
	plan, ok := st["plan_view"].(map[string]any)
	if !ok || plan["source"] != "frozen" {
		t.Fatalf("应标出冻结计划，而不是说已经升级: %#v", st["plan_view"])
	}
	found := false
	for _, item := range plan["checks"].([]any) {
		cv := item.(map[string]any)
		if cv["name"] != "show" {
			continue
		}
		found = true
		argv := strings.Join(stringList(cv["argv"]), " ")
		projectArgv := strings.Join(stringList(cv["project_argv"]), " ")
		if !strings.Contains(argv, "show.sh") || !strings.Contains(projectArgv, "other.sh") {
			t.Fatalf("应同时看见冻结命令和当前定义: %#v", cv)
		}
	}
	if !found {
		t.Fatalf("计划里没有 show: %#v", plan)
	}
	text := humanStatus(t, a)
	if !strings.Contains(text, "没有自动升级") {
		t.Fatalf("人读输出应说明没有自动升级:\n%s", text)
	}
	stateAfter, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if string(stateBefore) != string(stateAfter) {
		t.Fatal("status 不能重写变更状态")
	}
	cfg, err := os.ReadFile(proj)
	if err != nil || string(cfg) != edited {
		t.Fatal("status 不能改配置")
	}
	_ = before
}

func TestStatusSkipPendingAndResourceNote(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	body := strings.Replace(sharedResourceProject, `"required": true`, `"required": true},{"name":"lint","layer":"static","gate":"self_verification","argv":["resource:checks/dep.txt"],"required": false`, 1)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), body)
	must(t, a, ExitOK, "change", "create", "--goal", "跳过", "--skip", "lint=只改文档")
	st := must(t, a, ExitOK, "status")
	if !planHasSkip(st, "lint", "只改文档") {
		t.Fatalf("应看见排除理由: %#v", st["plan_view"])
	}
	writeFile(t, filepath.Join(a, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	must(t, a, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, a, ExitOK, "change", "advance")
	must(t, a, ExitOK, "change", "plan", "propose", "--include", "lint")
	st = must(t, a, ExitOK, "status")
	if st["plan_pending"] == nil {
		t.Fatal("待确认的计划变更应留在状态里")
	}
	if !strings.Contains(humanStatus(t, a), "待确认") {
		t.Fatal("人读输出应提到待确认")
	}
	must(t, a, ExitOK, "check", "--name", "show")
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "dep.txt"), "changed\n")
	st = must(t, a, ExitOK, "status")
	note := planEvidence(st, "show")
	if !strings.Contains(note, "资源") {
		t.Fatalf("证据失效原因应写明资源: %s", note)
	}
	if _, err := os.Lstat(filepath.Join(a, "lauren.project.json")); err == nil {
		t.Fatal("只读 status 不应创建入口")
	}
}

func TestDirectoryResourceAndExitCode(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	if err := os.MkdirAll(filepath.Join(main, ".lauren", "project", "checks", "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(sharedResourceProject, `"resources": ["resource:checks/dep.txt"]`, `"resources": ["resource:checks/bundled"]`, 1)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), body)
	must(t, a, ExitOK, "change", "create", "--goal", "目录")
	ran := must(t, a, ExitCheck, "check", "--name", "show")
	if !strings.Contains(fmtNote(namedCheck(t, ran, "show")), "暂不支持目录") {
		t.Fatalf("目录不应说成不存在: %#v", namedCheck(t, ran, "show"))
	}
}

func TestMidRunKeepsExitCode(t *testing.T) {
	main, _, b := setupResourceRepo(t)
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "mutate.sh"), "#!/bin/sh\necho x > \"$LAUREN_RESOURCE/checks/dep.txt\"\nexit 0\n")
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), strings.Replace(sharedResourceProject, "resource:checks/show.sh", "resource:checks/mutate.sh", 1))
	must(t, b, ExitOK, "change", "create", "--goal", "退出码")
	ran := must(t, b, ExitCheck, "check", "--name", "show")
	got := namedCheck(t, ran, "show")
	if got["status"] != "fail" {
		t.Fatalf("执行中资源变了应记失败: %#v", got)
	}
	if got["exit_code"] != float64(0) {
		t.Fatalf("应保留真实退出码 0，而不是未退出: %#v", got["exit_code"])
	}
}

func planHasSkip(st map[string]any, name, reason string) bool {
	plan, _ := st["plan_view"].(map[string]any)
	if plan == nil {
		return false
	}
	for _, item := range plan["checks"].([]any) {
		cv := item.(map[string]any)
		if cv["name"] == name && cv["skip_reason"] == reason {
			return true
		}
	}
	return false
}

func planEvidence(st map[string]any, name string) string {
	plan, _ := st["plan_view"].(map[string]any)
	if plan == nil {
		return ""
	}
	for _, item := range plan["checks"].([]any) {
		cv := item.(map[string]any)
		if cv["name"] == name {
			note, _ := cv["evidence_note"].(string)
			return note
		}
	}
	return ""
}

func stringList(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}
