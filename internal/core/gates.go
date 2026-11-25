package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (c *Change) syncCatalog(proj Project) {
	h := catalogHash(proj)
	if c.CatalogHash == "" {
		c.CatalogHash = h
		return
	}
	if c.CatalogHash != h {
		c.CatalogHash = h
		c.invalidateEvidence()
		c.History = append(c.History, Event{At: time.Now().UTC(), Kind: "catalog", Message: "checks、requirements 或检查超时变了，已有证据作废"})
	}
}

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

func (c *Change) voidConclusions() {
	for i := range c.Accepts {
		c.Accepts[i].Voided = true
	}
	c.DeclaredVersion = nil
}

func (c *Change) invalidateEvidence() {
	c.EvidenceGeneration++
	c.voidConclusions()
}

func (c *Change) effective(run CheckRun, fp Fingerprint) string {
	if run.Status == "blocked" {
		return "blocked"
	}
	if run.BindKind == BindIntegration {
		if c.Merge == nil || c.Merge.Withdrawn || c.Merge.SHA != run.IntegrationSHA || c.Merge.Branch != run.IntegrationBranch {
			return "stale"
		}
		if run.EvidenceGeneration != c.EvidenceGeneration {
			return "stale"
		}
		return run.Status
	}
	if run.CandidateKind != "ok" {
		return "blocked"
	}
	if run.EvidenceGeneration != c.EvidenceGeneration || fp.Kind != "ok" || run.Fingerprint != fp.Hash {
		return "stale"
	}
	return run.Status
}

func envAccepted(spec CheckSpec, proj Project, env string) bool {
	if spec.Layer == "release_readback" || spec.Gate == GateRelease {
		allowed := spec.Environments
		if len(allowed) == 0 {
			allowed = []string{"staging", "production"}
		}
		return containsStr(allowed, env)
	}
	if len(spec.Environments) > 0 {
		return containsStr(spec.Environments, env)
	}
	return containsStr(proj.Environments, env)
}

func (c *Change) satisfying(spec CheckSpec, proj Project, fp Fingerprint) (*CheckRun, string) {
	// 每个被接受的环境只看自己仍适用的最新一条。另一个环境或另一项检查的结果不覆盖它。
	latest := map[string]int{}
	for i := range c.Runs {
		r := &c.Runs[i]
		if r.Name != spec.Name || !envAccepted(spec, proj, r.Environment) {
			continue
		}
		if c.effective(*r, fp) == "stale" {
			continue
		}
		latest[r.Environment] = i
	}
	if len(latest) == 0 {
		return nil, spec.Name + " 没有仍有效、且环境被接受的通过记录"
	}
	envs := make([]string, 0, len(latest))
	for env := range latest {
		envs = append(envs, env)
	}
	sort.Strings(envs)
	var pass *CheckRun
	var bad []string
	for _, env := range envs {
		r := &c.Runs[latest[env]]
		eff := c.effective(*r, fp)
		if eff == "pass" {
			if pass == nil || !r.Ended.Before(pass.Ended) {
				pass = r
			}
			continue
		}
		bad = append(bad, fmt.Sprintf("%s 在 %s 的最新结果是 %s，不能沿用更早的通过", spec.Name, env, eff))
	}
	if pass != nil {
		return pass, ""
	}
	return nil, strings.Join(bad, "；")
}

func (c *Change) liveAccept(what, bindKind, key string) *Accept {
	var found *Accept
	for i := range c.Accepts {
		a := &c.Accepts[i]
		if a.What == what && !a.Voided && a.BindKind == bindKind && a.Fingerprint == key {
			found = a
		}
	}
	return found
}

