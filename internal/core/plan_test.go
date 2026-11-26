package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planProject(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [
	    {"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true},
	    {"name": "lint", "layer": "static", "gate": "self_verification", "argv": ["./checks/lint.sh"], "required": false}
	  ],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "lint.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "fail.sh"), "#!/bin/sh\nexit 1\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	gitInit(t, dir)
}

func TestPolicyDoesNotStaleFrozenEvidence(t *testing.T) {
	dir := t.TempDir()
	project(t, dir, ReleaseExcluded)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "环境")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "policy", "set", "--environment", "staging", "--reason", "换环境")
	st := must(t, dir, ExitOK, "status")
	for _, c := range st["checks"].([]any) {
		cv := c.(map[string]any)
		if cv["name"] == "unit" && cv["effective_status"] != "pass" {
			t.Fatalf("policy 环境变化不应作废检查: %#v", cv)
		}
	}
}

func TestPlanHashStable(t *testing.T) {
	dir := t.TempDir()
	planProject(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "哈希")
	id := must(t, dir, ExitOK, "status")["id"].(string)
	ch, err := readChange(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := catalogHash(ch.projectView(proj))
	if ch.Plan == nil || ch.CatalogHash != h {
		t.Fatalf("plan=%v stored=%s now=%s", ch.Plan != nil, ch.CatalogHash, h)
	}
}

func TestFrozenPlan(t *testing.T) {
	t.Run("projectEditDoesNotChangePlan", func(t *testing.T) {
		dir := t.TempDir()
		planProject(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "冻结")
		raw, err := os.ReadFile(filepath.Join(dir, "lauren.project.json"))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(raw), "./checks/unit.sh", "./checks/fail.sh", 1)
		if err := os.WriteFile(filepath.Join(dir, "lauren.project.json"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, m, errb := runAt(t, dir, "check", "--name", "unit"); code != ExitOK {
			t.Fatalf("项目配置改了 argv，冻结计划仍应跑原来的命令，退出码=%d %s", code, errText(m, errb))
		}
	})

	t.Run("requiredCannotBeSkipped", func(t *testing.T) {
		dir := t.TempDir()
		planProject(t, dir)
		_, m, errb := runAt(t, dir, "change", "create", "--goal", "计划", "--skip", "unit=不想跑")
		if !strings.Contains(errText(m, errb), "必过") {
			t.Fatalf("必过检查不能从计划里拿掉: %s", errText(m, errb))
		}
	})

	t.Run("confirmAfterDemand", func(t *testing.T) {
		dir := t.TempDir()
		planProject(t, dir)
		must(t, dir, ExitOK, "change", "create", "--goal", "计划", "--skip", "lint=文档变更")
		if code, _, _ := runAt(t, dir, "check", "--name", "lint"); code == ExitOK {
			t.Fatal("跳过的可选检查不应执行")
		}
		must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
		must(t, dir, ExitOK, "change", "advance")
		_, m, errb := runAt(t, dir, "change", "plan", "skip", "--name", "lint", "--reason", "太晚")
		if errText(m, errb) == "" {
			t.Fatal("过了明确需求，不能直接改计划")
		}
		must(t, dir, ExitOK, "change", "plan", "propose", "--include", "lint")
		if must(t, dir, ExitOK, "status")["plan_pending"] == nil {
			t.Fatal("待确认的计划变更应出现在状态里")
		}
		if code, _, _ := runAt(t, dir, "check", "--name", "lint"); code == ExitOK {
			t.Fatal("确认前不能把检查加回来")
		}
		_, m, errb = runAt(t, dir, "change", "plan", "confirm")
		if !strings.Contains(errText(m, errb), "owner-confirmed") {
			t.Fatalf("确认必须显式声明是所有者: %s", errText(m, errb))
		}
		must(t, dir, ExitOK, "check", "--name", "unit")
		must(t, dir, ExitOK, "change", "plan", "confirm", "--owner-confirmed")
		st := must(t, dir, ExitOK, "status")
		if st["plan_pending"] != nil {
			t.Fatal("确认后不应再挂着待确认变更")
		}
		for _, c := range st["checks"].([]any) {
			cv := c.(map[string]any)
			if cv["name"] == "unit" && cv["effective_status"] == "pass" {
				t.Fatal("计划变更应作废已有证据")
			}
		}
		if code, _, _ := runAt(t, dir, "check", "--name", "lint"); code != ExitOK {
			t.Fatal("所有者确认后，可选检查才进入计划")
		}
	})
}
