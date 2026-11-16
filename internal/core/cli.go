package core

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type app struct {
	json bool
	root string
	out  io.Writer
	err  io.Writer
}

func Run(args []string, stdout, stderr io.Writer) int {
	a := &app{out: stdout, err: stderr}
	fs := flag.NewFlagSet("lauren", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&a.json, "json", false, "稳定 JSON 输出")
	fs.StringVar(&a.root, "root", "", "项目根目录")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		a.fail(ExitUsage, "用法：lauren init | change | check | status | history | next | skills")
		return ExitUsage
	}
	switch rest[0] {
	case "init":
		return a.cmdInit(rest[1:])
	case "check":
		return a.cmdCheck(rest[1:])
	case "status":
		return a.cmdStatus(rest[1:])
	case "history":
		return a.cmdHistory(rest[1:])
	case "next":
		return a.cmdNext(rest[1:])
	case "skills":
		return a.cmdSkills(rest[1:])
	case "change":
		return a.cmdChange(rest[1:])
	case "stage", "pass":
		a.fail(ExitUsage, "没有直接设置阶段或填写 pass 的命令")
		return ExitUsage
	default:
		a.fail(ExitUsage, "未知命令 "+rest[0])
		return ExitUsage
	}
}

func (a *app) projectRoot() (string, error) {
	if a.root != "" {
		if _, err := os.Stat(filepath.Join(a.root, "lauren.project.json")); err != nil {
			return "", err
		}
		abs, err := filepath.Abs(a.root)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findRoot(wd)
}

func (a *app) emit(code int, v any) int {
	if a.json {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(v)
		return code
	}
	return code
}

func (a *app) fail(code int, msg string) int {
	if a.json {
		return a.emit(code, map[string]any{"ok": false, "error": msg})
	}
	fmt.Fprintln(a.err, msg)
	return code
}

func (a *app) cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(a.err)
	env := fs.String("environment", "development", "defaults.environment")
	release := fs.String("release-stage", ReleaseExcluded, "included 或 excluded")
	level := fs.String("artifact-level", LevelStandard, "standard 或 abbreviated")
	branch := fs.String("integration-branch", "", "项目默认集成分支")
	dir := fs.String("dir", "", "要初始化的目录")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	root := *dir
	if root == "" {
		if a.root != "" {
			root = a.root
		} else {
			wd, err := os.Getwd()
			if err != nil {
				return a.fail(ExitInternal, err.Error())
			}
			root = wd
		}
	}
	if strings.TrimSpace(*branch) == "" {
		return a.fail(ExitUsage, "init 需要 --integration-branch。引擎不写死分支名")
	}
	path := filepath.Join(root, "lauren.project.json")
	if _, err := os.Stat(path); err == nil {
		return a.fail(ExitUsage, "lauren.project.json 已存在")
	}
	p := Project{
		Schema:       1,
		Environments: []string{"development", "integration", "staging", "production"},
		Defaults: Defaults{
			Environment: *env, ReleaseStage: *release, ArtifactLevel: *level, IntegrationBranch: *branch,
			PromoteThreshold: 2,
		},
		Checks: []CheckSpec{}, Requirements: []Requirement{},
	}
	if err := p.validate(); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return a.fail(ExitInternal, err.Error())
	}
	_ = os.MkdirAll(runtimeDir(root), 0o755)
	_ = ensureGitignore(root)
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "init", "config": path, "runtime": runtimeDir(root)})
	}
	fmt.Fprintf(a.out, "已写入 %s\n运行状态在 %s，按变更分目录，不进 git。\n", path, runtimeDir(root))
	return ExitOK
}

func ensureGitignore(root string) error {
	line := ".lauren/runtime/"
	p := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if strings.Contains(string(b), line) {
		return nil
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(line + "\n")
	return err
}

func (a *app) cmdChange(args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, "用法：lauren change create|submit|advance|rollback|block|resume|cancel|accept|declare-version|policy|integration|release|problem")
	}
	switch args[0] {
	case "create":
		return a.changeCreate(args[1:])
	case "submit":
		return a.changeSubmit(args[1:])
	case "advance":
		return a.changeAdvance(args[1:])
	case "rollback":
		return a.changeRollback(args[1:])
	case "block":
		return a.changeBlock(args[1:])
	case "resume":
		return a.changeResume(args[1:])
	case "cancel":
		return a.changeCancel(args[1:])
	case "accept":
		return a.changeAccept(args[1:])
	case "declare-version":
		return a.changeVersion(args[1:])
	case "policy":
		return a.changePolicy(args[1:])
	case "integration":
		return a.changeIntegration(args[1:])
	case "release":
		return a.changeRelease(args[1:])
	case "problem":
		return a.changeProblem(args[1:])
	case "stage", "pass":
		return a.fail(ExitUsage, "没有直接设置阶段或填写 pass 的命令")
	default:
		return a.fail(ExitUsage, "未知 change 子命令 "+args[0])
	}
}

func (a *app) withChange(id string, fn func(*Change, Project, Fingerprint) (int, any)) int {
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	proj, err := loadProject(root)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	id, err = resolveChangeID(root, id, true)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	fp := fingerprint(root, proj)
	var code int
	var payload any
	err = WithChange(root, id, func(ch *Change) error {
		ch.syncCatalog(proj)
		code, payload = fn(ch, proj, fp)
		return nil
	})
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if payload == nil {
		return code
	}
	if a.json {
		return a.emit(code, payload)
	}
	if view, ok := payload.(statusPayload); ok {
		writeHuman(a.out, view)
		return code
	}
	fmt.Fprintln(a.out, payload)
	return code
}

