package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func runHuman(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(append([]string{"--root", dir}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

func jsonStrings(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	raw, ok := m[key].([]any)
	if !ok {
		if m[key] == nil {
			return nil
		}
		t.Fatalf("%s 不是字符串列表: %#v", key, m[key])
	}
	out := make([]string, 0, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s[%d] 不是字符串: %#v", key, i, v)
		}
		out = append(out, s)
	}
	return out
}

func guidanceText(t *testing.T, m map[string]any) string {
	t.Helper()
	return strings.Join(jsonStrings(t, m, "guidance"), "\n")
}

func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			lines = append(lines, "link "+rel+" -> "+target)
		case info.IsDir():
			lines = append(lines, "dir "+rel)
		default:
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			sum := sha256.Sum256(b)
			lines = append(lines, "file "+rel+" "+hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func TestSessionContextNotApplicable(t *testing.T) {
	dir := t.TempDir()
	code, m, errb := runAt(t, dir, "session-context")
	if code != ExitOK {
		t.Fatalf("无关目录应安静返回: %d %s", code, errText(m, errb))
	}
	if m["applicable"] != false || m["kind"] != "not_applicable" || m["ok"] != true {
		t.Fatalf("应明确 not_applicable: %#v", m)
	}
	if m["error"] != nil {
		t.Fatalf("not_applicable 不是错误: %#v", m)
	}
	hcode, text, herr := runHuman(t, dir, "session-context")
	if hcode != ExitOK || herr != "" || !strings.Contains(text, "不适用") {
		t.Fatalf("人读输出应只说明不适用: code=%d stderr=%q stdout=%q", hcode, herr, text)
	}

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	code, m, errb = runAt(t, repo, "session-context")
	if code != ExitOK || m["kind"] != "not_applicable" {
		t.Fatalf("没有 lauren 配置的仓库应是 not_applicable，而不是未初始化错误: %d %s %#v", code, errText(m, errb), m)
	}
}

func TestSessionContextLegacyProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), sharedBody)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\necho TOKEN_SHOULD_NOT_LEAK\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "README"), "legacy\n")
	gitInit(t, dir)

	st := must(t, dir, ExitOK, "session-context")
	if st["applicable"] != true || st["host_injected"] != false {
		t.Fatalf("旧项目应有上下文，且不表示宿主已注入: %#v", st)
	}
	ws := workspace(t, st)
	if ws["config_mode"] != "legacy" || st["entry_ready"] != true {
		t.Fatalf("旧的根配置就是入口: %#v %#v", ws, st["entry_ready"])
	}
	if st["runtime_present"] != false {
		t.Fatal("还没有 runtime 目录")
	}
	guide := guidanceText(t, st)
	if strings.Contains(guide, "init-worktree") || strings.Contains(guide, "未初始化") {
		t.Fatalf("已有根配置时不要要求初始化: %s", guide)
	}
	if !strings.Contains(guide, "change create") {
		t.Fatalf("没有变更时应提示按需求创建: %s", guide)
	}
	if strings.Contains(guide, "TOKEN_SHOULD_NOT_LEAK") || strings.Contains(st["trust"].(string), "TOKEN_SHOULD_NOT_LEAK") {
		t.Fatal("上下文不能带上检查脚本内容")
	}
}

