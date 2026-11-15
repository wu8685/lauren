package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
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

func project(t *testing.T, dir string, extra string) {
	t.Helper()
	body := `{
  "schema": 1,
  "environment": "local",
  "release": false,
  "profile": "full",
  "checks": [
    {"name": "static", "layer": "static", "when": "self", "argv": ["./checks/static.sh"], "required": true},
    {"name": "unit", "layer": "unit", "when": "self", "argv": ["./checks/unit.sh"], "required": true},
    {"name": "interact", "layer": "interaction", "when": "acceptance", "argv": ["./checks/interact.sh"], "required": true},
    {"name": "version", "layer": "release-readback", "when": "release", "argv": ["./checks/read-version.sh"], "required": true}
  ],
  "requirements": [
    {"id": "copy", "claim": "文案含空列表说明", "min_layer": "static", "required": true, "check": "static"},
    {"id": "paths", "claim": "关闭、空选择和失败路径", "min_layer": "unit", "required": true, "check": "unit"},
    {"id": "click", "claim": "真实交互", "min_layer": "interaction", "required": true, "check": "interact", "independent": true}
  ]
}`
	if extra != "" {
		body = extra
	}
	writeFile(t, filepath.Join(dir, "lauren.project.json"), body)
	writeFile(t, filepath.Join(dir, "checks", "static.sh"), "#!/bin/sh\ngrep -q '空列表' app/copy.txt\n")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\n./app/logic.sh off | grep -q closed\n./app/logic.sh empty | grep -q empty-selection\n./app/logic.sh fail | grep -q failed-path\n")
	writeFile(t, filepath.Join(dir, "checks", "interact.sh"), "#!/bin/sh\ngrep -q 'clicked-save' app/session.log\n")
	writeFile(t, filepath.Join(dir, "checks", "read-version.sh"), "#!/bin/sh\nprintf 'version=%s\\n' \"$(cat app/build-version.txt)\"\n")
	writeFile(t, filepath.Join(dir, "checks", "partial.sh"), "#!/bin/sh\nexit 75\n")
	writeFile(t, filepath.Join(dir, "checks", "fail.sh"), "#!/bin/sh\necho token=supersecret\nexit 1\n")
	writeFile(t, filepath.Join(dir, "app", "copy.txt"), "空列表时说明没有条目\n")
	writeFile(t, filepath.Join(dir, "app", "logic.sh"), "#!/bin/sh\ncase \"$1\" in\noff) echo closed;;\nempty) echo empty-selection;;\nfail) echo failed-path;;\n*) echo ok;;\nesac\n")
	writeFile(t, filepath.Join(dir, "app", "session.log"), "clicked-save\n")
	writeFile(t, filepath.Join(dir, "app", "build-version.txt"), "0.1.0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"空选择要失败","acceptance":["关闭","空选择","失败路径"],"baseline":"虚构记事"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"补三条路径","risks":"只测文案","prechecks":["unit"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"补了三条路径"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "accept.json"), `{"summary":"按层次验收"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "release.json"), `{"observation":"读回版本"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "retro.json"), `{"summary":"问题有去向"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "small-demand.json"), `{"goal":"只写目标"}`)
}

func must(t *testing.T, dir string, code int, args ...string) map[string]any {
	t.Helper()
	got, m, errb := runAt(t, dir, args...)
	if got != code {
		t.Fatalf("%v 退出码=%d 期望 %d stderr=%s json=%v", args, got, code, errb, m)
	}
	return m
}

func walkToBuild(t *testing.T, dir string) {
	t.Helper()
	must(t, dir, ExitOK, "change", "create", "--goal", "空选择要有失败路径", "--release", "false")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
}

func TestNoReleaseCloseAndReleasePath(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	walkToBuild(t, dir)
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "check")
	m := must(t, dir, ExitOK, "change", "advance")
	if m["stage"] != StageAccept {
		t.Fatalf("stage=%v", m["stage"])
	}
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "看过失败路径", "--scope", "fixture")
	m = must(t, dir, ExitOK, "change", "advance")
	if m["stage"] != StageRetro {
		t.Fatalf("不发布应进入复盘，得到 %v", m["stage"])
	}
	facts := m["facts"].(map[string]any)
	if facts["delivery_accepted"] != true || facts["release_declared"] != false || facts["notified"] != nil {
		t.Fatalf("事实混淆: %#v", facts)
	}
	if _, ok := facts["notified"]; ok {
		t.Fatal("没有投递回执时不应出现已通知字段")
	}
	must(t, dir, ExitOK, "change", "problem", "add", "--summary", "曾漏掉关闭路径", "--repro", "off", "--expected", "closed", "--actual", "静默成功")
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/retro.json")
	st := must(t, dir, ExitOK, "status")
	pb := st["problems"].([]any)[0].(map[string]any)
	if pb["verified_fixed"] != false {
		t.Fatal("只有后续去向不能算已验证修复")
	}
	must(t, dir, ExitOK, "change", "problem", "dispose", "--problem", pb["id"].(string), "--disposition", "later", "--reason", "留到以后")
	m = must(t, dir, ExitOK, "change", "advance")
	if m["status"] != StatusClosed {
		t.Fatalf("status=%v", m["status"])
	}

	dir2 := t.TempDir()
	project(t, dir2, "")
	gitInit(t, dir2)
	must(t, dir2, ExitOK, "change", "create", "--goal", "发布读回", "--release", "true")
	for _, step := range [][]string{
		{"change", "submit", "--artifact", "artifacts/demand.json"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/plan.json"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/impl.json"},
		{"check", "--name", "static"},
		{"check", "--name", "unit"},
		{"check", "--name", "interact"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/accept.json"},
		{"change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture"},
		{"change", "advance"},
	} {
		must(t, dir2, ExitOK, step...)
	}
	m = must(t, dir2, ExitOK, "status")
	if m["stage"] != StageRelease {
		t.Fatalf("发布路径阶段=%v", m["stage"])
	}
	must(t, dir2, ExitGate, "change", "advance")
	must(t, dir2, ExitOK, "change", "declare-version", "--version", "9.9.9", "--source", "declaration")
	must(t, dir2, ExitGate, "change", "advance")
	must(t, dir2, ExitOK, "change", "accept", "--what", "release", "--source", "human", "--reason", "声明发布", "--scope", "fixture")
	m = must(t, dir2, ExitOK, "status")
	if m["facts"].(map[string]any)["release_verified"] != false {
		t.Fatal("人工声明不应变成工具读回")
	}
	if m["declared_version"].(map[string]any)["source"] != "declaration" {
		t.Fatal("手填版本来源不对")
	}
	must(t, dir2, ExitOK, "check", "--name", "version")
	m = must(t, dir2, ExitOK, "status")
	if m["tool_version"].(map[string]any)["source"] != "tool" || m["tool_version"].(map[string]any)["value"] != "0.1.0" {
		t.Fatalf("工具版本=%v", m["tool_version"])
	}
	if m["facts"].(map[string]any)["release_verified"] != true {
		t.Fatal("读回通过后 release_verified 应为 true")
	}
	must(t, dir2, ExitOK, "change", "submit", "--artifact", "artifacts/release.json")
	must(t, dir2, ExitOK, "change", "advance")
	if must(t, dir2, ExitOK, "status")["stage"] != StageRetro {
		t.Fatal("发布后应进入复盘")
	}
}