func validateArtifact(stage, level string, m map[string]any) error {
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
	short := level == LevelAbbreviated
	switch stage {
	case StageDemand:
		if short {
			return need("goal")
		}
		return need("goal", "acceptance", "baseline")
	case StagePlan:
		if short {
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

func (c *Change) requirementMet(root string, req Requirement, proj Project, fp Fingerprint) (bool, string) {
	spec := findCheck(proj, req.Check)
	if spec == nil {
		return false, "要求 " + req.ID + " 指向不存在的检查 " + req.Check
	}
	if layerRank[spec.Layer] < layerRank[req.MinLayer] {
		return false, "检查 " + spec.Name + " 的层次 " + spec.Layer + " 低于要求 " + req.MinLayer
	}
	if req.Independent && spec.Gate == GateSelf {
		return false, "要求 " + req.ID + " 需要独立验收来源，自验检查 " + spec.Name + " 不能改名充当"
	}
	if _, why := c.satisfying(*spec, proj, fp); why != "" {
		return false, why
	}
	if why := c.reportGate(root, proj, req, spec, fp); why != "" {
		return false, why
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

func (c *Change) requiredFresh(p Project, gate string, fp Fingerprint) []string {
	var bad []string
	for _, ch := range p.Checks {
		if ch.Gate != gate || !ch.Required {
			continue
		}
		if _, why := c.satisfying(ch, p, fp); why != "" {
			bad = append(bad, why)
		}
	}
	return bad
}

func (c *Change) mergedNow() bool {
	return c.Merge != nil && !c.Merge.Withdrawn && c.Merge.Branch == c.Policy.IntegrationBranch && c.Policy.IntegrationIntent == IntentBranch
}

func (c *Change) gate(root string, p Project, fp Fingerprint, libs []*LibraryProblem) (string, []string) {
	if c.Status == StatusCancel {
		return "", []string{"变更已取消"}
	}
	if c.Status == StatusClosed {
		return "", []string{"变更已正常关闭"}
	}
	if c.Status == StatusBlocked {
		return "", []string{"变更被阻塞：" + c.Block.Reason}
	}
	body, ok := c.latestBody(c.Stage)
	if !ok {
		return "", []string{"当前阶段「" + c.Stage + "」还没有产物"}
	}
	if err := validateArtifact(c.Stage, c.Policy.ArtifactLevel, body); err != nil {
		return "", []string{err.Error()}
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
		if !hasRequiredCheck(p, GateSelf) {
			return "", []string{"没有配置必过的自验检查。空检查配置不能通过实现与自验"}
		}
		bad := c.requiredFresh(p, GateSelf, fp)
		if len(bad) > 0 {
			return "", bad
		}
		return StageAccept, nil
	case StageAccept:
		if fp.Kind != "ok" {
			return "", []string{fp.Note}
		}
		var bad []string
		if !hasRequiredCheck(p, GateAcceptance) && !hasRequiredRequirement(p) {
			bad = append(bad, "没有配置必过的验收检查或验收要求。空配置不能通过验收与交付")
		}
		bad = append(bad, c.requiredFresh(p, GateAcceptance, fp)...)
		for _, req := range p.Requirements {
			if !req.Required {
				continue
			}
			ok, why := c.requirementMet(root, req, p, fp)
			if !ok {
				bad = append(bad, why)
			}
		}
		if c.liveAccept("delivery", BindWorktree, fp.Hash) == nil {
			bad = append(bad, "交付尚未被接受。检查通过不等于交付接受")
		}
		if len(bad) > 0 {
			return "", bad
		}
		if c.Policy.ReleaseStage == ReleaseExcluded {
			return StageRetro, nil
		}
		if c.Policy.IntegrationIntent != IntentBranch {
			return "", []string{"本次流程暂不纳入当前迭代，不能进入共享环境的发布与观察"}
		}
		if c.Policy.IntegrationBranch != p.Defaults.IntegrationBranch {
			return "", []string{"集成分支还不是项目当前集成分支 " + p.Defaults.IntegrationBranch + "，不能占用共享预发或生产"}
		}
		if !c.mergedNow() {
			return "", []string{"尚未记录已合入集成分支 " + c.Policy.IntegrationBranch + "。不会自动 merge 或 push"}
		}
		return StageRelease, nil
	case StageRelease:
		if !c.mergedNow() {
			return "", []string{"集成记录已失效，不能把发布读回当作通过"}
		}
		if c.liveAccept("release", BindIntegration, c.Merge.SHA) == nil {
			return "", []string{"还没有对这个集成版本的发布声明。声明不是工具读回"}
		}
		if !hasReleaseReadback(p) {
			return "", []string{"启用了发布阶段，但没有必过的 release_readback。普通检查和发布声明都不能代替机器读回"}
		}
		bad := c.requiredFresh(p, GateRelease, fp)
		if len(bad) > 0 {
			return "", bad
		}
		return StageRetro, nil
	case StageRetro:
		var bad []string
		for _, pb := range libs {
			if pb == nil || !pb.seenIn(c.ID) {
				continue
			}
			if pb.Count >= p.promoteThreshold() && pb.Status != "promoted" && pb.Disposition != "rejected" {
				bad = append(bad, "问题 "+pb.ID+" 已达到晋升阈值但还没登记到已有检查。later 不能跳过；可以指定 target-check，或把处置写成 rejected")
			}
			if pb.Disposition == "" || strings.TrimSpace(pb.Reason) == "" {
				bad = append(bad, "问题 "+pb.ID+" 还没有处置去向和理由")
			}
		}
		for _, pb := range c.Problems {
			if pb.Disposition == "" || strings.TrimSpace(pb.Reason) == "" {
				bad = append(bad, "变更里还有未写入公共库的问题 "+pb.ID+"，还没有处置")
			}
		}
		if len(bad) > 0 {
			return "", bad
		}
		return StatusClosed, nil
	default:
		return "", []string{"未知阶段"}
	}
}

func isReleaseReadback(ch CheckSpec) bool {
	return ch.Required && ch.Gate == GateRelease && ch.Layer == "release_readback"
}

func hasReleaseReadback(p Project) bool {
	for _, ch := range p.Checks {
		if isReleaseReadback(ch) {
			return true
		}
	}
	return false
}

func (c *Change) releaseReadbackVerified(p Project, fp Fingerprint) bool {
	if !hasReleaseReadback(p) {
		return false
	}
	for _, ch := range p.Checks {
		if !isReleaseReadback(ch) {
			continue
		}
		if _, why := c.satisfying(ch, p, fp); why != "" {
			return false
		}
	}
	return true
}

func (c *Change) gateChecksMet(p Project, gate string, fp Fingerprint) bool {
	if !hasRequiredCheck(p, gate) {
		return false
	}
	return len(c.requiredFresh(p, gate, fp)) == 0
}

func (c *Change) independentRequirementsMet(root string, p Project, fp Fingerprint) bool {
	found := false
	for _, req := range p.Requirements {
		if !req.Required || !req.Independent {
			continue
		}
		found = true
		ok, _ := c.requirementMet(root, req, p, fp)
		if !ok {
			return false
		}
	}
	return found
}

func hasRequiredCheck(p Project, gate string) bool {
	for _, ch := range p.Checks {
		if ch.Gate == gate && ch.Required {
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

func (c *Change) earliestArtifactGap() string {
	for _, stage := range StageOrder {
		body, ok := c.latestBody(stage)
		if !ok {
			continue
		}
		if err := validateArtifact(stage, c.Policy.ArtifactLevel, body); err != nil {
			return stage
		}
	}
	return ""
}

func validatePolicy(p Project, pol Policy) error {
	if !containsStr(p.Environments, pol.Environment) {
		return fmt.Errorf("environment %s 不在项目 environments 里", pol.Environment)
	}
	if pol.ReleaseStage != ReleaseIncluded && pol.ReleaseStage != ReleaseExcluded {
		return fmt.Errorf("release_stage 只能是 included 或 excluded")
	}
	if pol.ArtifactLevel != LevelStandard && pol.ArtifactLevel != LevelAbbreviated {
		return fmt.Errorf("artifact_level 只能是 standard 或 abbreviated")
	}
	switch pol.IntegrationIntent {
	case IntentBranch:
		if strings.TrimSpace(pol.IntegrationBranch) == "" {
			return fmt.Errorf("integration_intent 为 branch 时必须给出 integration_branch")
		}
	case IntentNotCurrent:
		return nil
	default:
		return fmt.Errorf("integration_intent 只能是 branch 或 not_in_current_iteration")
	}
	return nil
}

func nextActions(root string, c *Change, p Project, fp Fingerprint, libs []*LibraryProblem) []string {
	if c == nil {
		return []string{"创建变更"}
	}
	if c.Status == StatusClosed || c.Status == StatusCancel {
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
	if _, bad := c.gate(root, p, fp, libs); len(bad) == 0 {
		n = append(n, "申请推进")
	}
	if stageIndex(c.Stage) > 0 {
		n = append(n, "申请回退")
	}
	if c.Stage == StageAccept {
		n = append(n, "记录交付接受")
		if c.Policy.ReleaseStage == ReleaseIncluded {
			n = append(n, "记录合入集成分支")
		}
	}
	if c.Stage == StageRelease {
		n = append(n, "记录发布声明")
	}
	if c.ReleaseHold != nil && c.ReleaseHold.Active {
		n = append(n, "人确认后释放共享环境占用")
	}
	if c.Stage == StageRetro {
		n = append(n, "记录问题处置")
	}
	n = append(n, "修订 policy", "阻塞", "取消")
	return n
}
