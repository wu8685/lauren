package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func runAt(t *testing.T, dir string, args ...string) (int, map[string]any, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(append([]string{"--root", dir, "--json"}, args...), &out, &errb)
	var m map[string]any
	if strings.TrimSpace(out.String()) != "" {
		if err := json.Unmarshal(out.Bytes(), &m); err != nil {
			t.Fatalf("stdout 不是 JSON: %s\nstderr: %s", out.String(), errb.String())
		}
	}
	return code, m, errb.String()
}

func errText(m map[string]any, errb string) string {
	if m != nil && m["error"] != nil {
		return m["error"].(string)
	}
	return errb
}

func must(t *testing.T, dir string, code int, args ...string) map[string]any {
	t.Helper()
	got, m, errb := runAt(t, dir, args...)
	if got != code {
		t.Fatalf("%v 退出码=%d 期望 %d stderr=%s json=%v", args, got, code, errb, m)
	}
	return m
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "init")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, b)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func projectJSON(release string) string {
	return `{
  "schema": 1,
  "environments": ["development", "integration", "staging", "production"],
  "defaults": {
    "environment": "development",
    "release_stage": "` + release + `",
    "artifact_level": "standard",
    "integration_branch": "main"
  },
  "checks": [
    {"name": "static", "layer": "static", "gate": "self_verification", "argv": ["./checks/static.sh"], "required": true, "environments": ["development", "integration"]},
    {"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true, "environments": ["development", "integration"]},
    {"name": "interact", "layer": "interaction", "gate": "acceptance", "argv": ["./checks/interact.sh"], "required": true, "environments": ["integration", "staging"]},
    {"name": "version", "layer": "release_readback", "gate": "release", "argv": ["./checks/read-version.sh"], "required": true, "environments": ["staging", "production"]}
  ],
  "requirements": [
    {"id": "copy", "claim": "文案含空列表说明", "min_layer": "static", "required": true, "check": "static"},
    {"id": "paths", "claim": "关闭、空选择和失败路径", "min_layer": "unit", "required": true, "check": "unit"},
    {"id": "click", "claim": "真实交互", "min_layer": "interaction", "required": true, "check": "interact", "independent": true}
  ]
}`
}

func project(t *testing.T, dir, release string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), projectJSON(release))
	writeFile(t, filepath.Join(dir, "checks", "static.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "interact.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "read-version.sh"), "#!/bin/sh\necho version=0.1.0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"空选择要失败","acceptance":["关闭","空选择","失败路径"],"baseline":"虚构记事"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"补三条路径","risks":"只测文案","prechecks":["unit"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"补了三条路径"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "accept.json"), `{"summary":"按层次验收"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "release.json"), `{"observation":"读回版本"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "retro.json"), `{"summary":"问题有去向"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "short-demand.json"), `{"goal":"只写目标"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "short-plan.json"), `{"approach":"只写做法"}`)
}

func walkBuild(t *testing.T, dir string) {
	t.Helper()
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
}

