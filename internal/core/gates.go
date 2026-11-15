package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (c *Change) artifact(stage string) *Artifact {
	for i := range c.Artifacts {
		if c.Artifacts[i].Stage == stage {
			return &c.Artifacts[i]
		}
	}
	return nil
}

func (c *Change) latestBody(stage string) (map[string]any, bool) {
	a := c.artifact(stage)
	if a == nil || len(a.Revisions) == 0 {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(a.Revisions[len(a.Revisions)-1].Body, &m); err != nil {
		return nil, false
	}
	return m, true
}

func (c *Change) addArtifact(stage string, body []byte) {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	rev := ArtifactRev{At: time.Now().UTC(), Hash: hash, Body: append(json.RawMessage{}, body...)}
	for i := range c.Artifacts {
		if c.Artifacts[i].Stage == stage {
			if n := len(c.Artifacts[i].Revisions); n > 0 {
				rev.Supersedes = c.Artifacts[i].Revisions[n-1].Hash
			}
			c.Artifacts[i].Revisions = append(c.Artifacts[i].Revisions, rev)
			return
		}
	}
	c.Artifacts = append(c.Artifacts, Artifact{Stage: stage, Revisions: []ArtifactRev{rev}})
}

func (c *Change) effective(run CheckRun, fp Fingerprint) string {
	if run.CandidateKind != "ok" || run.Status == "blocked" {
		return "blocked"
	}
	if run.Generation != c.Generation || run.Fingerprint != fp.Hash || fp.Kind != "ok" {
		return "stale"
	}
	return run.Status
}

func (c *Change) latestRun(name string) *CheckRun {
	var found *CheckRun
	for i := range c.Runs {
		if c.Runs[i].Name == name {
			found = &c.Runs[i]
		}
	}
	return found
}

func (c *Change) liveAccept(what string, fp Fingerprint) *Accept {
	if fp.Kind != "ok" {
		return nil
	}
	var found *Accept
	for i := range c.Accepts {
		a := &c.Accepts[i]
		if a.What == what && !a.Voided && a.Generation == c.Generation && a.Fingerprint == fp.Hash {
			found = a
		}
	}
	return found
}

func (v *VersionFact) current(gen int, fp Fingerprint) bool {
	return v != nil && fp.Kind == "ok" && v.Generation == gen && v.Fingerprint == fp.Hash
}

func (c *Change) voidDownstream() {
	c.Generation++
	for i := range c.Accepts {
		c.Accepts[i].Voided = true
	}
	c.ToolVersion = nil
}

func validateArtifact(stage, profile string, m map[string]any) error {
	need := func(keys ...string) error {
		for _, k := range keys {
			v, ok := m[k]
			if !ok {
				return fmt.Errorf("阶段「%s」的产物缺少字段 %s", stage, k)
			}
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) == "" {
					return fmt.Errorf("阶段「%s」的字段 %s 为空", stage, k)
				}
			case []any:
				if len(t) == 0 {
					return fmt.Errorf("阶段「%s」的字段 %s 为空", stage, k)
				}
			default:
				return fmt.Errorf("阶段「%s」的字段 %s 类型不对", stage, k)
			}
		}
		return nil
	}
	small := profile == "small"
	switch stage {
	case StageDemand:
		if small {
			return need("goal")
		}
		return need("goal", "acceptance", "baseline")
	case StagePlan:
		if small {
			return need("approach")
		}
		return need("approach", "risks", "prechecks")
	case StageBuild, StageAccept:
		return need("summary")
	case StageRelease:
		return need("observation")
	case StageRetro:
		return need("summary")
	default:
		return fmt.Errorf("未知阶段 %s", stage)
	}
}

func (c *Change) requirementMet(req Requirement, proj Project, fp Fingerprint) (bool, string) {
	spec := findCheck(proj, req.Check)
	if spec == nil {
		return false, "要求 " + req.ID + " 指向不存在的检查 " + req.Check
	}
	if layerRank[spec.Layer] < layerRank[req.MinLayer] {
		return false, "检查 " + spec.Name + " 的层次 " + spec.Layer + " 低于要求 " + req.MinLayer
	}
	if req.Independent && spec.When == "self" {
		return false, "要求 " + req.ID + " 需要独立验收来源，自验检查 " + spec.Name + " 不能改名充当"
	}
	run := c.latestRun(spec.Name)
	if run == nil {
		return false, "检查 " + spec.Name + " 未运行"
	}
	eff := c.effective(*run, fp)
	if eff != "pass" {
		return false, "检查 " + spec.Name + " 的有效结果是 " + eff
	}
	return true, ""
}

func findCheck(p Project, name string) *CheckSpec {
	for i := range p.Checks {
		if p.Checks[i].Name == name {
			return &p.Checks[i]
		}
	}
	return nil
}

func (c *Change) checksOf(p Project, when string) []CheckSpec {
	var out []CheckSpec
	for _, ch := range p.Checks {
		if ch.When == when {
			out = append(out, ch)
		}
	}
	return out
}