func (a *app) changeCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(a.err)
	goal := fs.String("goal", "", "变更目标")
	env := fs.String("environment", "", "覆盖 defaults.environment")
	release := fs.String("release-stage", "", "included 或 excluded")
	level := fs.String("artifact-level", "", "standard 或 abbreviated")
	branch := fs.String("integration-branch", "", "覆盖集成分支")
	intent := fs.String("integration-intent", "", "branch 或 not_in_current_iteration")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*goal) == "" {
		return a.fail(ExitUsage, "需要 --goal")
	}
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	proj, err := loadProject(root)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	pol := overlayPolicy(Policy{
		Environment: proj.Defaults.Environment, ReleaseStage: proj.Defaults.ReleaseStage,
		ArtifactLevel: proj.Defaults.ArtifactLevel, IntegrationBranch: proj.Defaults.IntegrationBranch,
		IntegrationIntent: IntentBranch,
	}, *env, *release, *level, *branch, *intent)
	if err := validatePolicy(proj, pol); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	now := time.Now().UTC()
	created := Change{
		ID: newID("c-"), Goal: *goal, Stage: StageDemand, Status: StatusActive, Policy: pol,
		CatalogHash: catalogHash(proj), CreatedAt: now,
		History: []Event{{At: now, Kind: "create", Message: *goal}},
	}
	if err := createChange(root, &created); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "change create", "id": created.ID, "stage": created.Stage, "policy": created.Policy})
	}
	fmt.Fprintf(a.out, "变更 %s\n阶段: %s\n环境: %s\n发布阶段: %s\n产物级别: %s\n集成: %s %s\n",
		created.ID, created.Stage, pol.Environment, pol.ReleaseStage, pol.ArtifactLevel, pol.IntegrationIntent, pol.IntegrationBranch)
	return ExitOK
}

func (a *app) changeSubmit(args []string) int {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	stage := fs.String("stage", "", "")
	art := fs.String("artifact", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *art == "" {
		return a.fail(ExitUsage, "需要 --artifact")
	}
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	full, err := safePath(root, *art)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if len(raw) > 256*1024 {
		return a.fail(ExitUsage, "产物超过 256KB")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return a.fail(ExitUsage, "产物不是 JSON 对象")
	}
	canon, _ := json.Marshal(m)
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		st := *stage
		if st == "" {
			st = ch.Stage
		}
		if stageIndex(st) < 0 {
			return a.fail(ExitUsage, "未知阶段 "+st), nil
		}
		if stageIndex(st) > stageIndex(ch.Stage) {
			return a.fail(ExitGate, "不能提交还没走到的阶段"), nil
		}
		if err := validateArtifact(st, ch.Policy.ArtifactLevel, m); err != nil {
			return a.fail(ExitGate, err.Error()), nil
		}
		revised := st != ch.Stage || ch.artifact(st) != nil
		ch.addArtifact(st, canon)
		if revised {
			ch.invalidateEvidence()
			ch.Stage = st
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "revise", Message: "修正「" + st + "」，下游结论和证据失效"})
		} else {
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "submit", Message: st})
		}
		return ExitOK, a.payload(root, "change submit", ch, proj, fp)
	})
}

func (a *app) changeAdvance(args []string) int {
	id := flagID(a.err, args)
	if id.err != nil {
		return ExitUsage
	}
	root, _ := a.projectRoot()
	return a.withChange(id.id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Stage == StageRetro {
			if _, err := promoteDue(root, &proj, ch); err != nil {
				return a.fail(ExitGate, err.Error()), nil
			}
		}
		libs, lerr := loadLibrary(root)
		if lerr != nil {
			return a.fail(ExitInternal, lerr.Error()), nil
		}
		next, bad := ch.gate(proj, fp, libs)
		if len(bad) > 0 {
			return a.fail(ExitGate, strings.Join(bad, "；")), nil
		}
		if next == StageRelease {
			if wt, oid, ok := findSharedHold(root); ok && oid != ch.ID {
				return a.fail(ExitGate, fmt.Sprintf("共享 staging/production 正被 %s 的变更 %s 占用。其他流程可以继续开发和验收", wt, oid)), nil
			}
			ch.ReleaseHold = &ReleaseHold{Active: true, At: time.Now().UTC()}
		}
		if next == StageRetro && ch.Stage == StageRelease && ch.ReleaseHold != nil {
			ch.ReleaseHold.Active = false
		}
		if next == StatusClosed {
			ch.Status = StatusClosed
			if ch.ReleaseHold != nil {
				ch.ReleaseHold.Active = false
			}
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "close", Message: "复盘完成，正常关闭"})
		} else {
			prev := ch.Stage
			ch.Stage = next
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "advance", Message: prev + " -> " + next})
		}
		return ExitOK, a.payload(root, "change advance", ch, proj, fp)
	})
}

func (a *app) changeRollback(args []string) int {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	to := fs.String("to", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *to == "" || strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "回退需要 --to 和 --reason")
	}
	if stageIndex(*to) < 0 {
		return a.fail(ExitUsage, "未知阶段")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Status != StatusActive {
			return a.fail(ExitGate, "只有进行中的变更可以回退"), nil
		}
		if stageIndex(*to) >= stageIndex(ch.Stage) {
			return a.fail(ExitGate, "只能退回更早的阶段"), nil
		}
		ch.Stage = *to
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "rollback", Message: *reason + " -> " + *to})
		return ExitOK, a.payload(root, "change rollback", ch, proj, fp)
	})
}

