package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LibraryProblem struct {
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Status        string       `json:"status"`
	Layer         string       `json:"layer"`
	Symptom       string       `json:"symptom"`
	Repro         string       `json:"repro,omitempty"`
	Expected      string       `json:"expected,omitempty"`
	Actual        string       `json:"actual,omitempty"`
	Detail        string       `json:"detail,omitempty"`
	Defense       string       `json:"defense,omitempty"`
	TargetCheck   string       `json:"target_check,omitempty"`
	Scope         string       `json:"scope"`
	Count         int          `json:"count"`
	Occurrences   []Occurrence `json:"occurrences"`
	Disposition   string       `json:"disposition,omitempty"`
	Reason        string       `json:"reason,omitempty"`
	VerifiedFixed bool         `json:"verified_fixed"`
	PromotedCheck string       `json:"promoted_check,omitempty"`
	FailRun       string       `json:"fail_run,omitempty"`
	PassRun       string       `json:"pass_run,omitempty"`
}

type Occurrence struct {
	At          time.Time `json:"at"`
	ChangeID    string    `json:"change_id"`
	Environment string    `json:"environment,omitempty"`
	Note        string    `json:"note,omitempty"`
}

func problemsDir(root string) string {
	return filepath.Join(root, ".lauren", "problems")
}

func normKey(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func (p *LibraryProblem) sameAs(title, layer, symptom string) bool {
	return normKey(p.Title) == normKey(title) && normKey(p.Layer) == normKey(layer) && normKey(p.Symptom) == normKey(symptom)
}

func (p *LibraryProblem) seenIn(changeID string) bool {
	for _, oc := range p.Occurrences {
		if oc.ChangeID == changeID {
			return true
		}
	}
	return false
}

func loadLibrary(root string) ([]*LibraryProblem, error) {
	entries, err := os.ReadDir(problemsDir(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*LibraryProblem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(problemsDir(root), e.Name()))
		if err != nil {
			return nil, err
		}
		var pb LibraryProblem
		if err := json.Unmarshal(b, &pb); err != nil {
			return nil, fmt.Errorf("问题文件 %s 无法解析：%w", e.Name(), err)
		}
		out = append(out, &pb)
	}
	return out, nil
}

func saveLibraryProblem(root string, pb *LibraryProblem) error {
	if err := os.MkdirAll(problemsDir(root), 0o755); err != nil {
		return err
	}
	return writeAtomicJSON(filepath.Join(problemsDir(root), pb.ID+".json"), pb)
}

type problemInput struct {
	Title, Layer, Symptom, Repro, Expected, Actual, Detail, Defense, TargetCheck, Scope, Note, Disposition, Reason string
}

func recordProblem(root string, proj *Project, ch *Change, in problemInput, promote bool) (*LibraryProblem, bool, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, false, fmt.Errorf("问题需要标题")
	}
	if in.Layer == "" {
		in.Layer = "unit"
	}
	if in.Symptom == "" {
		in.Symptom = in.Title
	}
	if in.Scope == "" {
		in.Scope = "project"
	}
	if in.Scope != "project" && in.Scope != "change" {
		return nil, false, fmt.Errorf("适用范围只能是 project 或 change，不会自动变成全局规则")
	}
	libs, err := loadLibrary(root)
	if err != nil {
		return nil, false, err
	}
	var pb *LibraryProblem
	for _, item := range libs {
		if item.sameAs(in.Title, in.Layer, in.Symptom) {
			pb = item
			break
		}
	}
	now := time.Now().UTC()
	if pb == nil {
		pb = &LibraryProblem{
			ID: newID("p-"), Title: normKey(in.Title), Status: "open", Layer: normKey(in.Layer), Symptom: normKey(in.Symptom), Scope: in.Scope,
		}
	} else {
		// 新的一次出现不沿用上一轮的处置或已验证结论。
		pb.Disposition = ""
		pb.Reason = ""
		pb.VerifiedFixed = false
		pb.FailRun = ""
		pb.PassRun = ""
		if pb.Status != "promoted" {
			pb.Status = "open"
		}
	}
	fill := func(dst *string, src string) {
		if strings.TrimSpace(src) != "" {
			*dst = src
		}
	}
	fill(&pb.Repro, in.Repro)
	fill(&pb.Expected, in.Expected)
	fill(&pb.Actual, in.Actual)
	fill(&pb.Detail, in.Detail)
	fill(&pb.Defense, in.Defense)
	fill(&pb.TargetCheck, in.TargetCheck)
	pb.Scope = in.Scope
	if in.Disposition != "" {
		pb.Disposition = in.Disposition
		pb.Reason = in.Reason
		if in.Disposition == "rejected" && pb.Status != "promoted" {
			pb.Status = "rejected"
		}
	}
	pb.Occurrences = append(pb.Occurrences, Occurrence{At: now, ChangeID: ch.ID, Environment: ch.Policy.Environment, Note: in.Note})
	pb.Count = len(pb.Occurrences)
	promoted := false
	if promote && pb.Status == "open" && pb.Disposition != "rejected" && pb.Count >= proj.promoteThreshold() && pb.TargetCheck != "" {
		if err := promoteIntoProject(root, proj, pb); err != nil {
			return nil, false, err
		}
		promoted = true
	}
	if err := saveLibraryProblem(root, pb); err != nil {
		return nil, false, err
	}
	return pb, promoted, nil
}

