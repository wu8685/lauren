package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQAReportDigestGates(t *testing.T) {
	// 报告夹具显式关闭 Red。理由：这里只测摘要和文件，不重放分类失败。
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "reports_dir": ".lauren/reports", "red_gate": "off"},
	  "checks": [
	    {"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true},
	    {"name": "interact", "layer": "interaction", "gate": "acceptance", "argv": ["./checks/interact.sh"], "required": true, "environments": ["integration"]}
	  ],
	  "requirements": [{"id": "click", "claim": "保存", "min_layer": "interaction", "required": true, "check": "interact", "independent": true, "requires_report": true}]
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "interact.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "accept.json"), `{"summary":"a"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "报告")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	_, m, errb := runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "报告") {
		t.Fatalf("没有 QA 报告不能通过需要报告的要求: %s", errText(m, errb))
	}

	report := filepath.Join(dir, ".lauren", "reports", "qa.txt")
	writeFile(t, report, "first\n")
	m = must(t, dir, ExitOK, "change", "report", "submit", "--requirement", "click", "--file", ".lauren/reports/qa.txt", "--source", "external", "--environment", "integration")
	reps := m["reports"].([]any)
	if len(reps) != 1 || reps[0].(map[string]any)["source_verified"] != false || reps[0].(map[string]any)["source_kind"] != "external" {
		t.Fatalf("来源只是声明: %#v", reps)
	}
	if must(t, dir, ExitOK, "change", "advance")["stage"] != StageRetro {
		t.Fatal("摘要匹配时应能离开验收")
	}

	// 新开一轮，证明摘要不符或缺文件不能满足门禁。
	dir = t.TempDir()
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main", "red_gate": "off"},
	  "checks": [
	    {"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true},
	    {"name": "interact", "layer": "interaction", "gate": "acceptance", "argv": ["./checks/interact.sh"], "required": true, "environments": ["integration"]}
	  ],
	  "requirements": [{"id": "click", "claim": "保存", "min_layer": "interaction", "required": true, "check": "interact", "independent": true, "requires_report": true}]
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "checks", "interact.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "accept.json"), `{"summary":"a"}`)
	gitInit(t, dir)
	must(t, dir, ExitOK, "change", "create", "--goal", "摘要")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "unit")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/accept.json")
	must(t, dir, ExitOK, "check", "--name", "interact", "--environment", "integration")
	must(t, dir, ExitOK, "change", "accept", "--what", "delivery", "--source", "human", "--reason", "接受", "--scope", "fixture")
	writeFile(t, filepath.Join(dir, ".lauren", "reports", "qa.txt"), "body\n")
	must(t, dir, ExitOK, "change", "report", "submit", "--requirement", "click", "--file", ".lauren/reports/qa.txt", "--source", "declared", "--environment", "integration")
	writeFile(t, filepath.Join(dir, ".lauren", "reports", "qa.txt"), "changed\n")
	_, m, errb = runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "摘要") {
		t.Fatalf("摘要不符不能通过: %s", errText(m, errb))
	}
	os.Remove(filepath.Join(dir, ".lauren", "reports", "qa.txt"))
	_, m, errb = runAt(t, dir, "change", "advance")
	if !strings.Contains(errText(m, errb), "缺失") {
		t.Fatalf("报告缺失不能通过: %s", errText(m, errb))
	}
	_, m, errb = runAt(t, dir, "change", "report", "submit", "--requirement", "click", "--file", "artifacts/accept.json", "--source", "self")
	if !strings.Contains(errText(m, errb), "reports") {
		t.Fatalf("报告必须在报告目录里: %s", errText(m, errb))
	}
}