func (a *app) changeBlock(args []string) int {
	fs := flag.NewFlagSet("block", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "阻塞需要 --reason")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Status != StatusActive {
			return a.fail(ExitGate, "当前状态不能阻塞"), nil
		}
		ch.Status = StatusBlocked
		ch.Block = &BlockInfo{Reason: *reason, At: time.Now().UTC()}
		ch.History = append(ch.History, Event{At: ch.Block.At, Kind: "block", Message: *reason})
		return ExitOK, a.payload(root, "change block", ch, proj, fp)
	})
}

func (a *app) changeResume(args []string) int {
	id := flagID(a.err, args)
	if id.err != nil {
		return ExitUsage
	}
	root, _ := a.projectRoot()
	return a.withChange(id.id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Status != StatusBlocked {
			return a.fail(ExitGate, "变更没有处于阻塞"), nil
		}
		ch.Status = StatusActive
		ch.Block = nil
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "resume", Message: "恢复"})
		return ExitOK, a.payload(root, "change resume", ch, proj, fp)
	})
}

func (a *app) changeCancel(args []string) int {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "取消需要 --reason")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Status == StatusClosed || ch.Status == StatusCancel {
			return a.fail(ExitGate, "变更已经结束"), nil
		}
		ch.Status = StatusCancel
		msg := *reason
		if ch.ReleaseHold != nil && ch.ReleaseHold.Active {
			msg += "。共享环境占用还在，没有自动撤回部署。人处理完后执行 change release clear"
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "cancel", Message: msg})
		return ExitOK, a.payload(root, "change cancel", ch, proj, fp)
	})
}

func (a *app) changeAccept(args []string) int {
	fs := flag.NewFlagSet("accept", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	what := fs.String("what", "", "delivery 或 release")
	source := fs.String("source", "", "human、other-session 或 external")
	reason := fs.String("reason", "", "")
	scope := fs.String("scope", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *what != "delivery" && *what != "release" {
		return a.fail(ExitUsage, "--what 只能是 delivery 或 release")
	}
	switch *source {
	case "human", "other-session", "external":
	default:
		return a.fail(ExitUsage, "接受来源只能是 human、other-session 或 external。自验不能改名为独立验收。本地 CLI 不认证人类身份。")
	}
	if strings.TrimSpace(*reason) == "" || strings.TrimSpace(*scope) == "" {
		return a.fail(ExitUsage, "接受需要 --reason 和 --scope")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		acc := Accept{What: *what, Source: *source, Reason: *reason, Scope: *scope, At: time.Now().UTC()}
		if *what == "delivery" {
			if fp.Kind != "ok" {
				return a.fail(ExitGate, fp.Note), nil
			}
			acc.BindKind = BindWorktree
			acc.Fingerprint = fp.Hash
		} else {
			if !ch.mergedNow() {
				return a.fail(ExitGate, "发布声明要绑到已记录且未撤回的集成 SHA"), nil
			}
			acc.BindKind = BindIntegration
			acc.Fingerprint = ch.Merge.SHA
		}
		ch.Accepts = append(ch.Accepts, acc)
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "accept", Message: *what + " 来源 " + *source})
		return ExitOK, a.payload(root, "change accept", ch, proj, fp)
	})
}

func (a *app) changeVersion(args []string) int {
	fs := flag.NewFlagSet("declare-version", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	ver := fs.String("version", "", "")
	source := fs.String("source", "declaration", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *ver == "" {
		return a.fail(ExitUsage, "需要 --version")
	}
	if *source != "declaration" {
		return a.fail(ExitUsage, "手填版本只能是 declaration。工具读回只能来自 release_readback 检查。")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		ch.DeclaredVersion = &VersionFact{Value: *ver, Source: "declaration", At: time.Now().UTC()}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "declare-version", Message: *ver})
		return ExitOK, a.payload(root, "change declare-version", ch, proj, fp)
	})
}

func (a *app) changePolicy(args []string) int {
	if len(args) == 0 || args[0] != "set" {
		return a.fail(ExitUsage, "用法：lauren change policy set --reason ...")
	}
	fs := flag.NewFlagSet("policy set", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	env := fs.String("environment", "", "")
	release := fs.String("release-stage", "", "")
	level := fs.String("artifact-level", "", "")
	branch := fs.String("integration-branch", "", "")
	intent := fs.String("integration-intent", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "修订 policy 需要 --reason")
	}
	if *env == "" && *release == "" && *level == "" && *branch == "" && *intent == "" {
		return a.fail(ExitUsage, "没有要改的 policy 字段")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		next := overlayPolicy(ch.Policy, *env, *release, *level, *branch, *intent)
		if err := validatePolicy(proj, next); err != nil {
			return a.fail(ExitUsage, err.Error()), nil
		}
		targetChanged := ch.Policy.IntegrationIntent != next.IntegrationIntent || ch.Policy.IntegrationBranch != next.IntegrationBranch
		if targetChanged && ch.Merge != nil && !ch.Merge.Withdrawn {
			return a.fail(ExitGate, fmt.Sprintf("已合入 %s %s，尚未撤回。请先 change integration withdraw，不能只改记录", ch.Merge.Branch, ch.Merge.SHA)), nil
		}
		if ch.Policy.Environment != next.Environment || ch.Policy.ReleaseStage != next.ReleaseStage {
			ch.voidConclusions()
		}
		ch.Policy = next
		if gap := ch.earliestArtifactGap(); gap != "" && stageIndex(gap) < stageIndex(ch.Stage) {
			ch.Stage = gap
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "policy", Message: "artifact_level 变了，已提交的「" + gap + "」不符合新规则"})
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "policy", Message: *reason})
		return ExitOK, a.payload(root, "change policy set", ch, proj, fp)
	})
}