func TestSessionContextUninitializedWorktree(t *testing.T) {
	main, a, _ := setupSharedRepo(t)
	if _, err := os.Stat(filepath.Join(a, "lauren.project.json")); !os.IsNotExist(err) {
		t.Fatal("新工作区不应已有根 JSON")
	}
	if _, err := os.Stat(filepath.Join(a, "skills", "lauren-workflow")); !os.IsNotExist(err) {
		t.Fatal("新工作区不应已有 Skill 入口")
	}
	before := treeDigest(t, a)
	sharedBefore, err := os.ReadFile(filepath.Join(main, ".lauren", "project", "lauren.project.json"))
	if err != nil {
		t.Fatal(err)
	}

	st := must(t, a, ExitOK, "session-context")
	ws := workspace(t, st)
	if ws["config_mode"] != "shared" || st["entry_ready"] != false {
		t.Fatalf("没有本地入口时应发现共享配置并要求初始化: %#v entry=%v", ws, st["entry_ready"])
	}
	if canonPath(t, ws["config_path"].(string)) != canonPath(t, filepath.Join(main, ".lauren", "project", "lauren.project.json")) {
		t.Fatalf("应写出共享来源: %#v", ws)
	}
	if canonPath(t, ws["worktree"].(string)) != canonPath(t, a) {
		t.Fatalf("工作区应是当前链接工作区: %#v", ws["worktree"])
	}
	if st["runtime_present"] != false {
		t.Fatal("没有 runtime 不是这份上下文要报的未初始化")
	}
	guide := guidanceText(t, st)
	if !strings.Contains(guide, "init-worktree") {
		t.Fatalf("应指引执行初始化: %s", guide)
	}
	if strings.Contains(guide, "因为 runtime") || strings.Contains(guide, "运行目录缺失") {
		t.Fatalf("不能把缺少 runtime 当成要初始化的原因: %s", guide)
	}
	hcode, text, herr := runHuman(t, a, "session-context")
	if hcode != ExitOK || herr != "" || !strings.Contains(text, "init-worktree") || !strings.Contains(text, "lauren.project.json") {
		t.Fatalf("人读输出应给出初始化动作和共享来源: code=%d stderr=%q stdout=%q", hcode, herr, text)
	}
	if treeDigest(t, a) != before {
		t.Fatal("读取上下文不能改工作区文件")
	}
	sharedAfter, err := os.ReadFile(filepath.Join(main, ".lauren", "project", "lauren.project.json"))
	if err != nil || !bytes.Equal(sharedBefore, sharedAfter) {
		t.Fatal("读取上下文不能改共享配置")
	}

	must(t, a, ExitOK, "init-worktree")
	again := must(t, a, ExitOK, "session-context")
	if again["entry_ready"] != true {
		t.Fatalf("初始化后入口应就绪: %#v", again)
	}
	if strings.Contains(guidanceText(t, again), "init-worktree") {
		t.Fatalf("已初始化后不要再要求初始化: %s", guidanceText(t, again))
	}
	if again["runtime_present"] != false {
		t.Fatal("init-worktree 不创建 runtime，缺少 runtime 仍应可用")
	}
	if !strings.Contains(guidanceText(t, again), "change create") {
		t.Fatalf("初始化后、还没有变更时提示创建: %s", guidanceText(t, again))
	}
}

func TestSessionContextMatchesStatusNext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), strings.Replace(sharedBody, `"red_gate": "off"`, `"red_gate": "off"`, 1))
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "在途")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	before := treeDigest(t, dir)

	st := must(t, dir, ExitOK, "status")
	nx := must(t, dir, ExitOK, "next")
	sc := must(t, dir, ExitOK, "session-context")
	if sc["stage"] != st["stage"] || sc["status"] != st["status"] || sc["id"] != st["id"] {
		t.Fatalf("阶段应与 status 相同: context=%v status=%v", sc, st)
	}
	if !reflect.DeepEqual(jsonStrings(t, sc, "next"), jsonStrings(t, nx, "next")) || !reflect.DeepEqual(jsonStrings(t, sc, "next"), jsonStrings(t, st, "next")) {
		t.Fatalf("下一步应与 next 相同:\ncontext=%v\nnext=%v", sc["next"], nx["next"])
	}
	if treeDigest(t, dir) != before {
		t.Fatal("session-context 不能改配置、runtime 或变更")
	}

	must(t, dir, ExitOK, "change", "block", "--reason", "等接口定稿")
	sc = must(t, dir, ExitOK, "session-context")
	nx = must(t, dir, ExitOK, "next")
	st = must(t, dir, ExitOK, "status")
	if sc["status"] != "blocked" || sc["block"] != "等接口定稿" || sc["stage"] != st["stage"] {
		t.Fatalf("应展示阻塞原因且不解除: %#v", sc)
	}
	if !reflect.DeepEqual(jsonStrings(t, sc, "next"), jsonStrings(t, nx, "next")) {
		t.Fatalf("阻塞时下一步仍与 next 相同: %#v vs %#v", sc["next"], nx["next"])
	}
	guide := guidanceText(t, sc)
	if !strings.Contains(guide, "等接口定稿") || !strings.Contains(guide, "不要自动 resume") {
		t.Fatalf("应先说明阻塞原因，并禁止自动 resume: %s", guide)
	}
	if strings.Contains(guide, "已解除") {
		t.Fatalf("不能把阻塞说成已经解除: %s", guide)
	}
	if must(t, dir, ExitOK, "status")["status"] != "blocked" {
		t.Fatal("读取上下文不能解除阻塞")
	}
}