func validCheckName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") || strings.Contains(name, " ") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func promoteIntoProject(root string, proj *Project, pb *LibraryProblem) error {
	name := pb.TargetCheck
	if !validCheckName(name) {
		return fmt.Errorf("达到晋升阈值时必须给出已有检查的 target-check，名称只能包含字母、数字、减号和下划线")
	}
	spec := findCheck(*proj, name)
	if spec == nil {
		return fmt.Errorf("target-check %s 不是项目里已有的检查，不会生成占位脚本", name)
	}
	wantLayer, _ := mapProblemLayer(pb.Layer)
	if layerRank[spec.Layer] < layerRank[wantLayer] {
		return fmt.Errorf("检查 %s 的层次 %s 低于问题层次 %s", name, spec.Layer, wantLayer)
	}
	claim := pb.Title
	exists := false
	for _, req := range proj.Requirements {
		if req.Check == name && normKey(req.Claim) == normKey(claim) {
			exists = true
			break
		}
	}
	if !exists {
		_, gate := mapProblemLayer(pb.Layer)
		proj.Requirements = append(proj.Requirements, Requirement{
			ID: "req-" + pb.ID, Claim: claim, MinLayer: wantLayer, Required: true, Check: name, Independent: gate == GateAcceptance,
		})
		if err := writeAtomicJSON(filepath.Join(root, "lauren.project.json"), proj); err != nil {
			return err
		}
	}
	pb.Status = "promoted"
	pb.PromotedCheck = name
	return nil
}

func mapProblemLayer(layer string) (string, string) {
	switch normKey(layer) {
	case "lint", "static":
		return "static", GateSelf
	case "unit":
		return "unit", GateSelf
	case "regression", "integration":
		return "integration", GateSelf
	case "interaction":
		return "interaction", GateAcceptance
	case "release", "release_readback":
		return "release_readback", GateRelease
	default:
		if layerRank[layer] > 0 {
			if layer == "interaction" {
				return layer, GateAcceptance
			}
			if layer == "release_readback" {
				return layer, GateRelease
			}
			return layer, GateSelf
		}
		return "static", GateSelf
	}
}

func promoteDue(root string, proj *Project, ch *Change) (bool, error) {
	libs, err := loadLibrary(root)
	if err != nil {
		return false, err
	}
	changed := false
	for _, pb := range libs {
		if pb == nil || !pb.seenIn(ch.ID) || pb.Status != "open" || pb.Disposition == "rejected" {
			continue
		}
		if pb.Count < proj.promoteThreshold() || pb.TargetCheck == "" {
			continue
		}
		if err := promoteIntoProject(root, proj, pb); err != nil {
			return changed, err
		}
		if err := saveLibraryProblem(root, pb); err != nil {
			return changed, err
		}
		changed = true
	}
	if changed {
		ch.invalidateEvidence()
		ch.CatalogHash = catalogHash(*proj)
	}
	return changed, nil
}

func libsFor(root string, ch *Change) []*LibraryProblem {
	libs, err := loadLibrary(root)
	if err != nil {
		return nil
	}
	var out []*LibraryProblem
	for _, pb := range libs {
		if pb.seenIn(ch.ID) {
			out = append(out, pb)
		}
	}
	return out
}

func (c *Change) libraryVerified(pb *LibraryProblem, fp Fingerprint) bool {
	if pb.FailRun == "" || pb.PassRun == "" {
		return false
	}
	var fail, pass *CheckRun
	for i := range c.Runs {
		if c.Runs[i].ID == pb.FailRun {
			fail = &c.Runs[i]
		}
		if c.Runs[i].ID == pb.PassRun {
			pass = &c.Runs[i]
		}
	}
	if fail == nil || pass == nil || fail.Name != pass.Name || fail.Status != "fail" || !pass.Ended.After(fail.Ended) {
		return false
	}
	return c.effective(*pass, fp) == "pass"
}