func (a *app) changeIntegration(args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, "用法：lauren change integration record|withdraw")
	}
	switch args[0] {
	case "record":
		return a.integrationRecord(args[1:])
	case "withdraw":
		return a.integrationWithdraw(args[1:])
	default:
		return a.fail(ExitUsage, "未知 integration 子命令")
	}
}

func (a *app) integrationRecord(args []string) int {
	fs := flag.NewFlagSet("integration record", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	sha := fs.String("sha", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if !isSHA(*sha) || strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "需要 --sha 和 --reason。不会自动 merge 或 push")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Policy.IntegrationIntent != IntentBranch {
			return a.fail(ExitGate, "本次流程暂不纳入当前迭代，不能记录合入共享集成分支"), nil
		}
		if ch.Merge != nil && !ch.Merge.Withdrawn {
			return a.fail(ExitGate, "已有未撤回的集成记录 "+ch.Merge.SHA), nil
		}
		ch.Merge = &MergeRecord{Branch: ch.Policy.IntegrationBranch, SHA: *sha, At: time.Now().UTC(), Reason: *reason}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "integrate", Message: ch.Policy.IntegrationBranch + " " + *sha})
		return ExitOK, a.payload(root, "change integration record", ch, proj, fp)
	})
}

func (a *app) integrationWithdraw(args []string) int {
	fs := flag.NewFlagSet("integration withdraw", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "撤回需要 --reason。不会自动改 git 历史")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Merge == nil || ch.Merge.Withdrawn {
			return a.fail(ExitGate, "没有可撤回的集成记录"), nil
		}
		sha := ch.Merge.SHA
		ch.Merge.Withdrawn = true
		for i := range ch.Accepts {
			if ch.Accepts[i].BindKind == BindIntegration && ch.Accepts[i].Fingerprint == sha {
				ch.Accepts[i].Voided = true
			}
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "withdraw", Message: *reason + " " + sha})
		return ExitOK, a.payload(root, "change integration withdraw", ch, proj, fp)
	})
}

func (a *app) changeRelease(args []string) int {
	if len(args) == 0 || args[0] != "clear" {
		return a.fail(ExitUsage, "用法：lauren change release clear --reason ...")
	}
	fs := flag.NewFlagSet("release clear", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	if strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "释放共享环境占用需要 --reason")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.ReleaseHold == nil || !ch.ReleaseHold.Active {
			return a.fail(ExitGate, "没有占用共享环境"), nil
		}
		ch.ReleaseHold.Active = false
		ch.ReleaseHold.Note = *reason
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "release-clear", Message: *reason})
		return ExitOK, a.payload(root, "change release clear", ch, proj, fp)
	})
}

func overlayPolicy(base Policy, env, release, level, branch, intent string) Policy {
	if env != "" {
		base.Environment = env
	}
	if release != "" {
		base.ReleaseStage = release
	}
	if level != "" {
		base.ArtifactLevel = level
	}
	if intent != "" {
		base.IntegrationIntent = intent
	}
	if branch != "" {
		base.IntegrationBranch = branch
		if base.IntegrationIntent == "" {
			base.IntegrationIntent = IntentBranch
		}
	}
	if base.IntegrationIntent == IntentNotCurrent {
		base.IntegrationBranch = ""
	}
	return base
}

func isSHA(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func (a *app) changeProblem(args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, "用法：lauren change problem add|dispose|verify")
	}
	switch args[0] {
	case "add":
		return a.problemAdd(args[1:])
	case "dispose":
		return a.problemDispose(args[1:])
	case "verify":
		return a.problemVerify(args[1:])
	default:
		return a.fail(ExitUsage, "未知 problem 子命令")
	}
}

func (a *app) problemAdd(args []string) int {
	fs := flag.NewFlagSet("problem add", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	title := fs.String("title", "", "")
	summary := fs.String("summary", "", "")
	layer := fs.String("layer", "", "")
	symptom := fs.String("symptom", "", "")
	repro := fs.String("repro", "", "")
	expected := fs.String("expected", "", "")
	actual := fs.String("actual", "", "")
	detail := fs.String("detail", "", "")
	note := fs.String("note", "", "")
	target := fs.String("target-check", "", "")
	disp := fs.String("disposition", "", "")
	reason := fs.String("reason", "", "")
	defense := fs.String("defense", "", "")
	scope := fs.String("scope", "project", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *title == "" {
		*title = *summary
	}
	if *title == "" {
		return a.fail(ExitUsage, "问题需要 --title")
	}
	switch *disp {
	case "", "implemented", "later", "rejected":
	default:
		return a.fail(ExitUsage, "--disposition 只能是 implemented、later、rejected，或先留空")
	}
	if *disp != "" && *reason == "" {
		return a.fail(ExitUsage, "写了处置去向就需要 --reason")
	}
	if *scope != "project" && *scope != "change" {
		return a.fail(ExitUsage, "适用范围只能是 project 或 change，不会自动变成全局规则")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Stage != StageRetro {
			return a.fail(ExitGate, "记录问题是复盘与改进的内容"), nil
		}
		pb, promoted, err := recordProblem(root, &proj, ch, problemInput{
			Title: *title, Layer: *layer, Symptom: *symptom, Repro: *repro, Expected: *expected, Actual: *actual,
			Detail: *detail, Defense: *defense, TargetCheck: *target, Scope: *scope, Note: *note,
			Disposition: *disp, Reason: *reason,
		}, true)
		if err != nil {
			return a.fail(ExitUsage, err.Error()), nil
		}
		if promoted {
			ch.invalidateEvidence()
			ch.CatalogHash = catalogHash(proj)
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "problem", Message: pb.ID + " count=" + fmt.Sprint(pb.Count)})
		return ExitOK, a.payload(root, "change problem add", ch, proj, fp)
	})
}

