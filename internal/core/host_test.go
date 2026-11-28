package core

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hostHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "宿主家目录")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func countHooks(t *testing.T, path, host string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	n := 0
	walkHooks(raw["hooks"], func(cmd string) {
		if strings.Contains(cmd, " host run --host "+host) {
			n++
		}
	})
	return n
}

func walkHooks(v any, fn func(string)) {
	switch x := v.(type) {
	case map[string]any:
		if cmd, ok := x["command"].(string); ok {
			fn(cmd)
		}
		for _, child := range x {
			walkHooks(child, fn)
		}
	case []any:
		for _, child := range x {
			walkHooks(child, fn)
		}
	}
}

func TestHostInstallIsExplicitAndIdempotent(t *testing.T) {
	main, a, _ := setupSharedRepo(t)
	home := hostHome(t)
	before := treeDigest(t, main)
	code, m, errb := runAt(t, main, "host", "install", "--home", home)
	if code != ExitOK || m["applied"] != false {
		t.Fatalf("没有 --apply 不能改宿主配置: %d %#v %s", code, m, errb)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("预览不应写入 Claude 配置")
	}
	if treeDigest(t, main) != before {
		t.Fatal("安装预览不能改项目")
	}

	other := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	kept := []byte("{\n  \"permissions\": {\"allow\": [\"Bash(go test)\"]},\n  \"hooks\": {\"SessionStart\": [{\"hooks\": [{\"type\": \"command\", \"command\": \"echo keep-me\"}]}]}\n}\n")
	if err := os.WriteFile(other, kept, 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		must(t, main, ExitOK, "host", "install", "--apply", "--home", home)
	}
	if countHooks(t, other, "claude") != 1 {
		t.Fatal("三次安装只能有一份 Claude 受管入口")
	}
	codex := filepath.Join(home, ".codex", "hooks.json")
	if countHooks(t, codex, "codex") != 1 {
		t.Fatal("三次安装只能有一份 Codex 受管入口")
	}
	body, err := os.ReadFile(other)
	if err != nil || !strings.Contains(string(body), "echo keep-me") || !strings.Contains(string(body), "Bash(go test)") {
		t.Fatalf("应保留用户原有配置: %s", body)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err == nil {
		t.Fatal("不应再写 Codex config.toml，避免和 hooks.json 各算一份")
	}
	reg, err := os.ReadFile(filepath.Join(home, ".lauren", "host-registry.json"))
	if err != nil || strings.Count(string(reg), "git_common_dir") != 1 {
		t.Fatalf("仓库身份只登记一次: %s", reg)
	}

	st := must(t, a, ExitOK, "host", "run", "--host", "claude", "--home", home)
	if st["context_delivered"] != true || st["host_injected"] != false {
		t.Fatalf("适配器写出了上下文，但没有真实宿主进程，不能标成已注入: %#v", st)
	}
	var out, errb2 bytes.Buffer
	code = Run([]string{"--root", a, "host", "run", "--host", "claude", "--home", home}, &out, &errb2)
	if code != ExitOK || !strings.Contains(out.String(), "init-worktree") || strings.Contains(out.String(), "\"context_delivered\"") {
		t.Fatalf("钩子 stdout 应是 session-context 的纯文本: code=%d stderr=%s stdout=%s", code, errb2.String(), out.String())
	}
	if _, err := os.Stat(filepath.Join(a, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("冷启动不能靠复制项目里的 hook 文件")
	}
	sc := must(t, a, ExitOK, "session-context")
	if sc["host_injected"] != false || !strings.Contains(sc["injection"].(string), "host run") {
		t.Fatalf("session-context 的 host_injected 仍表示没有观察到宿主会话: %#v", sc["injection"])
	}

	otherRepo := t.TempDir()
	writeFile(t, filepath.Join(otherRepo, "README"), "nope\n")
	gitInit(t, otherRepo)
	var quiet, qerr bytes.Buffer
	code = Run([]string{"--root", otherRepo, "host", "run", "--host", "claude", "--home", home}, &quiet, &qerr)
	if code != ExitOK || quiet.Len() != 0 {
		t.Fatalf("未授权仓库不应注入项目规则: code=%d stdout=%q stderr=%q", code, quiet.String(), qerr.String())
	}

	must(t, main, ExitOK, "host", "uninstall", "--apply", "--home", home)
	body, err = os.ReadFile(other)
	if err != nil || !strings.Contains(string(body), "echo keep-me") || strings.Contains(string(body), "host run --host claude") {
		t.Fatalf("卸载只去掉 Lauren 的钩子: %s %v", body, err)
	}
}

func TestHostRunFailureDoesNotBlockSession(t *testing.T) {
	main, _, _ := setupSharedRepo(t)
	home := hostHome(t)
	must(t, main, ExitOK, "host", "install", "--apply", "--home", home)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, ".claude", "settings.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, m, errTextOut := runAt(t, main, "host", "install", "--apply", "--home", dir)
	after, err := os.ReadFile(bad)
	if err != nil || string(after) != "{" || code == ExitOK || m["applied"] == true || !strings.Contains(errText(m, errTextOut), "无法解析") {
		t.Fatalf("损坏的宿主配置不能当成安装成功: code=%d file=%q err=%s", code, after, errText(m, errTextOut))
	}

	ro := hostHome(t)
	roFile := filepath.Join(ro, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(roFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(roFile, []byte("{}\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(roFile), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(roFile), 0o755) })
	code, m, errTextOut = runAt(t, main, "host", "install", "--apply", "--home", ro)
	if code == ExitOK || m["applied"] == true {
		t.Fatalf("只读配置不能报安装成功: %d %#v %s", code, m, errTextOut)
	}

	if err := os.Symlink("missing.json", filepath.Join(main, "lauren.project.json")); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code = Run([]string{"--root", main, "host", "run", "--host", "codex", "--home", home}, &out, &errb)
	if code != ExitOK || out.Len() != 0 || !strings.Contains(errb.String(), "配置入口断了") {
		t.Fatalf("上下文失败应退出 0、不往会话写正文，诊断留在 stderr: code=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
}

func TestHostRunTimeoutAndMissingBinary(t *testing.T) {
	old := hostRunTimeout
	hostRunTimeout = 150 * time.Millisecond
	oldBody := hostRunBody
	hostRunBody = func(ctx context.Context, a *app, host, home, start string) int {
		<-ctx.Done()
		return ExitInternal
	}
	t.Cleanup(func() {
		hostRunTimeout = old
		hostRunBody = oldBody
	})
	start := time.Now()
	var out, errb bytes.Buffer
	code := Run([]string{"host", "run", "--host", "claude", "--home", t.TempDir()}, &out, &errb)
	if code != ExitOK || out.Len() != 0 || time.Since(start) > time.Second || !strings.Contains(errb.String(), "超时") {
		t.Fatalf("超时不能挂住会话: code=%d wait=%s stdout=%q stderr=%q", code, time.Since(start), out.String(), errb.String())
	}

	root := moduleRoot(t)
	bin := filepath.Join(t.TempDir(), "lauren bin")
	build := exec.Command("go", "build", "-o", bin, "./cmd/lauren")
	build.Dir = root
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建钩子要用的绝对路径: %v\n%s", err, b)
	}
	main, a, _ := setupSharedRepo(t)
	home := hostHome(t)
	must(t, main, ExitOK, "host", "install", "--apply", "--home", home, "--bin", bin)
	claude := filepath.Join(home, ".claude", "settings.json")
	body, err := os.ReadFile(claude)
	if err != nil || !strings.Contains(string(body), bin) || strings.Contains(string(body), "\"command\": \"lauren ") {
		t.Fatalf("钩子命令应是绝对路径，不依赖 PATH: %s", body)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	var cmdText string
	walkHooks(raw["hooks"], func(cmd string) {
		if strings.Contains(cmd, "host run --host claude") {
			cmdText = cmd
		}
	})
	if !strings.Contains(cmdText, "\"timeout\"") && !strings.Contains(string(body), "\"timeout\": 5") {
		t.Fatalf("钩子应带短超时，避免宿主一直等: %s", body)
	}
	parts := splitCommand(t, cmdText)
	run := exec.Command(parts[0], parts[1:]...)
	run.Dir = a
	run.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	got, err := run.CombinedOutput()
	if err != nil || !strings.Contains(string(got), "init-worktree") {
		t.Fatalf("PATH 里没有 lauren 时绝对路径仍应送出上下文: %v\n%s", err, got)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod")
		}
		dir = parent
	}
}

func splitCommand(t *testing.T, s string) []string {
	t.Helper()
	var args []string
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			b.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
		case c == ' ':
			if b.Len() > 0 {
				args = append(args, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		args = append(args, b.String())
	}
	if len(args) == 0 {
		t.Fatalf("无法拆开钩子命令 %q", s)
	}
	return args
}