func TestRejectsAndStaleEvidence(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "门禁")
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitGate, "change", "advance")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 1\n")
	runGit(t, dir, "add", "checks/unit.sh")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "break unit")
	must(t, dir, ExitCheck, "check", "--name", "unit")
	must(t, dir, ExitGate, "change", "advance")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\n./app/logic.sh off | grep -q closed\n./app/logic.sh empty | grep -q empty-selection\n./app/logic.sh fail | grep -q failed-path\n")
	runGit(t, dir, "add", "checks/unit.sh")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "fix unit")
	must(t, dir, ExitOK, "check")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "change", "advance")

	writeFile(t, filepath.Join(dir, "app", "notes.txt"), "dirty\n")
	m := must(t, dir, ExitOK, "status")
	for _, c := range m["checks"].([]any) {
		cv := c.(map[string]any)
		if cv["effective_status"] != "stale" {
			t.Fatalf("未提交文件应使证据过期: %#v", cv)
		}
	}
	must(t, dir, ExitGate, "change", "advance")
	os.Remove(filepath.Join(dir, "app", "notes.txt"))
	writeFile(t, filepath.Join(dir, "app", "untracked.txt"), "u\n")
	m = must(t, dir, ExitOK, "status")
	if m["checks"].([]any)[0].(map[string]any)["effective_status"] != "stale" {
		t.Fatal("untracked 应使证据过期")
	}
	os.Remove(filepath.Join(dir, "app", "untracked.txt"))
	cfg, _ := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
	if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), append(cfg, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	m = must(t, dir, ExitOK, "status")
	if m["checks"].([]any)[0].(map[string]any)["effective_status"] != "stale" {
		t.Fatal("配置变化应使证据过期")
	}
}