func (a *app) problemDispose(args []string) int {
	fs := flag.NewFlagSet("problem dispose", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	pid := fs.String("problem", "", "")
	disp := fs.String("disposition", "", "")
	reason := fs.String("reason", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	switch *disp {
	case "implemented", "later", "rejected":
	default:
		return a.fail(ExitUsage, "--disposition 只能是 implemented、later 或 rejected")
	}
	if *pid == "" || strings.TrimSpace(*reason) == "" {
		return a.fail(ExitUsage, "需要 --problem、--disposition 和 --reason")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		if ch.Stage != StageRetro {
			return a.fail(ExitGate, "处置问题是复盘与改进的内容"), nil
		}
		libs, err := loadLibrary(root)
		if err != nil {
			return a.fail(ExitInternal, err.Error()), nil
		}
		for _, pb := range libs {
			if pb.ID != *pid {
				continue
			}
			pb.Disposition = *disp
			pb.Reason = *reason
			if *disp == "rejected" {
				pb.Status = "rejected"
			}
			if err := saveLibraryProblem(root, pb); err != nil {
				return a.fail(ExitInternal, err.Error()), nil
			}
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "dispose", Message: *pid + " " + *disp})
			return ExitOK, a.payload(root, "change problem dispose", ch, proj, fp)
		}
		return a.fail(ExitUsage, "找不到问题"), nil
	})
}

func (a *app) problemVerify(args []string) int {
	fs := flag.NewFlagSet("problem verify", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	pid := fs.String("problem", "", "")
	check := fs.String("check", "", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *pid == "" || *check == "" {
		return a.fail(ExitUsage, "需要 --problem 和 --check")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		libs, err := loadLibrary(root)
		if err != nil {
			return a.fail(ExitInternal, err.Error()), nil
		}
		var pb *LibraryProblem
		for _, item := range libs {
			if item.ID == *pid {
				pb = item
			}
		}
		if pb == nil {
			return a.fail(ExitUsage, "找不到问题"), nil
		}
		var failRun, passRun *CheckRun
		for i := range ch.Runs {
			r := &ch.Runs[i]
			if r.Name != *check {
				continue
			}
			if r.Status == "fail" {
				failRun = r
				passRun = nil
			}
			if r.Status == "pass" && failRun != nil && r.Ended.After(failRun.Ended) && ch.effective(*r, fp) == "pass" {
				passRun = r
			}
		}
		if failRun == nil || passRun == nil {
			return a.fail(ExitGate, "没有同一检查的旧失败和新通过记录，不能标成已验证修复"), nil
		}
		pb.FailRun = failRun.ID
		pb.PassRun = passRun.ID
		pb.VerifiedFixed = true
		if err := saveLibraryProblem(root, pb); err != nil {
			return a.fail(ExitInternal, err.Error()), nil
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "verify", Message: pb.ID + " " + *check})
		return ExitOK, a.payload(root, "change problem verify", ch, proj, fp)
	})
}

func (a *app) cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	name := fs.String("name", "", "")
	envFlag := fs.String("environment", "", "这次检查的实际环境")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	proj, err := loadProject(root)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	cid, err := resolveChangeID(root, *id, true)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	ch0, err := readChange(root, cid)
	if err != nil || ch0 == nil {
		return a.fail(ExitUsage, "找不到变更")
	}
	if ch0.Status == StatusBlocked {
		return a.fail(ExitBlocked, "变更被阻塞，未运行检查")
	}
	env := *envFlag
	if env == "" {
		env = ch0.Policy.Environment
	}
	if !containsStr(proj.Environments, env) {
		return a.fail(ExitUsage, "环境 "+env+" 不在项目 environments 里")
	}
	var specs []CheckSpec
	for _, ch := range proj.Checks {
		if *name == "" || ch.Name == *name {
			specs = append(specs, ch)
		}
	}
	if *name != "" && len(specs) == 0 {
		return a.fail(ExitUsage, "配置里没有检查 "+*name)
	}
	if len(proj.Checks) == 0 {
		return a.fail(ExitUsage, "项目配置没有检查命令")
	}
	fp := fingerprint(root, proj)
	var runs []CheckRun
	for _, spec := range specs {
		if spec.Gate == GateRelease || spec.Layer == "release_readback" {
			switch {
			case ch0.Policy.IntegrationIntent != IntentBranch:
				runs = append(runs, blockedRun(spec, env, "暂不纳入当前迭代，不能占用共享预发或生产的发布读回"))
			case ch0.Policy.IntegrationBranch != proj.Defaults.IntegrationBranch:
				runs = append(runs, blockedRun(spec, env, "集成分支还不是项目当前集成分支，不能做共享环境发布读回"))
			case !ch0.mergedNow():
				runs = append(runs, blockedRun(spec, env, "尚未记录合入集成分支，不能把工作区里的版本当成已部署证据"))
			case !envAccepted(spec, proj, env):
				runs = append(runs, blockedRun(spec, env, "环境 "+env+" 不能充当 release_readback。开发或 CI 里打印的版本不是已部署证据"))
			default:
				if wt, oid, ok := findSharedHold(root); ok && oid != ch0.ID {
					runs = append(runs, blockedRun(spec, env, fmt.Sprintf("共享环境正被 %s 的变更 %s 占用", wt, oid)))
					continue
				}
				_ = WithChange(root, cid, func(ch *Change) error {
					if ch.ReleaseHold == nil || !ch.ReleaseHold.Active {
						ch.ReleaseHold = &ReleaseHold{Active: true, At: time.Now().UTC(), Note: "发布读回占用 staging/production"}
					}
					return nil
				})
				runs = append(runs, runCheck(root, cid, spec, fp, ch0.EvidenceGeneration, env, ch0.Policy.IntegrationBranch, ch0.Merge.SHA))
			}
			continue
		}
		if !envAccepted(spec, proj, env) {
			runs = append(runs, blockedRun(spec, env, "环境 "+env+" 不在该检查可接受的证据环境里"))
			continue
		}
		runs = append(runs, runCheck(root, cid, spec, fp, ch0.EvidenceGeneration, env, "", ""))
	}
	fpAfter := fingerprint(root, proj)
	code := ExitOK
	err = WithChange(root, cid, func(ch *Change) error {
		ch.syncCatalog(proj)
		if ch.EvidenceGeneration != ch0.EvidenceGeneration {
			return fmt.Errorf("检查期间证据目录变了，请重试")
		}
		for i := range runs {
			ch.Runs = append(ch.Runs, runs[i])
			if runs[i].Status != "pass" {
				code = ExitCheck
			}
		}
		if fpAfter.Kind != "ok" || fpAfter.Hash != fp.Hash {
			for _, r := range runs {
				if r.BindKind == BindWorktree {
					code = ExitCheck
				}
			}
		}
		if err := noteCheckFailures(root, &proj, ch, runs); err != nil {
			return err
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "check", Message: fmt.Sprintf("%s 运行 %d 项", env, len(runs))})
		view := a.payload(root, "check", ch, proj, fpAfter)
		view.OK = code == ExitOK
		if a.json {
			_ = a.emit(code, view)
		} else {
			writeHuman(a.out, view)
		}
		return nil
	})
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	return code
}