func (c *Change) requiredFresh(p Project, when string, fp Fingerprint) []string {
	var bad []string
	for _, ch := range c.checksOf(p, when) {
		if !ch.Required {
			continue
		}
		run := c.latestRun(ch.Name)
		if run == nil {
			bad = append(bad, ch.Name+" 未运行")
			continue
		}
		eff := c.effective(*run, fp)
		if eff != "pass" {
			bad = append(bad, ch.Name+" 为 "+eff)
		}
	}
	return bad
}

func (c *Change) gate(p Project, fp Fingerprint) (string, []string) {
	if c.Status == StatusCancel {
		return "", []string{"变更已取消"}
	}
	if c.Status == StatusClosed {
		return "", []string{"变更已正常关闭"}
	}
	if c.Status == StatusBlocked {
		return "", []string{"变更被阻塞：" + c.Block.Reason}
	}
	if _, ok := c.latestBody(c.Stage); !ok {
		return "", []string{"当前阶段「" + c.Stage + "」还没有产物"}
	}
	switch c.Stage {
	case StageDemand:
		return StagePlan, nil
	case StagePlan:
		return StageBuild, nil
	case StageBuild:
		if fp.Kind != "ok" {
			return "", []string{fp.Note}
		}
		if !hasRequiredCheck(p, "self") {
			return "", []string{"没有配置必过的自验检查。空检查配置不能通过实现与自验"}
		}
		bad := c.requiredFresh(p, "self", fp)
		if len(bad) > 0 {
			return "", bad
		}
		return StageAccept, nil
	case StageAccept:
		if fp.Kind != "ok" {
			return "", []string{fp.Note}
		}
		var bad []string
		if !hasRequiredCheck(p, "acceptance") && !hasRequiredRequirement(p) {
			bad = append(bad, "没有配置必过的验收检查或验收要求。空配置不能通过验收与交付")
		}
		bad = append(bad, c.requiredFresh(p, "acceptance", fp)...)
		for _, req := range p.Requirements {
			if !req.Required {
				continue
			}
			ok, why := c.requirementMet(req, p, fp)
			if !ok {
				bad = append(bad, why)
			}
		}
		if c.liveAccept("delivery", fp) == nil {
			bad = append(bad, "交付尚未被接受。检查通过不等于交付接受")
		}
		if len(bad) > 0 {
			return "", bad
		}
		if c.Release {
			return StageRelease, nil
		}
		return StageRetro, nil
	case StageRelease:
		if c.liveAccept("release", fp) == nil {
			return "", []string{"还没有对当前候选的发布声明。声明不是工具读回，候选变化后要重新声明"}
		}
		bad := c.requiredFresh(p, "release", fp)
		if len(bad) > 0 {
			return "", bad
		}
		return StageRetro, nil
	case StageRetro:
		for _, pb := range c.Problems {
			if pb.Disposition == "" || strings.TrimSpace(pb.Reason) == "" {
				return "", []string{"问题 " + pb.ID + " 还没有处置去向和理由"}
			}
		}
		return StatusClosed, nil
	default:
		return "", []string{"未知阶段"}
	}
}

func hasRequiredCheck(p Project, when string) bool {
	for _, ch := range p.Checks {
		if ch.When == when && ch.Required {
			return true
		}
	}
	return false
}

func hasRequiredRequirement(p Project) bool {
	for _, req := range p.Requirements {
		if req.Required {
			return true
		}
	}
	return false
}

func (c *Change) verifiedFixed(pb Problem, fp Fingerprint) bool {
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
	if fail == nil || pass == nil {
		return false
	}
	if fail.Name != pass.Name || fail.Status != "fail" || !pass.Ended.After(fail.Ended) {
		return false
	}
	return c.effective(*pass, fp) == "pass"
}

func nextActions(c *Change, p Project, fp Fingerprint) []string {
	if c == nil {
		return []string{"创建变更"}
	}
	if c.Status == StatusClosed {
		return []string{}
	}
	if c.Status == StatusCancel {
		return []string{}
	}
	if c.Status == StatusBlocked {
		return []string{"恢复", "取消"}
	}
	var n []string
	n = append(n, "提交当前阶段产物")
	if c.Stage == StageBuild || c.Stage == StageAccept || c.Stage == StageRelease {
		n = append(n, "运行检查")
	}
	if _, bad := c.gate(p, fp); len(bad) == 0 {
		n = append(n, "申请推进")
	}
	if stageIndex(c.Stage) > 0 {
		n = append(n, "申请回退")
	}
	if c.Stage == StageAccept {
		n = append(n, "记录交付接受")
	}
	if c.Stage == StageRelease {
		n = append(n, "记录发布声明")
	}
	if c.Stage == StageRetro {
		n = append(n, "记录问题处置")
	}
	n = append(n, "阻塞", "取消")
	return n
}