func TestExcludedReleaseAndPolicy(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	m := must(t, dir, ExitOK, "change", "create", "--goal", "不发布", "--artifact-level", "abbreviated", "--release-stage", "excluded")
	pol := m["policy"].(map[string]any)
	if pol["artifact_level"] != LevelAbbreviated || pol["release_stage"] != ReleaseExcluded || pol["environment"] != "development" {
		t.Fatalf("policy 没有从覆盖和默认拷贝: %#v", pol)
	}
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/short-demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/short-plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	code, m, errb := runAt(t, dir, "check", "--name", "interact")
	if code == ExitOK || !blockedCheck(m, "interact") {
		t.Fatalf("development 不能给只接受 integration/staging 的验收检查放行: code=%d %v %s", code, m, errb)
	}
	_, m, errb = runAt(t, dir, "change", "policy", "set", "--environment", "integration")
	if !strings.Contains(errText(m, errb), "--reason") {
		t.Fatalf("policy set 没有理由时应拒绝: %s", errText(m, errb))
	}
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "policy", "set", "--environment", "staging", "--reason", "换默认环境")
	st := must(t, dir, ExitOK, "status")
	okEnv := false
	for _, c := range st["checks"].([]any) {
		cv := c.(map[string]any)
		if cv["name"] == "unit" && cv["effective_status"] == "pass" && cv["environment"] == "development" {
			okEnv = true
		}
	}
	if !okEnv {
		t.Fatalf("切换默认环境不应作废另一环境的有效证据: %#v", st["checks"])
	}
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "看过", "--scope", "fixture")
	m = must(t, dir, ExitOK, "change", "advance")
	if m["stage"] != StageRetro {
		t.Fatalf("excluded 应跳过发布进入复盘，得到 %v", m["stage"])
	}
	must(t, dir, ExitOK, "change", "problem", "add", "--summary", "曾漏掉关闭路径", "--disposition", "later", "--reason", "以后再说")
	st = must(t, dir, ExitOK, "status")
	if st["problems"].([]any)[0].(map[string]any)["verified_fixed"] != false {
		t.Fatal("later 不是 verified-fixed")
	}
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/retro.json")
	m = must(t, dir, ExitOK, "change", "advance")
	if m["status"] != StatusClosed {
		t.Fatalf("status=%v", m["status"])
	}
}

func TestReleaseMergeAndSharedEnv(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseIncluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "要发布")
	walkBuild(t, dir)
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "尚未记录已合入") {
		t.Fatalf("未记录合入不能进发布: %s", errText(m, errb))
	}
	sha := strings.TrimSpace(gitSHA(t, dir))
	must(t, dir, ExitOK, "change", "integration", "record", "--sha", sha, "--reason", "假定已合入 main")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitCheck, "check", "--name", "version", "--environment", "development")
	st := must(t, dir, ExitOK, "status")
	if st["facts"].(map[string]any)["release_verified"] != false || st["tool_version"] != nil {
		t.Fatal("development 的版本输出不能充当发布读回")
	}
	must(t, dir, ExitOK, "change", "accept", "--what", "release", "--source", "human", "--reason", "声明发布", "--scope", "fixture")
	if must(t, dir, ExitOK, "status")["facts"].(map[string]any)["release_verified"] != false {
		t.Fatal("人工声明不是工具读回")
	}
	must(t, dir, ExitOK, "check", "--name", "version", "--environment", "staging")
	st = must(t, dir, ExitOK, "status")
	if st["facts"].(map[string]any)["release_verified"] != true || st["tool_version"].(map[string]any)["source"] != "tool" {
		t.Fatalf("staging 读回应分开记录: %#v", st["tool_version"])
	}
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/release.json")
	if must(t, dir, ExitOK, "change", "advance")["stage"] != StageRetro {
		t.Fatal("发布后应进入复盘")
	}
}

func TestAcceptanceFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	writeFile(t, filepath.Join(dir, "checks", "interact.sh"), "#!/bin/sh\nexit 1\n")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "验收失败")
	walkBuild(t, dir)
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitCheck, "check", "--name", "interact", "--environment", "integration")
	st := must(t, dir, ExitOK, "status")
	if st["stage"] != StageBuild {
		t.Fatalf("验收失败应回到实现与自验，得到 %v", st["stage"])
	}
	if len(st["problems"].([]any)) == 0 {
		t.Fatal("验收失败应记下问题")
	}
}

func TestIntegrationTargetAndWorktrees(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseIncluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "改目标")
	must(t, dir, ExitOK, "change", "policy", "set", "--integration-branch", "iter-2", "--reason", "还没合入，改目标")
	sha := gitSHA(t, dir)
	must(t, dir, ExitOK, "change", "integration", "record", "--sha", sha, "--reason", "记到 iter-2")
	walkBuild(t, dir)
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "还不是项目当前集成分支") {
		t.Fatalf("新目标还不是当前集成分支时不能占共享发布: %s", errText(m, errb))
	}
}