func blockedRun(spec CheckSpec, env, note string) CheckRun {
	now := time.Now().UTC()
	return CheckRun{
		ID: newID("r-"), Name: spec.Name, Layer: spec.Layer, Gate: spec.Gate, Argv: spec.Argv,
		Environment: env, Status: "blocked", Started: now, Ended: now, Note: note,
		BindKind: BindWorktree, CandidateKind: "blocked",
	}
}

func noteCheckFailures(root string, proj *Project, ch *Change, runs []CheckRun) error {
	for _, r := range runs {
		if r.Status != "fail" {
			continue
		}
		if r.Gate == GateAcceptance && (ch.Stage == StageAccept || ch.Stage == StageRelease) {
			ch.Stage = StageBuild
			if _, _, err := recordProblem(root, proj, ch, problemInput{
				Title: r.Name + " 验收失败", Layer: r.Layer, Symptom: "验收检查失败", Actual: r.Note, Scope: "change",
			}, false); err != nil {
				return err
			}
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "loop", Message: "验收失败，回到实现与自验"})
		}
		if (r.Gate == GateRelease || r.Layer == "release_readback") && ch.Stage == StageRelease {
			ch.Stage = StageBuild
			if _, _, err := recordProblem(root, proj, ch, problemInput{
				Title: r.Name + " 发布读回失败", Layer: r.Layer, Symptom: "发布读回失败", Actual: "没有自动撤回共享环境", Scope: "change",
			}, false); err != nil {
				return err
			}
			if ch.ReleaseHold != nil {
				ch.ReleaseHold.Note = "发布读回失败，占用保留到人处理"
			}
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "loop", Message: "发布读回失败，回到实现与自验。没有自动撤回共享环境"})
		}
	}
	return nil
}

func (a *app) cmdStatus(args []string) int  { return a.show("status", flagID(a.err, args), false) }
func (a *app) cmdHistory(args []string) int { return a.show("history", flagID(a.err, args), true) }
func (a *app) cmdNext(args []string) int    { return a.show("next", flagID(a.err, args), false) }

func (a *app) show(cmd string, id idFlag, hist bool) int {
	if id.err != nil {
		return ExitUsage
	}
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	proj, err := loadProject(root)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	cid, err := resolveChangeID(root, id.id, false)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if cid == "" {
		p := statusPayload{OK: true, Command: cmd, Next: []string{"创建变更"}}
		if a.json {
			return a.emit(ExitOK, p)
		}
		writeHuman(a.out, p)
		return ExitOK
	}
	var payload statusPayload
	err = WithChange(root, cid, func(ch *Change) error {
		ch.syncCatalog(proj)
		fp := fingerprint(root, proj)
		payload = a.payload(root, cmd, ch, proj, fp)
		if hist {
			payload.History = ch.History
		}
		return nil
	})
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if a.json {
		return a.emit(ExitOK, payload)
	}
	writeHuman(a.out, payload)
	return ExitOK
}

type idFlag struct {
	id  string
	err error
}

func flagID(errw io.Writer, args []string) idFlag {
	fs := flag.NewFlagSet("id", flag.ContinueOnError)
	fs.SetOutput(errw)
	id := fs.String("id", "", "")
	if err := fs.Parse(args); err != nil {
		return idFlag{err: err}
	}
	return idFlag{id: *id}
}

type checkView struct {
	Name            string `json:"name"`
	Layer           string `json:"layer"`
	Gate            string `json:"gate"`
	Environment     string `json:"environment,omitempty"`
	Status          string `json:"status"`
	EffectiveStatus string `json:"effective_status"`
	ExitCode        *int   `json:"exit_code"`
	Cwd             string `json:"cwd,omitempty"`
	LogPath         string `json:"log_path,omitempty"`
	LogBytes        int    `json:"log_bytes"`
	Preview         string `json:"preview,omitempty"`
	Note            string `json:"note,omitempty"`
}

type problemView struct {
	ID            string `json:"id"`
	Summary       string `json:"summary"`
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason"`
	Scope         string `json:"scope"`
	VerifiedFixed bool   `json:"verified_fixed"`
	Count         int    `json:"count"`
	Status        string `json:"status"`
	PromotedCheck string `json:"promoted_check,omitempty"`
}

