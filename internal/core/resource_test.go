package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedScriptUsesWorktreeCwd(t *testing.T) {
	main, a, b := setupResourceRepo(t)
	must(t, a, ExitOK, "change", "create", "--goal", "A")
	must(t, b, ExitOK, "change", "create", "--goal", "B")
	ra := must(t, a, ExitOK, "check", "--name", "show")
	rb := must(t, b, ExitOK, "check", "--name", "show")
	la := readLog(t, namedCheck(t, ra, "show"))
	lb := readLog(t, namedCheck(t, rb, "show"))
	if !strings.Contains(la, "fixture=A") || strings.Contains(la, "fixture=MAIN") || !strings.Contains(lb, "fixture=B") {
		t.Fatalf("共享脚本应读各自工作区，而不是主工作区:\nA:%s\nB:%s", la, lb)
	}
	if !strings.Contains(la, "cwd="+canonPath(t, a)) && !strings.Contains(la, "cwd="+a) {
		t.Fatalf("cwd 应是工作区 A: %s", la)
	}
	if namedCheck(t, ra, "show")["cwd"] == nil || canonPath(t, namedCheck(t, ra, "show")["cwd"].(string)) != canonPath(t, a) {
		t.Fatalf("记录的 cwd 不是 A: %#v", namedCheck(t, ra, "show")["cwd"])
	}
	_ = main
}

func TestResourceDigestNotFingerprint(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	writeFile(t, filepath.Join(a, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(a, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(a, "artifacts", "impl.json"), `{"summary":"s"}`)
	must(t, a, ExitOK, "change", "create", "--goal", "摘要")
	must(t, a, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, a, ExitOK, "change", "advance")
	must(t, a, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, a, ExitOK, "change", "advance")
	must(t, a, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, a, ExitOK, "check", "--name", "show")
	st := must(t, a, ExitOK, "status")
	hash := st["candidate"].(map[string]any)["hash"]
	if namedCheck(t, st, "show")["effective_status"] != "pass" {
		t.Fatal("第一次应通过")
	}
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "dep.txt"), "dep-v2\n")
	st = must(t, a, ExitOK, "status")
	if st["candidate"].(map[string]any)["hash"] != hash {
		t.Fatal("共享脚本内容变化不应改变候选指纹")
	}
	if namedCheck(t, st, "show")["effective_status"] == "pass" {
		t.Fatal("声明依赖的字节变了，旧通过不能继续有效")
	}
	note := fmtNote(namedCheck(t, st, "show"))
	if !strings.Contains(note, "资源") || !strings.Contains(note, "dep.txt") {
		t.Fatalf("状态应说明资源来源: %s", note)
	}
	nx := must(t, a, ExitOK, "next")
	if wantsAdvance(nx) {
		t.Fatal("next 不应在资源过期时建议推进")
	}
	_, m, errb := runAt(t, a, "change", "advance")
	adv := errText(m, errb)
	if !strings.Contains(adv, "资源") || !strings.Contains(adv, "dep.txt") {
		t.Fatalf("advance 应给出同样的资源原因: %s", adv)
	}

	writeFile(t, filepath.Join(main, ".lauren", "project", "skills", "lauren-workflow", "SKILL.md"), "无关文案\n")
	os.Remove(filepath.Join(main, ".lauren", "project", "checks", "dep.txt"))
	_, m, errb = runAt(t, a, "change", "advance")
	if !strings.Contains(errText(m, errb), "资源不存在") {
		t.Fatalf("删掉依赖应说明资源不存在: %s", errText(m, errb))
	}
}