func blockedCheck(m map[string]any, name string) bool {
	checks, _ := m["checks"].([]any)
	for _, c := range checks {
		cv, _ := c.(map[string]any)
		if cv["name"] == name && cv["effective_status"] == "blocked" {
			return true
		}
	}
	return false
}

func gitSHA(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = dir
	b, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestTwoWorktrees(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	other := filepath.Join(t.TempDir(), "wt-b")
	runGit(t, dir, "worktree", "add", "-b", "feature-b", other)
	// worktree add copies tracked files including project json
	must(t, dir, ExitOK, "change", "create", "--goal", "工作区 A")
	must(t, other, ExitOK, "change", "create", "--goal", "工作区 B")
	_, m, errb := runAt(t, dir, "change", "create", "--goal", "第二个")
	if !strings.Contains(errText(m, errb), "这个工作区已有进行中的变更") {
		t.Fatalf("同一工作区不能有两个进行中变更: %s", errText(m, errb))
	}
	idA := must(t, dir, ExitOK, "status")["id"]
	idB := must(t, other, ExitOK, "status")["id"]
	if idA == idB {
		t.Fatal("两个工作区不应共用同一次变更")
	}
}

func TestCatalogNotDefaults(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "目录")
	walkBuild(t, dir)
	proj, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := fingerprint(dir, proj)
	proj.Defaults.Environment = "integration"
	raw, _ := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["defaults"].(map[string]any)["environment"] = "integration"
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, _ = loadProject(dir)
	after := fingerprint(dir, proj)
	if after.Hash != before.Hash {
		t.Fatal("只改 defaults 不应改变证据指纹")
	}
	st := must(t, dir, ExitOK, "status")
	for _, c := range st["checks"].([]any) {
		if c.(map[string]any)["name"] == "unit" && c.(map[string]any)["effective_status"] != "pass" {
			t.Fatal("改 defaults 不应作废已有证据")
		}
	}
	checks := doc["checks"].([]any)
	checks[0].(map[string]any)["argv"] = []any{"./checks/static.sh", "--changed"}
	b, _ = json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	st = must(t, dir, ExitOK, "status")
	for _, c := range st["checks"].([]any) {
		if c.(map[string]any)["effective_status"] != "stale" && c.(map[string]any)["effective_status"] != "blocked" {
			t.Fatalf("改 checks 应作废证据: %#v", c)
		}
	}
}

func TestDirtyIgnoredAndNotInIteration(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseIncluded)
	gitInit(t, dir)
	proj, _ := loadProject(dir)
	base := fingerprint(dir, proj)
	writeFile(t, filepath.Join(dir, ".lauren", "runtime", "noise.txt"), "x\n")
	writeFile(t, filepath.Join(dir, "build.out"), "a\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), ".lauren/runtime/\nbuild.out\n")
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "ignore")
	proj, _ = loadProject(dir)
	mid := fingerprint(dir, proj)
	writeFile(t, filepath.Join(dir, "build.out"), "b\n")
	writeFile(t, filepath.Join(dir, ".lauren", "runtime", "noise.txt"), "y\n")
	if fingerprint(dir, proj).Hash != mid.Hash {
		t.Fatal("忽略文件和 runtime 不应改变源码指纹")
	}
	_ = base
	must(t, dir, ExitOK, "change", "create", "--goal", "暂不纳入", "--integration-intent", "not_in_current_iteration")
	walkBuild(t, dir)
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "staging")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "暂不纳入") {
		t.Fatalf("暂不纳入不能占共享发布: %s", errText(m, errb))
	}
	writeFile(t, filepath.Join(dir, "app", "extra.txt"), "dirty\n")
	st := must(t, dir, ExitOK, "status")
	for _, c := range st["checks"].([]any) {
		if c.(map[string]any)["bind_kind"] == BindWorktree && c.(map[string]any)["effective_status"] == "pass" {
			t.Fatalf("未跟踪文件应使工作区证据过期: %#v", c)
		}
	}
}