type facts struct {
	ResultsRecorded  bool `json:"results_recorded"`
	DeliveryAccepted bool `json:"delivery_accepted"`
	ReleaseDeclared  bool `json:"release_declared"`
	ReleaseVerified  bool `json:"release_verified"`
	ProductVerified  bool `json:"product_verified"`
}

type statusPayload struct {
	OK              bool          `json:"ok"`
	Command         string        `json:"command"`
	ID              string        `json:"id,omitempty"`
	Goal            string        `json:"goal,omitempty"`
	Stage           string        `json:"stage,omitempty"`
	Status          string        `json:"status,omitempty"`
	Policy          *Policy       `json:"policy,omitempty"`
	Block           string        `json:"block,omitempty"`
	Candidate       Fingerprint   `json:"candidate,omitempty"`
	Merge           *MergeRecord  `json:"merge,omitempty"`
	ReleaseHold     bool          `json:"release_hold,omitempty"`
	DeclaredVersion *VersionFact  `json:"declared_version,omitempty"`
	ToolVersion     *VersionFact  `json:"tool_version,omitempty"`
	Facts           *facts        `json:"facts,omitempty"`
	Checks          []checkView   `json:"checks,omitempty"`
	Problems        []problemView `json:"problems,omitempty"`
	Next            []string      `json:"next,omitempty"`
	History         []Event       `json:"history,omitempty"`
	Trust           string        `json:"trust,omitempty"`
}

func (a *app) payload(root, cmd string, ch *Change, proj Project, fp Fingerprint) statusPayload {
	p := statusPayload{
		OK: true, Command: cmd, ID: ch.ID, Goal: ch.Goal, Stage: ch.Stage, Status: ch.Status,
		Policy: &ch.Policy, Candidate: fp, Merge: ch.Merge, DeclaredVersion: ch.DeclaredVersion,
		Next:  nextActions(ch, proj, fp, libsFor(root, ch)),
		Trust: "本地状态文件可以被改写。这个 CLI 不认证人类，也不能防止恶意篡改。合并是否可信看仓库 CI 重跑；交付是否接受看来源被明确记下的接受记录。不会自动 merge、push、部署或通知。",
	}
	if ch.Block != nil && ch.Status == StatusBlocked {
		p.Block = ch.Block.Reason
	}
	if ch.ReleaseHold != nil && ch.ReleaseHold.Active {
		p.ReleaseHold = true
	}
	seen := map[string]bool{}
	for i := len(ch.Runs) - 1; i >= 0; i-- {
		r := ch.Runs[i]
		if seen[r.Name+"@"+r.Environment] {
			continue
		}
		seen[r.Name+"@"+r.Environment] = true
		p.Checks = append([]checkView{{
			Name: r.Name, Layer: r.Layer, Gate: r.Gate, Environment: r.Environment,
			Status: r.Status, EffectiveStatus: ch.effective(r, fp), ExitCode: r.ExitCode,
			Cwd: r.Cwd, LogPath: r.LogPath, LogBytes: r.LogBytes, Preview: r.Preview, Note: r.Note,
		}}, p.Checks...)
	}
	product := len(proj.Requirements) > 0
	for _, req := range proj.Requirements {
		if !req.Required {
			continue
		}
		ok, _ := ch.requirementMet(req, proj, fp)
		if !ok {
			product = false
		}
	}
	if len(proj.Requirements) == 0 {
		product = false
	}
	releaseVerified := false
	var tool *VersionFact
	if hasRequiredCheck(proj, GateRelease) {
		releaseVerified = len(ch.requiredFresh(proj, GateRelease, fp)) == 0
		for _, spec := range proj.Checks {
			if spec.Layer != "release_readback" {
				continue
			}
			run, _ := ch.satisfying(spec, proj, fp)
			if run != nil {
				if v := toolVersionFrom(*run); v != "" {
					tool = &VersionFact{Value: v, Source: "tool", At: run.Ended}
				}
			}
		}
	}
	p.ToolVersion = tool
	p.Facts = &facts{
		ResultsRecorded:  len(ch.Runs) > 0,
		DeliveryAccepted: fp.Kind == "ok" && ch.liveAccept("delivery", BindWorktree, fp.Hash) != nil,
		ReleaseDeclared:  ch.mergedNow() && ch.liveAccept("release", BindIntegration, ch.Merge.SHA) != nil,
		ReleaseVerified:  releaseVerified,
		ProductVerified:  product,
	}
	for _, pb := range libsFor(root, ch) {
		p.Problems = append(p.Problems, problemView{
			ID: pb.ID, Summary: pb.Title, Disposition: pb.Disposition, Reason: pb.Reason,
			Scope: pb.Scope, VerifiedFixed: ch.libraryVerified(pb, fp),
			Count: pb.Count, Status: pb.Status, PromotedCheck: pb.PromotedCheck,
		})
	}
	return p
}