func TestSymlinkRetargetAndMidRunChange(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	realA := filepath.Join(main, ".lauren", "project", "checks", "real-a.sh")
	realB := filepath.Join(main, ".lauren", "project", "checks", "real-b.sh")
	writeFile(t, realA, "#!/bin/sh\necho same\n")
	writeFile(t, realB, "#!/bin/sh\necho same\n")
	link := filepath.Join(main, ".lauren", "project", "checks", "link.sh")
	if err := os.Symlink("real-a.sh", link); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(main, ".lauren", "project", "lauren.project.json")
	body := strings.Replace(sharedResourceProject, `"argv": ["resource:checks/show.sh"]`, `"argv": ["resource:checks/link.sh"]`, 1)
	body = strings.Replace(body, `"resources": ["resource:checks/dep.txt"]`, `"resources": []`, 1)
	writeFile(t, proj, body)
	must(t, a, ExitOK, "change", "create", "--goal", "链接")
	must(t, a, ExitOK, "check", "--name", "show")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real-b.sh", link); err != nil {
		t.Fatal(err)
	}
	st := must(t, a, ExitOK, "status")
	if namedCheck(t, st, "show")["effective_status"] == "pass" {
		t.Fatal("软链接目标变了，旧通过不能继续有效")
	}
}

func TestMidRunResourceChange(t *testing.T) {
	main, _, b := setupResourceRepo(t)
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "mutate.sh"), "#!/bin/sh\necho changed > \"$LAUREN_RESOURCE/checks/dep.txt\"\nexit 0\n")
	body := strings.Replace(sharedResourceProject, `"argv": ["resource:checks/show.sh"]`, `"argv": ["resource:checks/mutate.sh"]`, 1)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), body)
	must(t, b, ExitOK, "change", "create", "--goal", "执行中")
	ran := must(t, b, ExitCheck, "check", "--name", "show")
	got := namedCheck(t, ran, "show")
	if got["status"] == "pass" || !strings.Contains(fmtNote(got), "执行期间") {
		t.Fatalf("执行中改了声明资源，不能记成通过: %#v", got)
	}
}

func TestSkillTextDoesNotStaleChecks(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	must(t, a, ExitOK, "change", "create", "--goal", "文案")
	must(t, a, ExitOK, "check", "--name", "show")
	writeFile(t, filepath.Join(main, ".lauren", "project", "skills", "extra.md"), "只改无关说明\n")
	script := filepath.Join(main, ".lauren", "project", "checks", "show.sh")
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	st := must(t, a, ExitOK, "status")
	if namedCheck(t, st, "show")["effective_status"] != "pass" {
		t.Fatal("没声明的 Skill 文案变化不应作废检查")
	}
	writeFile(t, script, string(raw)+"\n")
	st = must(t, a, ExitOK, "status")
	if namedCheck(t, st, "show")["effective_status"] == "pass" {
		t.Fatal("已声明脚本多一个换行也是字节变化，不能忽略格式")
	}
}