func TestSessionContextRedIsNotSkipped(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(sharedBody, `"red_gate": "off", `, "", 1)
	writeFile(t, filepath.Join(dir, "lauren.project.json"), body)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\necho TOKEN_SHOULD_NOT_LEAK\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "red")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "unit")
	before := treeDigest(t, dir)

	start := time.Now()
	sc := must(t, dir, ExitOK, "session-context")
	if time.Since(start) > 5*time.Second {
		t.Fatal("session-context 应很快返回")
	}
	if sc["stage"] != StageBuild {
		t.Fatalf("不能自动推进阶段: %#v", sc["stage"])
	}
	guide := guidanceText(t, sc)
	if !strings.Contains(guide, "真实执行") || !strings.Contains(guide, "不要跳过 Red") {
		t.Fatalf("Red 缺失时应要求真实执行: %s", guide)
	}
	if strings.Contains(guide, "red skip") {
		t.Fatalf("不能默认建议 red skip: %s", guide)
	}
	blob := guide + sc["trust"].(string)
	if strings.Contains(blob, "TOKEN_SHOULD_NOT_LEAK") {
		t.Fatal("不能把检查脚本内容放进上下文")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "checks", "unit.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := Run([]string{"--root", dir, "--json", "session-context"}, &out, &bytes.Buffer{})
	if code != ExitOK || strings.Contains(out.String(), string(bytes.TrimSpace(raw))) || len(out.Bytes()) > 4096 {
		t.Fatalf("输出应有长度上限且不含脚本: code=%d len=%d", code, out.Len())
	}
	if treeDigest(t, dir) != before {
		t.Fatal("提示 Red 时也不能写状态或跑新的检查")
	}
	if must(t, dir, ExitOK, "status")["stage"] != StageBuild {
		t.Fatal("不能自动推进")
	}
}

func TestSessionContextDiagnostics(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "lauren.project.json"), "{")
		writeFile(t, filepath.Join(dir, "README"), "x\n")
		gitInit(t, dir)
		code, m, errb := runAt(t, dir, "session-context")
		if code == ExitOK || m["kind"] == "not_applicable" || m["applicable"] == true {
			t.Fatalf("损坏配置要可诊断，且不是 not_applicable: %d %#v %s", code, m, errb)
		}
		if m["kind"] != "config" || !strings.Contains(errText(m, errb), "无法解析") {
			t.Fatalf("应指出配置损坏: %#v %s", m, errb)
		}
	})

	t.Run("brokenLink", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "README"), "x\n")
		gitInit(t, dir)
		if err := os.Symlink("missing-project.json", filepath.Join(dir, "lauren.project.json")); err != nil {
			t.Fatal(err)
		}
		code, m, errb := runAt(t, dir, "session-context")
		if code == ExitOK || m["kind"] != "broken_link" || !strings.Contains(errText(m, errb), "配置入口断了") {
			t.Fatalf("断链应单独诊断: %d %#v %s", code, m, errb)
		}
	})

	t.Run("git", func(t *testing.T) {
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
		runGit(t, main, "worktree", "add", "-b", "feature", linked)
		code, m, errb := runAt(t, linked, "session-context")
		if code == ExitOK || m["kind"] != "git" || m["kind"] == "not_applicable" || !strings.Contains(errText(m, errb), "无法定位主工作区") {
			t.Fatalf("Git 异常应与 not_applicable 分开: %d %#v %s", code, m, errb)
		}
	})
}