func TestLayersPartialSelfAndVersion(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "schema": 1, "environment": "local", "release": false, "profile": "full",
  "checks": [
    {"name": "static", "layer": "static", "when": "self", "argv": ["./checks/static.sh"], "required": true},
    {"name": "weak", "layer": "static", "when": "self", "argv": ["./checks/static.sh"], "required": true},
    {"name": "part", "layer": "interaction", "when": "acceptance", "argv": ["./checks/partial.sh"], "required": false},
    {"name": "interact", "layer": "interaction", "when": "self", "argv": ["./checks/interact.sh"], "required": false}
  ],
  "requirements": [
    {"id": "copy", "claim": "静态文案", "min_layer": "static", "required": true, "check": "static"},
    {"id": "click", "claim": "真实交互", "min_layer": "interaction", "required": true, "check": "interact", "independent": true}
  ]
}`
	project(t, dir, body)
	gitInit(t, dir)
	walkToBuild(t, dir)
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "weak")
	must(t, dir, ExitCheck, "check", "--name", "part")
	m := must(t, dir, ExitOK, "status")
	if m["checks"].([]any)[2].(map[string]any)["effective_status"] != "partial" {
		t.Fatalf("partial=%v", m["checks"])
	}
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "看过", "--scope", "fixture")
	_, m, errb := runAt(t, dir, "change", "advance")
	msg := errb
	if m["error"] != nil {
		msg = m["error"].(string)
	}
	if !strings.Contains(msg, "不能改名") {
		t.Fatalf("自验不能充当独立验收: %v %s", m, errb)
	}
	must(t, dir, ExitUsage, "change", "accept", "--what", "delivery", "--source", "self", "--reason", "x", "--scope", "y")
	must(t, dir, ExitUsage, "change", "declare-version", "--version", "1", "--source", "tool")
	must(t, dir, ExitUsage, "change", "stage")
	must(t, dir, ExitUsage, "pass")
}

func TestFlowControlAndRevise(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "控制流")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "block", "--reason", "等说明")
	must(t, dir, ExitBlocked, "check")
	must(t, dir, ExitGate, "change", "advance")
	must(t, dir, ExitOK, "change", "resume")
	must(t, dir, ExitOK, "change", "rollback", "--to", StageDemand, "--reason", "需求写错")
	m := must(t, dir, ExitOK, "status")
	if m["stage"] != StageDemand {
		t.Fatalf("回退后阶段=%v", m["stage"])
	}
	must(t, dir, ExitOK, "change", "submit", "--stage", StageDemand, "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "cancel", "--reason", "不做了")
	m = must(t, dir, ExitOK, "status")
	if m["status"] != StatusCancel || m["status"] == StatusClosed {
		t.Fatalf("取消不应是成功关闭: %v", m["status"])
	}
}

func TestSubprocessRecordAndPaths(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "schema": 1, "environment": "local", "release": false, "profile": "full",
  "checks": [
    {"name": "fail", "layer": "unit", "when": "self", "argv": ["./checks/fail.sh"], "required": true}
  ],
  "requirements": []
}`
	project(t, dir, body)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "子进程")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitCheck, "check", "--name", "fail")
	m := must(t, dir, ExitOK, "status")
	ch := m["checks"].([]any)[0].(map[string]any)
	if ch["effective_status"] != "fail" || ch["exit_code"].(float64) != 1 {
		t.Fatalf("失败不能记成通过: %#v", ch)
	}
	if strings.Contains(ch["preview"].(string), "supersecret") {
		t.Fatal("日志未脱敏")
	}
	logPath := ch["log_path"].(string)
	b, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(b), "[redacted]") {
		t.Fatalf("证据日志=%s err=%v", b, err)
	}
	st, err := os.ReadFile(filepath.Join(dir, ".lauren", "runtime", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(st), "supersecret") {
		t.Fatal("状态 JSON 含有未脱敏内容")
	}
	marker := strings.Repeat("Q", 400)
	writeFile(t, filepath.Join(dir, "checks", "fail.sh"), "#!/bin/sh\necho "+marker+"\nexit 1\n")
	must(t, dir, ExitCheck, "check", "--name", "fail")
	st, _ = os.ReadFile(filepath.Join(dir, ".lauren", "runtime", "state.json"))
	if strings.Contains(string(st), marker) {
		t.Fatal("大日志进了状态 JSON")
	}
	code, m, errb := runAt(t, dir, "change", "submit", "--artifact", "../outside.json")
	msg := errb
	if m["error"] != nil {
		msg = m["error"].(string)
	}
	if code == ExitOK || !strings.Contains(msg, "相对路径") {
		t.Fatalf("路径应被拒绝: code=%d %s %v", code, errb, m)
	}
}