func TestArgvFormsAndNoShell(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	script := filepath.Join(a, "检查 脚本.sh")
	writeFile(t, script, "#!/bin/sh\nprintf '%s\\n' \"$1\" \"$2\"\n")
	abs := script
	proj := filepath.Join(main, ".lauren", "project", "lauren.project.json")
	writeFile(t, proj, `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "red_gate": "off"},
	  "checks": [
	    {"name": "local", "layer": "unit", "gate": "self_verification", "argv": ["./checks/args.sh", "$HOME", "中文"], "required": true},
	    {"name": "abs", "layer": "unit", "gate": "self_verification", "argv": ["`+abs+`", "参数"], "required": false},
	    {"name": "dotdot", "layer": "unit", "gate": "self_verification", "argv": ["resource:../secret.sh"], "required": false},
	    {"name": "tool", "layer": "unit", "gate": "self_verification", "argv": ["true"], "required": false}
	  ],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(a, "checks", "args.sh"), "#!/bin/sh\nprintf '%s\\n' \"$1\" \"$2\"\n")
	must(t, a, ExitOK, "change", "create", "--goal", "参数")
	local := must(t, a, ExitOK, "check", "--name", "local")
	log := readLog(t, namedCheck(t, local, "local"))
	if !strings.Contains(log, "$HOME") || !strings.Contains(log, "中文") {
		t.Fatalf("argv 参数不能做 shell 展开: %s", log)
	}
	absRun := must(t, a, ExitOK, "check", "--name", "abs")
	if canonPath(t, namedCheck(t, absRun, "abs")["cwd"].(string)) != canonPath(t, a) {
		t.Fatal("绝对路径的检查 cwd 仍应是当前工作区")
	}
	dot := must(t, a, ExitCheck, "check", "--name", "dotdot")
	if !strings.Contains(fmtNote(namedCheck(t, dot, "dotdot")), "越界") && !strings.Contains(readLog(t, namedCheck(t, dot, "dotdot")), "越界") {
		t.Fatalf("resource 里的 .. 仍应拒绝: %#v", namedCheck(t, dot, "dotdot"))
	}
	tool := must(t, a, ExitOK, "check", "--name", "tool")
	note := fmtNote(namedCheck(t, tool, "tool"))
	if !strings.Contains(note, "外部程序") && !strings.Contains(note, "PATH") {
		t.Fatalf("PATH 上的程序应标明未做内容摘要: %s", note)
	}
}

func TestFrozenPlanKeepsCommand(t *testing.T) {
	main, a, _ := setupResourceRepo(t)
	must(t, a, ExitOK, "change", "create", "--goal", "冻结")
	must(t, a, ExitOK, "check", "--name", "show")
	proj := filepath.Join(main, ".lauren", "project", "lauren.project.json")
	body := strings.Replace(sharedResourceProject, "resource:checks/show.sh", "resource:checks/other.sh", 1)
	writeFile(t, proj, body)
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "other.sh"), "#!/bin/sh\necho OTHER\n")
	ran := must(t, a, ExitOK, "check", "--name", "show")
	log := readLog(t, namedCheck(t, ran, "show"))
	if strings.Contains(log, "OTHER") || !strings.Contains(log, "fixture=A") {
		t.Fatalf("在途计划不能被共享配置里的新命令换掉: %s", log)
	}
}

const sharedResourceProject = `{
  "schema": 1,
  "environments": ["development", "integration", "staging", "production"],
  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "red_gate": "off"},
  "checks": [{"name": "show", "layer": "unit", "gate": "self_verification", "argv": ["resource:checks/show.sh"], "resources": ["resource:checks/dep.txt"], "required": true}],
  "requirements": []
}`

func setupResourceRepo(t *testing.T) (main, a, b string) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "资源 仓库")
	main = filepath.Join(base, "main")
	a = filepath.Join(base, "功能 A")
	b = filepath.Join(base, "功能 B")
	writeFile(t, filepath.Join(main, "README"), "r\n")
	writeFile(t, filepath.Join(main, "fixture.txt"), "MAIN\n")
	gitInit(t, main)
	writeFile(t, filepath.Join(main, ".lauren", "project", "lauren.project.json"), sharedResourceProject)
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "show.sh"), "#!/bin/sh\necho \"cwd=$(pwd)\"\necho \"fixture=$(cat \"$LAUREN_WORKTREE/fixture.txt\")\"\n")
	writeFile(t, filepath.Join(main, ".lauren", "project", "checks", "dep.txt"), "dep-v1\n")
	runGit(t, main, "worktree", "add", "-b", "feature-a", a)
	runGit(t, main, "worktree", "add", "-b", "feature-b", b)
	writeFile(t, filepath.Join(a, "fixture.txt"), "A\n")
	writeFile(t, filepath.Join(b, "fixture.txt"), "B\n")
	return main, a, b
}

func readLog(t *testing.T, check map[string]any) string {
	t.Helper()
	p, _ := check["log_path"].(string)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("没有日志 %v: %#v", err, check)
	}
	return string(b)
}

func fmtNote(check map[string]any) string {
	note, _ := check["note"].(string)
	return note + " " + readPreview(check)
}

func readPreview(check map[string]any) string {
	p, _ := check["preview"].(string)
	return p
}