func writeHuman(w io.Writer, p statusPayload) {
	if p.ID == "" {
		fmt.Fprintln(w, "这个工作区没有进行中的变更")
		fmt.Fprintln(w, "下一合法动作:")
		for _, n := range p.Next {
			fmt.Fprintln(w, "  - "+n)
		}
		return
	}
	fmt.Fprintf(w, "变更: %s\n目标: %s\n阶段: %s\n状态: %s\n", p.ID, p.Goal, p.Stage, p.Status)
	if p.Policy != nil {
		fmt.Fprintf(w, "环境: %s\n发布阶段: %s\n产物级别: %s\n集成意图: %s\n集成分支: %s\n",
			p.Policy.Environment, p.Policy.ReleaseStage, p.Policy.ArtifactLevel, p.Policy.IntegrationIntent, p.Policy.IntegrationBranch)
	}
	if p.Block != "" {
		fmt.Fprintf(w, "阻塞: %s\n", p.Block)
	}
	fmt.Fprintf(w, "候选: %s %s\n", p.Candidate.Kind, p.Candidate.Hash)
	if p.Merge != nil {
		fmt.Fprintf(w, "集成记录: %s %s 已撤回=%t\n", p.Merge.Branch, p.Merge.SHA, p.Merge.Withdrawn)
	}
	if p.ReleaseHold {
		fmt.Fprintln(w, "共享环境占用: 是")
	}
	if p.Facts != nil {
		fmt.Fprintf(w, "结果已记录: %t\n交付已接受: %t\n发布已声明: %t\n发布已由工具读回: %t\n产品要求已满足: %t\n",
			p.Facts.ResultsRecorded, p.Facts.DeliveryAccepted, p.Facts.ReleaseDeclared, p.Facts.ReleaseVerified, p.Facts.ProductVerified)
	}
	if p.DeclaredVersion != nil {
		fmt.Fprintf(w, "声明版本: %s（来源 %s，不是机器读回）\n", p.DeclaredVersion.Value, p.DeclaredVersion.Source)
	}
	if p.ToolVersion != nil {
		fmt.Fprintf(w, "工具读回版本: %s\n", p.ToolVersion.Value)
	}
	if len(p.Checks) > 0 {
		fmt.Fprintln(w, "检查:")
		for _, c := range p.Checks {
			exit := "未退出"
			if c.ExitCode != nil {
				exit = fmt.Sprintf("exit %d", *c.ExitCode)
			}
			fmt.Fprintf(w, "  %s %s %s env=%s 记录=%s 有效=%s %s\n", c.Name, c.Gate, c.Layer, c.Environment, c.Status, c.EffectiveStatus, exit)
			if c.Note != "" {
				fmt.Fprintf(w, "    %s\n", c.Note)
			}
		}
	}
	if len(p.Problems) > 0 {
		fmt.Fprintln(w, "问题:")
		for _, pb := range p.Problems {
			fmt.Fprintf(w, "  %s %s 次数=%d 状态=%s 已验证修复=%t 范围=%s\n", pb.ID, pb.Disposition, pb.Count, pb.Status, pb.VerifiedFixed, pb.Scope)
		}
	}
	fmt.Fprintln(w, "下一合法动作:")
	if len(p.Next) == 0 {
		fmt.Fprintln(w, "  - （无）")
	}
	for _, n := range p.Next {
		fmt.Fprintln(w, "  - "+n)
	}
	if len(p.History) > 0 {
		fmt.Fprintln(w, "历史:")
		for _, e := range p.History {
			fmt.Fprintf(w, "  %s %s\n", e.Kind, e.Message)
		}
	}
}

func safePath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("产物路径必须是项目内的相对路径")
	}
	clean := filepath.Clean(rel)
	full := filepath.Join(root, clean)
	relBack, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relBack, "..") {
		return "", fmt.Errorf("产物路径越界")
	}
	return full, nil
}

func (a *app) cmdSkills(args []string) int {
	if len(args) == 0 || args[0] != "check" {
		return a.fail(ExitUsage, "用法：lauren skills check")
	}
	root := a.root
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			root = filepath.Dir(filepath.Dir(exe))
		}
	}
	wd, _ := os.Getwd()
	candidates := []string{root, wd}
	var base string
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "skills", "lauren-workflow", "SKILL.md")); err == nil {
			base = c
			break
		}
	}
	if base == "" {
		return a.fail(ExitUsage, "找不到 skills/lauren-workflow/SKILL.md")
	}
	skill := filepath.Join(base, "skills", "lauren-workflow", "SKILL.md")
	b, err := os.ReadFile(skill)
	if err != nil {
		return a.fail(ExitInternal, err.Error())
	}
	text := string(b)
	if !strings.HasPrefix(text, "---\n") {
		return a.fail(ExitGate, "SKILL.md 缺少 frontmatter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return a.fail(ExitGate, "SKILL.md frontmatter 未闭合")
	}
	fm := rest[:end]
	if !strings.Contains(fm, "name: lauren-workflow") || !strings.Contains(fm, "description:") {
		return a.fail(ExitGate, "SKILL.md 需要 name 和 description")
	}
	for _, rel := range []string{".agents/skills/lauren-workflow", ".claude/skills/lauren-workflow"} {
		p := filepath.Join(base, rel)
		if _, err := os.Stat(filepath.Join(p, "SKILL.md")); err != nil {
			return a.fail(ExitGate, "官方入口不存在: "+rel)
		}
	}
	man, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "plugin.json"))
	if err != nil {
		return a.fail(ExitGate, "缺少 .claude-plugin/plugin.json")
	}
	var raw map[string]any
	if err := json.Unmarshal(man, &raw); err != nil {
		return a.fail(ExitGate, "plugin.json 无法解析")
	}
	if raw["name"] != "lauren" {
		return a.fail(ExitGate, "plugin.json 的 name 不是 lauren")
	}
	if _, ok := raw["hooks"]; ok {
		return a.fail(ExitGate, "v0.1 清单不包含 hooks")
	}
	if _, ok := raw["mcp"]; ok {
		return a.fail(ExitGate, "v0.1 清单不包含 mcp")
	}
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "skills check", "skill": skill})
	}
	fmt.Fprintln(a.out, "Skill 入口可用。门禁仍只在 lauren CLI 里。")
	return ExitOK
}