func TestUnknownCandidateAndVerify(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	must(t, dir, ExitOK, "change", "create", "--goal", "无 git")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitCheck, "check")
	m := must(t, dir, ExitOK, "status")
	if m["candidate"].(map[string]any)["kind"] != "unknown" {
		t.Fatal(m["candidate"])
	}
	if m["checks"].([]any)[0].(map[string]any)["effective_status"] == "pass" {
		t.Fatal("未知候选不能静默通过")
	}

	dir = t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	walkToBuild(t, dir)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 1\n")
	must(t, dir, ExitCheck, "check", "--name", "unit")
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "problem", "add", "--summary", "unit 曾失败", "--disposition", "implemented", "--reason", "补了检查")
	st := must(t, dir, ExitOK, "status")
	pid := st["problems"].([]any)[0].(map[string]any)["id"].(string)
	if st["problems"].([]any)[0].(map[string]any)["verified_fixed"] != false {
		t.Fatal("处置本身不是验证")
	}
	must(t, dir, ExitOK, "change", "problem", "verify", "--problem", pid, "--check", "unit")
	st = must(t, dir, ExitOK, "status")
	if st["problems"].([]any)[0].(map[string]any)["verified_fixed"] != true {
		t.Fatal("旧失败加新通过后应标已验证修复")
	}
	writeFile(t, filepath.Join(dir, "app", "again.txt"), "x\n")
	st = must(t, dir, ExitOK, "status")
	if st["problems"].([]any)[0].(map[string]any)["verified_fixed"] != false {
		t.Fatal("候选变化后旧的验证不能继续有效")
	}
}