func TestMergedTargetRejectsUntilWithdraw(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseIncluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "已合入")
	sha := gitSHA(t, dir)
	must(t, dir, ExitOK, "change", "integration", "record", "--sha", sha, "--reason", "已合入 main")
	_, m, errb := runAt(t, dir, "change", "policy", "set", "--integration-branch", "iter-9", "--reason", "想改目标")
	if !strings.Contains(errText(m, errb), "尚未撤回") {
		t.Fatalf("已合入未撤回应拒绝: %s", errText(m, errb))
	}
	must(t, dir, ExitOK, "change", "integration", "withdraw", "--reason", "从 main 撤回")
	must(t, dir, ExitOK, "change", "policy", "set", "--integration-branch", "iter-9", "--reason", "撤回后改目标")
	if must(t, dir, ExitOK, "status")["policy"].(map[string]any)["integration_branch"] != "iter-9" {
		t.Fatal("撤回后应能改这个流程的集成分支")
	}
}

func TestSubprocessAndEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [{"name": "fail", "layer": "unit", "gate": "self_verification", "argv": ["./checks/fail.sh"], "required": true}],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "fail.sh"), "#!/bin/sh\necho token=supersecret\nexit 1\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"x","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "子进程")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitCheck, "check", "--name", "fail")
	st := must(t, dir, ExitOK, "status")
	ch := st["checks"].([]any)[0].(map[string]any)
	if ch["effective_status"] != "fail" || strings.Contains(fmt.Sprint(ch["preview"]), "supersecret") {
		t.Fatalf("失败或未脱敏: %#v", ch)
	}
	writeFile(t, filepath.Join(dir, "artifacts", "bad.json"), "not-json")
	_, m, errb := runAt(t, dir, "change", "submit", "--artifact", "artifacts/bad.json")
	if !strings.Contains(errText(m, errb), "JSON") {
		t.Fatal(errText(m, errb))
	}
	_, m, errb = runAt(t, dir, "change", "submit", "--artifact", "../outside.json")
	if !strings.Contains(errText(m, errb), "相对路径") {
		t.Fatal(errText(m, errb))
	}

	empty := t.TempDir()
	writeFile(t, filepath.Join(empty, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(empty, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(empty, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(empty, "artifacts", "impl.json"), `{"summary":"s"}`)
	gitInit(t, empty)
	must(t, empty, ExitOK, "change", "create", "--goal", "空配置")
	must(t, empty, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, empty, ExitOK, "change", "advance")
	must(t, empty, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, empty, ExitOK, "change", "advance")
	must(t, empty, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	_, m, errb = runAt(t, empty, "change", "advance")
	if !strings.Contains(errText(m, errb), "空检查配置") {
		t.Fatal(errText(m, errb))
	}
}

func TestTimeoutBlockCancel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "check_timeout_seconds": 1},
	  "checks": [{"name": "slow", "layer": "unit", "gate": "self_verification", "argv": ["./checks/slow.sh"], "required": true}],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "slow.sh"), "#!/bin/sh\nsleep 5\n")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "超时")
	old := checkTimeoutUnit
	checkTimeoutUnit = 200 * time.Millisecond
	t.Cleanup(func() { checkTimeoutUnit = old })
	code, _, _ := runAt(t, dir, "check", "--name", "slow")
	if code != ExitCheck {
		t.Fatalf("超时退出码=%d", code)
	}
	slow := must(t, dir, ExitOK, "status")["checks"].([]any)[0].(map[string]any)
	if slow["effective_status"] != "fail" || slow["timeout_seconds"].(float64) != 1 {
		t.Fatalf("超时应记失败并带上有效超时: %#v", slow)
	}
	must(t, dir, ExitOK, "change", "block", "--reason", "等")
	must(t, dir, ExitBlocked, "check")
	must(t, dir, ExitOK, "change", "resume")
	must(t, dir, ExitOK, "change", "cancel", "--reason", "停")
	if must(t, dir, ExitOK, "status")["status"] != StatusCancel {
		t.Fatal("取消不是正常关闭")
	}
}

