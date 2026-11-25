package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireExecuted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [{"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true, "require_executed": true}],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\necho lauren-stats executed=0 skipped=2\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "零执行")
	code, m, errb := runAt(t, dir, "check", "--name", "unit")
	if code == ExitOK {
		t.Fatal("executed=0 仍被记成通过")
	}
	got := namedCheck(t, m, "unit")
	if got["status"] != "fail" || got["executed"] != float64(0) {
		t.Fatalf("应失败并记下 executed=0: code=%d %#v %s", code, got, errb)
	}

	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\necho lauren-stats executed=1 skipped=0\nexit 0\n")
	must(t, dir, ExitOK, "check", "--name", "unit")

	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	code, m, errb = runAt(t, dir, "check", "--name", "unit")
	blob, _ := json.Marshal(m["checks"])
	if code == ExitOK || !strings.Contains(string(blob), "lauren-stats") {
		t.Fatalf("缺少统计时 require_executed 应失败: %d %s %s", code, errText(m, errb), blob)
	}
}

func TestZeroExecutedOverridesEarlierPass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [{"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true, "require_executed": true}],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nif [ -f .lauren/runtime/zero ]; then echo lauren-stats executed=0 skipped=1; exit 0; fi\necho lauren-stats executed=1 skipped=0\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	gitInit(t, dir)
	reachBuild(t, dir, "零执行盖过通过")
	must(t, dir, ExitOK, "check", "--name", "unit")
	writeFile(t, filepath.Join(dir, ".lauren", "runtime", "zero"), "1\n")
	must(t, dir, ExitCheck, "check", "--name", "unit")
	st := must(t, dir, ExitOK, "status")
	if namedCheck(t, st, "unit")["effective_status"] != "fail" || wantsAdvance(st) {
		t.Fatalf("executed=0 应成为最新失败: %#v", st["checks"])
	}
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "最新结果") {
		t.Fatalf("不能凭更早的有效执行通过: %s", errText(m, errb))
	}
}

func TestInitCopiesGoTestAdapter(t *testing.T) {
	dir := t.TempDir()
	must(t, dir, ExitOK, "init", "--integration-branch", "main", "--adapter", "go-test")
	script := filepath.Join(dir, ".lauren", "adapters", "go-test", "main.go")
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"require_executed": true`) || !strings.Contains(string(raw), ".lauren/adapters/go-test") {
		t.Fatalf("init 应引用复制进来的适配器并打开 require_executed: %s", raw)
	}
}
