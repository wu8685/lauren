package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedGate(t *testing.T) {
	writeRedProject := func(t *testing.T, dir, redGate string) {
		t.Helper()
		extra := ""
		if redGate != "" {
			extra = `, "red_gate": "` + redGate + `"`
		}
		writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
		  "schema": 1,
		  "environments": ["development", "integration", "staging", "production"],
		  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"`+extra+`},
		  "checks": [{"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true}],
		  "requirements": []
		}`)
		writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nif [ -f .lauren/runtime/red ]; then echo lauren-class classification=assertion_fail; exit 1; fi\nexit 0\n")
		writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
		writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
		writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
		gitInit(t, dir)
	}
	toBuild := func(t *testing.T, dir string) {
		t.Helper()
		must(t, dir, ExitOK, "change", "create", "--goal", "red")
		must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
		must(t, dir, ExitOK, "change", "advance")
		must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
		must(t, dir, ExitOK, "change", "advance")
		must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	}

	t.Run("requiredBlocksBarePass", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "required")
		toBuild(t, dir)
		must(t, dir, ExitOK, "check", "--name", "unit")
		_, m, errb := runAt(t, dir, "change", "advance")
		if !strings.Contains(errText(m, errb), "Red") {
			t.Fatalf("只有通过、没有先失败的记录时不能离开实现: %s", errText(m, errb))
		}
	})

	t.Run("classifiedFailThenPass", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "required")
		toBuild(t, dir)
		writeFile(t, filepath.Join(dir, ".lauren", "runtime", "red"), "1\n")
		must(t, dir, ExitCheck, "check", "--name", "unit")
		st := must(t, dir, ExitOK, "status")
		run := namedCheck(t, st, "unit")
		if run["classification"] != "assertion_fail" || run["log_digest"] == "" {
			t.Fatalf("分类和日志摘要应由脚本声明并被记下: %#v", run)
		}
		if err := os.Remove(filepath.Join(dir, ".lauren", "runtime", "red")); err != nil {
			t.Fatal(err)
		}
		must(t, dir, ExitOK, "check", "--name", "unit")
		if must(t, dir, ExitOK, "change", "advance")["stage"] != StageAccept {
			t.Fatal("先有分类过的失败再通过，应能离开实现")
		}
	})

	t.Run("unclassifiedFailDoesNotCount", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "required")
		writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nif [ -f .lauren/runtime/red ]; then exit 1; fi\nexit 0\n")
		toBuild(t, dir)
		writeFile(t, filepath.Join(dir, ".lauren", "runtime", "red"), "1\n")
		must(t, dir, ExitCheck, "check", "--name", "unit")
		if namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["classification"] != nil && namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["classification"] != "" {
			t.Fatal("没有 lauren-class 时不能猜分类")
		}
		if err := os.Remove(filepath.Join(dir, ".lauren", "runtime", "red")); err != nil {
			t.Fatal(err)
		}
		must(t, dir, ExitOK, "check", "--name", "unit")
		_, m, errb := runAt(t, dir, "change", "advance")
		if !strings.Contains(errText(m, errb), "Red") {
			t.Fatalf("未分类的失败不能充当 Red: %s", errText(m, errb))
		}
	})

	t.Run("skipNeedsReasonAndIsVisible", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "required")
		toBuild(t, dir)
		must(t, dir, ExitOK, "check", "--name", "unit")
		_, m, errb := runAt(t, dir, "change", "red", "skip")
		if !strings.Contains(errText(m, errb), "reason") && !strings.Contains(errText(m, errb), "理由") {
			t.Fatalf("跳过 Red 必须有理由: %s", errText(m, errb))
		}
		m = must(t, dir, ExitOK, "change", "red", "skip", "--reason", "这次只改文档")
		red := m["red"].(map[string]any)
		if red["required"] != true || red["skipped"] != true || red["skip_reason"] != "这次只改文档" {
			t.Fatalf("跳过必须出现在状态里: %#v", red)
		}
		if must(t, dir, ExitOK, "change", "advance")["stage"] != StageAccept {
			t.Fatal("显式跳过之后应能离开实现")
		}
	})

	t.Run("omittedDefaultsToRequired", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "")
		toBuild(t, dir)
		must(t, dir, ExitOK, "check", "--name", "unit")
		st := must(t, dir, ExitOK, "status")
		red := st["red"].(map[string]any)
		if red["required"] != true {
			t.Fatalf("省略 red_gate 时应按 required 计算: %#v", red)
		}
		_, m, errb := runAt(t, dir, "change", "advance")
		if !strings.Contains(errText(m, errb), "Red") {
			t.Fatalf("省略字段的新旧项目不能凭一次通过离开实现: %s", errText(m, errb))
		}
	})

	t.Run("explicitOffAllowsBarePass", func(t *testing.T) {
		dir := t.TempDir()
		writeRedProject(t, dir, "off")
		toBuild(t, dir)
		must(t, dir, ExitOK, "check", "--name", "unit")
		st := must(t, dir, ExitOK, "status")
		if st["red"].(map[string]any)["required"] != false {
			t.Fatalf("显式 off 应关闭 Red: %#v", st["red"])
		}
		if must(t, dir, ExitOK, "change", "advance")["stage"] != StageAccept {
			t.Fatal("写成 off 的项目应保持以前可以只靠通过离开实现的行为")
		}
	})
}