func TestSmallProfileHistoryAndStore(t *testing.T) {
	dir := t.TempDir()
	code, _, errb := runAt(t, dir, "init", "--profile", "small", "--dir", dir)
	if code != ExitOK {
		t.Fatal(code, errb)
	}
	gitInit(t, dir)
	writeFile(t, filepath.Join(dir, "artifacts", "small-demand.json"), `{"goal":"只写目标"}`)
	must(t, dir, ExitOK, "change", "create", "--goal", "小任务")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/small-demand.json")
	m := must(t, dir, ExitOK, "history")
	if len(m["history"].([]any)) < 2 {
		t.Fatal(m["history"])
	}
	next := must(t, dir, ExitOK, "next")
	if len(next["next"].([]any)) == 0 {
		t.Fatal("next 为空")
	}
	var raw bytes.Buffer
	if Run([]string{"--root", dir, "status"}, &raw, &bytes.Buffer{}) != ExitOK {
		t.Fatal("人读 status 失败")
	}
	if strings.Contains(raw.String(), "%") || strings.Contains(raw.String(), "已通知") {
		t.Fatal(raw.String())
	}
	err := WithState(dir, func(st *State) error {
		st.Changes = append(st.Changes, Change{ID: "extra"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".lauren", "runtime", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := WithState(dir, func(st *State) error {
				st.Changes = append(st.Changes, Change{ID: fmt.Sprintf("x-%d", n)})
				return nil
			}); err != nil {
				t.Errorf("write: %v", err)
			}
		}(i)
	}
	wg.Wait()
	b, err := os.ReadFile(filepath.Join(dir, ".lauren", "runtime", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Changes) != 8 {
		t.Fatalf("并发写入丢失: %d", len(st.Changes))
	}
}

func TestCandidateShiftDropsConclusions(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	walkToBuild(t, dir)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\necho x >> app/copy.txt\necho touched >> app/new-note.txt\nexit 0\n")
	code, m, errb := runAt(t, dir, "check", "--name", "unit")
	if code != ExitCheck {
		t.Fatalf("检查改动工作区仍返回 %d %v %s", code, m, errb)
	}
	m = must(t, dir, ExitOK, "status")
	for _, c := range m["checks"].([]any) {
		cv := c.(map[string]any)
		if cv["name"] == "unit" && cv["effective_status"] != "stale" {
			t.Fatalf("检查改动了跟踪文件和未跟踪文件，结果不能给新候选放行: %#v", cv)
		}
	}
	must(t, dir, ExitGate, "change", "advance")

	dir = t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	before := fingerprint(dir, "local")
	writeFile(t, filepath.Join(dir, ".lauren", "runtime", "noise.txt"), "runtime\n")
	writeFile(t, filepath.Join(dir, "build.out"), "artifact\n")
	writeFile(t, filepath.Join(dir, ".gitignore"), ".lauren/runtime/\nbuild.out\n")
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "ignore build")
	base := fingerprint(dir, "local")
	if base.Hash == before.Hash {
		t.Fatal("提交 gitignore 后指纹应变化")
	}
	writeFile(t, filepath.Join(dir, "build.out"), "artifact-2\n")
	writeFile(t, filepath.Join(dir, ".lauren", "runtime", "noise.txt"), "runtime-2\n")
	after := fingerprint(dir, "local")
	if after.Hash != base.Hash {
		t.Fatalf("忽略文件和 runtime 不应改变源码指纹: %s vs %s", base.Hash, after.Hash)
	}

	walkToAccept(t, dir)
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	cfg, err := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), append(cfg, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	m = must(t, dir, ExitOK, "status")
	if m["facts"].(map[string]any)["delivery_accepted"] != false {
		t.Fatal("配置变化后不能保留交付接受")
	}
	must(t, dir, ExitGate, "change", "advance")
}

func TestReviseVoidsReleaseFacts(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, "")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "发布读回", "--release", "true")
	for _, step := range [][]string{
		{"change", "submit", "--artifact", "artifacts/demand.json"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/plan.json"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/impl.json"},
		{"check", "--name", "static"},
		{"check", "--name", "unit"},
		{"change", "advance"},
		{"change", "submit", "--artifact", "artifacts/accept.json"},
		{"check", "--name", "interact"},
		{"change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture"},
		{"change", "advance"},
		{"change", "accept", "--what", "release", "--source", "human", "--reason", "声明", "--scope", "fixture"},
		{"check", "--name", "version"},
	} {
		must(t, dir, ExitOK, step...)
	}
	m := must(t, dir, ExitOK, "status")
	facts := m["facts"].(map[string]any)
	if facts["release_declared"] != true || facts["release_verified"] != true || m["tool_version"] == nil {
		t.Fatalf("发布事实未分开记下: %#v", m)
	}
	must(t, dir, ExitOK, "change", "submit", "--stage", StageDemand, "--artifact", "artifacts/demand.json")
	m = must(t, dir, ExitOK, "status")
	facts = m["facts"].(map[string]any)
	if m["stage"] != StageDemand || facts["delivery_accepted"] != false || facts["release_declared"] != false || facts["release_verified"] != false || m["tool_version"] != nil {
		t.Fatalf("修正需求后过期结论还在: %#v", m)
	}
}

