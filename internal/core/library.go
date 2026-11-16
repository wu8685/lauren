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
		if in.Disposition == "rejected" {
			pb.Status = "rejected"
		}
	}
	pb.Occurrences = append(pb.Occurrences, Occurrence{At: now, ChangeID: ch.ID, Environment: ch.Policy.Environment, Note: in.Note})
	pb.Count = len(pb.Occurrences)
	promoted := false
	if promote && pb.Status == "open" && pb.Disposition != "rejected" && pb.Count >= proj.promoteThreshold() {
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

func promoteIntoProject(root string, proj *Project, pb *LibraryProblem) error {
	layer, gate := mapProblemLayer(pb.Layer)
	name := pb.TargetCheck
	if name == "" {
		name = "promoted-" + pb.ID
	}
	if findCheck(*proj, name) == nil {
		envs := []string{"development", "integration"}
		if gate == GateAcceptance {
			envs = []string{"integration", "staging"}
		}
		if gate == GateRelease {
			envs = []string{"staging", "production"}
		}
		proj.Checks = append(proj.Checks, CheckSpec{
			Name: name, Layer: layer, Gate: gate, Argv: []string{"./checks/promoted/" + name + ".sh"},
			Required: true, Environments: envs,
		})
		script := filepath.Join(root, "checks", "promoted", name+".sh")
		if _, err := os.Stat(script); errors.Is(err, os.ErrNotExist) {
			body := "#!/bin/sh\necho \"promoted from " + pb.ID + "\"\nexit 1\n"
			if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
				return err
			}
		}
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
		proj.Requirements = append(proj.Requirements, Requirement{
			ID: "req-" + pb.ID, Claim: claim, MinLayer: layer, Required: true, Check: name, Independent: gate == GateAcceptance,
		})
	}
	if err := writeAtomicJSON(filepath.Join(root, "lauren.project.json"), proj); err != nil {
		return err
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