func TestConcurrentChangeWrites(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	m := must(t, dir, ExitOK, "change", "create", "--goal", "并发")
	id := m["id"].(string)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = WithChange(dir, id, func(ch *Change) error {
				ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "note", Message: fmt.Sprintf("n%d", n)})
				return nil
			})
		}(i)
	}
	wg.Wait()
	ch, err := readChange(dir, id)
	if err != nil || ch == nil {
		t.Fatal(err)
	}
	notes := 0
	for _, e := range ch.History {
		if e.Kind == "note" {
			notes++
		}
	}
	if notes != 8 {
		t.Fatalf("并发写入丢失: %d", notes)
	}
}

func TestProblemLibrary(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "问题库")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 1\n")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitCheck, "check", "--name", "unit")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	must(t, dir, ExitOK, "change", "advance")

	add := []string{"change", "problem", "add", "--title", "空选择静默成功", "--layer", "unit", "--symptom", "未传选择", "--target-check", "unit"}
	m := must(t, dir, ExitOK, add...)
	pb := m["problems"].([]any)[0].(map[string]any)
	promoted, _ := pb["promoted_check"].(string)
	if pb["count"].(float64) != 1 || pb["status"] != "open" || promoted != "" {
		t.Fatalf("一次出现不应晋升: %#v", pb)
	}
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/retro.json")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "还没有处置") {
		t.Fatalf("未处置不能关闭复盘: %s", errText(m, errb))
	}
	_, m, errb = runAt(t, dir, "change", "problem", "verify", "--problem", pb["id"].(string), "--check", "missing")
	if !strings.Contains(errText(m, errb), "不能标成已验证修复") {
		t.Fatalf("没有同一检查的失败和通过时不能标已修复: %s", errText(m, errb))
	}
	m = must(t, dir, ExitOK, append(add, "--note", "第二次")...)
	pb = m["problems"].([]any)[0].(map[string]any)
	if pb["count"].(float64) != 2 || pb["status"] != "promoted" || pb["promoted_check"] != "unit" || pb["disposition"] != "" || pb["verified_fixed"] != false {
		t.Fatalf("两次应晋升到已有检查，且不沿用旧处置: %#v", pb)
	}
	if _, err := os.Stat(filepath.Join(dir, "checks", "promoted")); !os.IsNotExist(err) {
		t.Fatal("晋升不应生成占位脚本")
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".lauren", "problems"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("同类应追加而不是新建，文件数=%d", n)
	}
	if pb["verified_fixed"] != false {
		t.Fatal("晋升本身不是已验证修复")
	}
	must(t, dir, ExitOK, "check", "--name", "unit")
	m = must(t, dir, ExitOK, "change", "problem", "verify", "--problem", pb["id"].(string), "--check", "unit")
	if m["problems"].([]any)[0].(map[string]any)["verified_fixed"] != true {
		t.Fatal("旧失败和新通过之后应能标已验证修复")
	}

	other := t.TempDir()
	raw := projectJSON(ReleaseExcluded)
	raw = strings.Replace(raw, `"integration_branch": "main"`, `"integration_branch": "main", "promote_threshold": 3`, 1)
	writeFile(t, filepath.Join(other, "lauren.project.json"), raw)
	writeFile(t, filepath.Join(other, "checks", "static.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(other, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(other, "checks", "interact.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(other, "checks", "read-version.sh"), "#!/bin/sh\necho version=0.1.0\n")
	writeFile(t, filepath.Join(other, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(other, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(other, "artifacts", "impl.json"), `{"summary":"s"}`)
	writeFile(t, filepath.Join(other, "artifacts", "accept.json"), `{"summary":"s"}`)
	writeFile(t, filepath.Join(other, "artifacts", "retro.json"), `{"summary":"s"}`)
	gitInit(t, other)
	must(t, other, ExitOK, "change", "create", "--goal", "阈值")
	must(t, other, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, other, ExitOK, "change", "advance")
	must(t, other, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, other, ExitOK, "change", "advance")
	must(t, other, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, other, ExitOK, "check", "--name", "static")
	must(t, other, ExitOK, "check", "--name", "unit")
	must(t, other, ExitOK, "change", "advance")
	must(t, other, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, other, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, other, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	must(t, other, ExitOK, "change", "advance")
	args := []string{"change", "problem", "add", "--title", "另一类", "--layer", "lint", "--symptom", "告警", "--disposition", "later", "--reason", "先记住"}
	m = must(t, other, ExitOK, args...)
	must(t, other, ExitOK, append(args, "--note", "第二次")...)
	m = must(t, other, ExitOK, "status")
	pb = m["problems"].([]any)[0].(map[string]any)
	if pb["count"].(float64) != 2 || pb["status"] == "promoted" {
		t.Fatalf("阈值 3 时两次不应晋升: %#v", pb)
	}
}

func TestLaterCannotSkipPromotion(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "未登记")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	must(t, dir, ExitOK, "change", "advance")
	add := []string{"change", "problem", "add", "--title", "需要登记", "--layer", "interaction", "--symptom", "点保存没记录"}
	must(t, dir, ExitOK, add...)
	must(t, dir, ExitOK, append(add, "--note", "第二次", "--disposition", "later", "--reason", "想跳过")...)
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/retro.json")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "还没登记") {
		t.Fatalf("later 不能跳过未完成的晋升: %s", errText(m, errb))
	}
	_, m, errb = runAt(t, dir, append(add, "--target-check", "unit", "--note", "层次不够")...)
	if !strings.Contains(errText(m, errb), "低于问题层次") {
		t.Fatalf("更弱的 target-check 不能晋升: %s", errText(m, errb))
	}
	libs, err := loadLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, pb := range libs {
		pb.Status = "rejected"
		pb.Disposition = "rejected"
		pb.Reason = "不采纳"
		if err := saveLibraryProblem(dir, pb); err != nil {
			t.Fatal(err)
		}
	}
	id := must(t, dir, ExitOK, "status")["id"].(string)
	err = WithChange(dir, id, func(ch *Change) error {
		ch.Problems = append(ch.Problems, Problem{ID: "legacy", Summary: "旧运行时问题"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, m, errb = runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "未写入公共库") {
		t.Fatalf("旧的变更内问题仍应挡住关闭: %s", errText(m, errb))
	}
}

func TestSkillsCheck(t *testing.T) {
	root := filepath.Join("..", "..")
	code, _, errb := runAt(t, root, "skills", "check")
	if code != ExitOK {
		t.Fatalf("skills check: %d %s", code, errb)
	}
}

func namedCheck(t *testing.T, m map[string]any, name string) map[string]any {
	t.Helper()
	for _, c := range m["checks"].([]any) {
		cm := c.(map[string]any)
		if cm["name"] == name {
			return cm
		}
	}
	t.Fatalf("没有检查 %s", name)
	return nil
}

func writeTimeoutProject(t *testing.T, dir, extraDefaults, checks string) {
	t.Helper()
	if extraDefaults != "" {
		extraDefaults = ", " + extraDefaults
	}
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"`+extraDefaults+`},
	  "checks": `+checks+`,
	  "requirements": []
	}`)
}

func TestCheckTimeout(t *testing.T) {
	okCheck := `[{"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/ok.sh"], "required": true}]`

	t.Run("initAndMissingFieldUse600", func(t *testing.T) {
		dir := t.TempDir()
		must(t, dir, ExitOK, "init", "--integration-branch", "main")
		proj, err := loadProject(dir)
		if err != nil {
			t.Fatal(err)
		}
		if proj.Defaults.CheckTimeoutSeconds != 600 || proj.projectTimeoutSeconds() != 600 {
			t.Fatalf("init 应写入 600 秒: %+v", proj.Defaults)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
		if err != nil || !strings.Contains(string(raw), `"check_timeout_seconds": 600`) {
			t.Fatalf("init 文件没有写出超时: %s", raw)
		}

		dir = t.TempDir()
		writeTimeoutProject(t, dir, "", okCheck)
		writeFile(t, filepath.Join(dir, "checks", "ok.sh"), "#!/bin/sh\nexit 0\n")
		gitInit(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "缺省超时")
		m := must(t, dir, ExitOK, "check", "--name", "unit")
		got := namedCheck(t, m, "unit")
		if got["timeout_seconds"].(float64) != 600 || got["effective_status"] != "pass" {
			t.Fatalf("缺字段应使用 600 秒并通过: %#v", got)
		}
		log, err := os.ReadFile(got["log_path"].(string))
		if err != nil || !strings.Contains(string(log), "timeout_seconds=600") {
			t.Fatalf("日志应记下有效超时: %s %v", log, err)
		}
	})

	t.Run("priorityAndZero", func(t *testing.T) {
		dir := t.TempDir()
		writeTimeoutProject(t, dir, `"check_timeout_seconds": 30`, `[
		  {"name": "plain", "layer": "unit", "gate": "self_verification", "argv": ["./checks/ok.sh"], "required": true},
		  {"name": "custom", "layer": "unit", "gate": "self_verification", "argv": ["./checks/ok.sh"], "required": true, "timeout_seconds": 10},
		  {"name": "inherit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/ok.sh"], "required": true, "timeout_seconds": 0}
		]`)
		writeFile(t, filepath.Join(dir, "checks", "ok.sh"), "#!/bin/sh\nexit 0\n")
		gitInit(t, dir)
		proj, err := loadProject(dir)
		if err != nil {
			t.Fatal(err)
		}
		if proj.effectiveTimeoutSeconds(proj.Checks[0]) != 30 || proj.effectiveTimeoutSeconds(proj.Checks[1]) != 10 || proj.effectiveTimeoutSeconds(proj.Checks[2]) != 30 {
			t.Fatalf("优先级不对: %+v", proj.Checks)
		}
		must(t, dir, ExitOK, "change", "create", "--goal", "优先级")
		m := must(t, dir, ExitOK, "check")
		if namedCheck(t, m, "plain")["timeout_seconds"].(float64) != 30 || namedCheck(t, m, "custom")["timeout_seconds"].(float64) != 10 || namedCheck(t, m, "inherit")["timeout_seconds"].(float64) != 30 {
			t.Fatalf("执行结果没有带上有效超时: %#v", m["checks"])
		}

		zero := t.TempDir()
		writeTimeoutProject(t, zero, `"check_timeout_seconds": 0`, okCheck)
		writeFile(t, filepath.Join(zero, "checks", "ok.sh"), "#!/bin/sh\nexit 0\n")
		gitInit(t, zero)
		must(t, zero, ExitOK, "change", "create", "--goal", "零")
		m = must(t, zero, ExitOK, "check", "--name", "unit")
		if namedCheck(t, m, "unit")["timeout_seconds"].(float64) != 600 {
			t.Fatalf("0 应沿用内置 600 秒: %#v", m["checks"])
		}
	})

	t.Run("invalid", func(t *testing.T) {
		cases := []struct {
			extra  string
			checks string
			want   string
		}{
			{`"check_timeout_seconds": -1`, `[]`, "不能为负"},
			{`"check_timeout_seconds": 9223372037`, `[]`, "时间无法表示"},
			{`"check_timeout_seconds": "600"`, `[]`, "无法解析"},
			{`"check_timeout_seconds": 1.5`, `[]`, "无法解析"},
			{`"check_timeout_seconds": 9223372036854775808`, `[]`, "无法解析"},
			{"", `[{"name":"unit","layer":"unit","gate":"self_verification","argv":["./checks/ok.sh"],"required":true,"timeout_seconds":-2}]`, "不能为负"},
			{"", `[{"name":"unit","layer":"unit","gate":"self_verification","argv":["./checks/ok.sh"],"required":true,"timeout_seconds":true}]`, "无法解析"},
		}
		for _, tc := range cases {
			dir := t.TempDir()
			writeTimeoutProject(t, dir, tc.extra, tc.checks)
			_, m, errb := runAt(t, dir, "status")
			if !strings.Contains(errText(m, errb), tc.want) {
				t.Fatalf("配置 %s checks %s: %s", tc.extra, tc.checks, errText(m, errb))
			}
		}
	})

	t.Run("timeoutKillsProcessGroup", func(t *testing.T) {
		dir := t.TempDir()
		pidPath := filepath.Join(t.TempDir(), "child.pid")
		writeTimeoutProject(t, dir, `"check_timeout_seconds": 1`, `[{"name":"slow","layer":"unit","gate":"self_verification","argv":["./checks/slow.sh","`+pidPath+`"],"required":true}]`)
		writeFile(t, filepath.Join(dir, "checks", "slow.sh"), "#!/bin/sh\nsleep 30 &\necho $! > \"$1\"\nwait\n")
		gitInit(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "清理")
		old := checkTimeoutUnit
		checkTimeoutUnit = 300 * time.Millisecond
		t.Cleanup(func() { checkTimeoutUnit = old })
		start := time.Now()
		code, m, errb := runAt(t, dir, "check", "--name", "slow")
		elapsed := time.Since(start)
		if code != ExitCheck {
			t.Fatalf("短超时应该失败: %d %s", code, errText(m, errb))
		}
		if elapsed > 2*time.Second {
			t.Fatalf("超时后没有及时返回: %s", elapsed)
		}
		got := namedCheck(t, m, "slow")
		if got["effective_status"] != "fail" || got["timeout_seconds"].(float64) != 1 || !strings.Contains(got["note"].(string), "检查超时（1秒）") {
			t.Fatalf("超时记录不对: %#v", got)
		}
		raw, err := os.ReadFile(pidPath)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("超时后子进程 %d 仍在", pid)
		}
	})

	t.Run("changingTimeoutStalesPass", func(t *testing.T) {
		dir := t.TempDir()
		writeTimeoutProject(t, dir, "", okCheck)
		writeFile(t, filepath.Join(dir, "checks", "ok.sh"), "#!/bin/sh\nexit 0\n")
		gitInit(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "作废")
		if namedCheck(t, must(t, dir, ExitOK, "check", "--name", "unit"), "unit")["effective_status"] != "pass" {
			t.Fatal("第一次应通过")
		}
		raw, err := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(raw), `"integration_branch": "main"`, `"integration_branch": "main", "check_timeout_seconds": 90`, 1)
		if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["effective_status"] != "stale" {
			t.Fatal("改项目默认超时后，旧的通过不能继续有效")
		}

		dir = t.TempDir()
		writeTimeoutProject(t, dir, `"check_timeout_seconds": 30`, okCheck)
		writeFile(t, filepath.Join(dir, "checks", "ok.sh"), "#!/bin/sh\nexit 0\n")
		gitInit(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "单项作废")
		must(t, dir, ExitOK, "check", "--name", "unit")
		raw, err = os.ReadFile(filepath.Join(dir, "lauren.project.json"))
		if err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(string(raw), `"required": true`, `"required": true, "timeout_seconds": 15`, 1)
		if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["effective_status"] != "stale" {
			t.Fatal("改单项超时后，旧的通过不能继续有效")
		}
	})
}
