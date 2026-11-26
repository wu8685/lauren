package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (p Project) reportsDir() string {
	d := strings.TrimSpace(p.Defaults.ReportsDir)
	if d == "" {
		return ".lauren/reports"
	}
	return filepath.ToSlash(filepath.Clean(d))
}

func validateReportsDir(d string) error {
	if strings.TrimSpace(d) == "" {
		return nil
	}
	if filepath.IsAbs(d) || strings.Contains(d, "..") {
		return fmt.Errorf("defaults.reports_dir 必须是 .lauren 下的相对路径")
	}
	clean := filepath.ToSlash(filepath.Clean(d))
	if clean == ".lauren" || !strings.HasPrefix(clean, ".lauren/") {
		return fmt.Errorf("defaults.reports_dir 必须在 .lauren/ 下")
	}
	if clean == ".lauren/runtime" || strings.HasPrefix(clean, ".lauren/runtime/") || clean == ".lauren/problems" || strings.HasPrefix(clean, ".lauren/problems/") {
		return fmt.Errorf("defaults.reports_dir 不能占用 runtime 或 problems")
	}
	return nil
}

func reportPathOK(proj Project, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("报告路径必须是项目内的相对路径")
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	dir := proj.reportsDir()
	if clean != dir && !strings.HasPrefix(clean, dir+"/") {
		return "", fmt.Errorf("报告必须放在 %s", dir)
	}
	return clean, nil
}

func fileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func validSourceKind(kind string) bool {
	switch kind {
	case "self", "declared", "external":
		return true
	default:
		return false
	}
}

func (c *Change) latestReport(requirement string) *QAReport {
	var found *QAReport
	for i := range c.Reports {
		if c.Reports[i].Requirement == requirement {
			found = &c.Reports[i]
		}
	}
	return found
}

func requirementCheck(proj Project, id string) string {
	for _, req := range proj.Requirements {
		if req.ID == id {
			return req.Check
		}
	}
	return ""
}

func (c *Change) describeReport(root string, proj Project, rep QAReport, fp Fingerprint) (bool, string) {
	if latest := c.latestReport(rep.Requirement); latest != nil && latest.ID != rep.ID {
		return false, "已被同一要求的更新报告代替"
	}
	spec := findCheck(proj, requirementCheck(proj, rep.Requirement))
	note := c.reportGate(root, proj, Requirement{ID: rep.Requirement, RequiresReport: true}, spec, fp)
	return note == "", note
}

func (c *Change) reportGate(root string, proj Project, req Requirement, spec *CheckSpec, fp Fingerprint) string {
	if !req.RequiresReport {
		return ""
	}
	rep := c.latestReport(req.ID)
	if rep == nil {
		return "要求 " + req.ID + " 需要 QA 报告，还没有提交"
	}
	if fp.Kind != "ok" || rep.Fingerprint != fp.Hash {
		return "要求 " + req.ID + " 的 QA 报告不属于当前候选"
	}
	if spec != nil && !envAccepted(*spec, proj, rep.Environment) {
		return "要求 " + req.ID + " 的 QA 报告环境不被该检查接受"
	}
	full := filepath.Join(root, filepath.FromSlash(rep.Path))
	sum, err := fileDigest(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "要求 " + req.ID + " 的 QA 报告文件缺失"
		}
		return "要求 " + req.ID + " 的 QA 报告无法读取"
	}
	if sum != rep.Digest {
		return "要求 " + req.ID + " 的 QA 报告摘要不一致"
	}
	return ""
}