func TestEmptyConfigTimeoutAndBadInput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{"schema":1,"environment":"local","release":false,"profile":"full","checks":[],"requirements":[]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"空选择要失败","acceptance":["关闭"],"baseline":"虚构"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"补路径","risks":"漏行为","prechecks":["unit"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"还没有检查"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "空配置")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	_, m, errb := runAt(t, dir, "change", "advance")
	msg := errb
	if m["error"] != nil {
		msg = m["error"].(string)
	}
	if !strings.Contains(msg, "空检查配置") {
		t.Fatalf("空配置不应通过自验: %s", msg)
	}

	dir = t.TempDir()
	body := `{
  "schema": 1, "environment": "local", "release": false, "profile": "full",
  "checks": [{"name": "slow", "layer": "unit", "when": "self", "argv": ["./checks/slow.sh"], "required": true}],
  "requirements": []
}`
	project(t, dir, body)
	writeFile(t, filepath.Join(dir, "checks", "slow.sh"), "#!/bin/sh\nsleep 5\n")
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "超时")
	old := checkTimeout
	checkTimeout = 200 * time.Millisecond
	code, _, _ := runAt(t, dir, "check", "--name", "slow")
	checkTimeout = old
	if code != ExitCheck {
		t.Fatalf("超时退出码=%d", code)
	}
	st := must(t, dir, ExitOK, "status")
	ch := st["checks"].([]any)[0].(map[string]any)
	if ch["effective_status"] != "fail" {
		t.Fatalf("超时应记失败: %#v", ch)
	}

	writeFile(t, filepath.Join(dir, "artifacts", "bad.json"), "not-json")
	writeFile(t, filepath.Join(dir, "artifacts", "arr.json"), "[]")
	_, m, errb = runAt(t, dir, "change", "submit", "--artifact", "artifacts/bad.json")
	msg = errText(m, errb)
	if !strings.Contains(msg, "JSON") {
		t.Fatalf("坏 JSON 应拒绝: %s", msg)
	}
	_, m, errb = runAt(t, dir, "change", "submit", "--artifact", "artifacts/arr.json")
	msg = errText(m, errb)
	if !strings.Contains(msg, "JSON") {
		t.Fatalf("数组产物应拒绝: %s", msg)
	}
	_, m, errb = runAt(t, dir, "change", "submit", "--artifact", "artifacts/../../outside.json")
	msg = errText(m, errb)
	if !strings.Contains(msg, "相对路径") {
		t.Fatalf("嵌套越界应拒绝: %s", msg)
	}
}

func errText(m map[string]any, errb string) string {
	if m["error"] != nil {
		return m["error"].(string)
	}
	return errb
}

func walkToAccept(t *testing.T, dir string) {
	t.Helper()
	must(t, dir, ExitOK, "change", "create", "--goal", "验收", "--release", "false")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "static")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact")
}

func TestSkillsCheck(t *testing.T) {
	root := filepath.Join("..", "..")
	code, _, errb := runAt(t, root, "skills", "check")
	if code != ExitOK {
		t.Fatalf("skills check: %d %s", code, errb)
	}
}
