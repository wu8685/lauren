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
		return a.root, nil
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
	env := fs.String("environment", "local", "声明环境")
	release := fs.Bool("release", false, "这次项目默认要发布")
	profile := fs.String("profile", "full", "full 或 small")
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
	path := filepath.Join(root, "lauren.project.json")
	if _, err := os.Stat(path); err == nil {
		return a.fail(ExitUsage, "lauren.project.json 已存在")
	}
	if *profile != "full" && *profile != "small" {
		return a.fail(ExitUsage, "profile 只能是 full 或 small")
	}
	p := Project{Schema: 1, Environment: *env, Release: *release, Profile: *profile, Checks: []CheckSpec{}, Requirements: []Requirement{}}
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return a.fail(ExitInternal, err.Error())
	}
	_ = os.MkdirAll(runtimeDir(root), 0o755)
	_ = ensureGitignore(root)
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "init", "config": path, "runtime": runtimeDir(root)})
	}
	fmt.Fprintf(a.out, "已写入 %s\n运行状态目录 %s\n项目命令写在配置里之后才会被执行。配置属于显式信任，不会从任务文本拼 shell。\n", path, runtimeDir(root))
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
		return a.fail(ExitUsage, "用法：lauren change create|submit|advance|rollback|block|resume|cancel|accept|declare-version|problem")
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
	case "problem":
		return a.changeProblem(args[1:])
	case "stage", "pass":
		return a.fail(ExitUsage, "没有直接设置阶段或填写 pass 的命令")
	default:
		return a.fail(ExitUsage, "未知 change 子命令 "+args[0])
	}
}

type changeFlags struct {
	id string
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
	var code int
	var payload any
	err = WithState(root, func(st *State) error {
		ch, err := pickChange(st, id, true)
		if err != nil {
			return err
		}
		fp := fingerprint(root, proj.Environment)
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

func pickChange(st *State, id string, need bool) (*Change, error) {
	if id != "" {
		for i := range st.Changes {
			if st.Changes[i].ID == id {
				return &st.Changes[i], nil
			}
		}
		return nil, fmt.Errorf("找不到变更 %s", id)
	}
	var opens []*Change
	for i := range st.Changes {
		s := st.Changes[i].Status
		if s == StatusActive || s == StatusBlocked {
			opens = append(opens, &st.Changes[i])
		}
	}
	if len(opens) == 1 {
		return opens[0], nil
	}
	if !need {
		return nil, nil
	}
	if len(opens) == 0 {
		return nil, fmt.Errorf("没有进行中的变更")
	}
	return nil, fmt.Errorf("有多个进行中的变更，请使用 --id")
}

func (a *app) changeCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(a.err)
	goal := fs.String("goal", "", "变更目标")
	rel := fs.String("release", "", "true、false，或留空沿用项目配置")
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
	release := proj.Release
	if *rel != "" {
		switch *rel {
		case "true":
			release = true
		case "false":
			release = false
		default:
			return a.fail(ExitUsage, "--release 只能是 true 或 false")
		}
	}
	var created Change
	err = WithState(root, func(st *State) error {
		for _, c := range st.Changes {
			if c.Status == StatusActive || c.Status == StatusBlocked {
				return fmt.Errorf("已有进行中的变更 %s。v0.1 同时只跟踪一个", c.ID)
			}
		}
		created = Change{
			ID: newID("c-"), Goal: *goal, Stage: StageDemand, Status: StatusActive,
			Release: release, Profile: proj.Profile, Environment: proj.Environment,
			CreatedAt: time.Now().UTC(),
		}
		created.History = append(created.History, Event{At: created.CreatedAt, Kind: "create", Message: *goal})
		st.Changes = append(st.Changes, created)
		return nil
	})
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if a.json {
		return a.emit(ExitOK, map[string]any{"ok": true, "command": "change create", "id": created.ID, "stage": created.Stage, "release": created.Release})
	}
	fmt.Fprintf(a.out, "变更 %s\n阶段: %s\n发布意图: %t\n", created.ID, created.Stage, created.Release)
	return ExitOK
}

func (a *app) changeSubmit(args []string) int {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "变更 ID")
	stage := fs.String("stage", "", "阶段名，默认当前阶段")
	art := fs.String("artifact", "", "JSON 产物路径")
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
		if err := validateArtifact(st, ch.Profile, m); err != nil {
			return a.fail(ExitGate, err.Error()), nil
		}
		revised := st != ch.Stage || ch.artifact(st) != nil
		ch.addArtifact(st, canon)
		if revised {
			ch.voidDownstream()
			ch.Stage = st
			ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "revise", Message: "修正「" + st + "」，下游结论失效"})
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
		next, bad := ch.gate(proj, fp)
		if len(bad) > 0 {
			return a.fail(ExitGate, strings.Join(bad, "；")), nil
		}
		if next == StatusClosed {
			ch.Status = StatusClosed
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
	to := fs.String("to", "", "退回的阶段")
	reason := fs.String("reason", "", "理由")
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
		ch.voidDownstream()
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
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "cancel", Message: *reason})
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
		ch.Accepts = append(ch.Accepts, Accept{
			What: *what, Source: *source, Reason: *reason, Scope: *scope,
			At: time.Now().UTC(), Generation: ch.Generation, Fingerprint: fp.Hash,
		})
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
		return a.fail(ExitUsage, "手填版本只能是 declaration。工具读回只能来自 release-readback 检查。")
	}
	root, _ := a.projectRoot()
	return a.withChange(*id, func(ch *Change, proj Project, fp Fingerprint) (int, any) {
		ch.DeclaredVersion = &VersionFact{Value: *ver, Source: "declaration", At: time.Now().UTC(), Generation: ch.Generation, Fingerprint: fp.Hash}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "declare-version", Message: *ver})
		return ExitOK, a.payload(root, "change declare-version", ch, proj, fp)
	})
}

func (a *app) changeProblem(args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, "用法：lauren change problem add|verify")
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
	summary := fs.String("summary", "", "")
	repro := fs.String("repro", "", "")
	expected := fs.String("expected", "", "")
	actual := fs.String("actual", "", "")
	disp := fs.String("disposition", "", "implemented、later 或 rejected")
	reason := fs.String("reason", "", "")
	defense := fs.String("defense", "", "")
	scope := fs.String("scope", "project", "")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *summary == "" {
		return a.fail(ExitUsage, "问题需要 --summary")
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
		pb := Problem{
			ID: newID("p-"), Summary: *summary, Repro: *repro, Expected: *expected, Actual: *actual,
			Disposition: *disp, Reason: *reason, Defense: *defense, Scope: *scope,
		}
		ch.Problems = append(ch.Problems, pb)
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "problem", Message: pb.ID + " " + *disp})
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
		for i := range ch.Problems {
			if ch.Problems[i].ID != *pid {
				continue
			}
			ch.Problems[i].Disposition = *disp
			ch.Problems[i].Reason = *reason
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
		var pb *Problem
		for i := range ch.Problems {
			if ch.Problems[i].ID == *pid {
				pb = &ch.Problems[i]
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
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "verify", Message: pb.ID + " " + *check})
		return ExitOK, a.payload(root, "change problem verify", ch, proj, fp)
	})
}

func (a *app) cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(a.err)
	id := fs.String("id", "", "")
	name := fs.String("name", "", "只跑一个检查")
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
	var gen int
	err = WithState(root, func(st *State) error {
		ch, err := pickChange(st, *id, true)
		if err != nil {
			return err
		}
		if ch.Status == StatusBlocked {
			return fmt.Errorf("变更被阻塞，未运行检查")
		}
		gen = ch.Generation
		return nil
	})
	if err != nil {
		code := ExitUsage
		if strings.Contains(err.Error(), "阻塞") {
			code = ExitBlocked
		}
		return a.fail(code, err.Error())
	}
	fp := fingerprint(root, proj.Environment)
	var runs []CheckRun
	for _, spec := range specs {
		runs = append(runs, runCheck(root, spec, fp, gen))
	}
	fpAfter := fingerprint(root, proj.Environment)
	var code = ExitOK
	err = WithState(root, func(st *State) error {
		ch, err := pickChange(st, *id, true)
		if err != nil {
			return err
		}
		if ch.Generation != gen {
			return fmt.Errorf("变更在检查期间被修改，请重试")
		}
		for i := range runs {
			if runs[i].Layer == "release-readback" && runs[i].Status == "pass" {
				if v := toolVersionFrom(runs[i], root); v != "" {
					ch.ToolVersion = &VersionFact{Value: v, Source: "tool", At: time.Now().UTC(), Generation: ch.Generation, Fingerprint: runs[i].Fingerprint}
				}
			}
			ch.Runs = append(ch.Runs, runs[i])
			if runs[i].Status != "pass" {
				code = ExitCheck
			}
		}
		if fpAfter.Kind != "ok" || fpAfter.Hash != fp.Hash {
			code = ExitCheck
		}
		ch.History = append(ch.History, Event{At: time.Now().UTC(), Kind: "check", Message: fmt.Sprintf("运行 %d 项", len(runs))})
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
		return a.fail(codeOr(code, ExitUsage), err.Error())
	}
	return code
}

func codeOr(code, fallback int) int {
	if code != ExitOK {
		return code
	}
	return fallback
}

func (a *app) cmdStatus(args []string) int {
	id := flagID(a.err, args)
	if id.err != nil {
		return ExitUsage
	}
	return a.show("status", id.id, false)
}

func (a *app) cmdHistory(args []string) int {
	id := flagID(a.err, args)
	if id.err != nil {
		return ExitUsage
	}
	return a.show("history", id.id, true)
}

func (a *app) cmdNext(args []string) int {
	id := flagID(a.err, args)
	if id.err != nil {
		return ExitUsage
	}
	return a.show("next", id.id, false)
}

func (a *app) show(cmd, id string, hist bool) int {
	root, err := a.projectRoot()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	proj, err := loadProject(root)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	var payload statusPayload
	err = WithState(root, func(st *State) error {
		ch, err := pickChange(st, id, false)
		if err != nil {
			return err
		}
		if ch == nil && id == "" && len(st.Changes) > 0 {
			ch = &st.Changes[len(st.Changes)-1]
		}
		if ch == nil {
			payload = statusPayload{OK: true, Command: cmd, Next: []string{"创建变更"}}
			return nil
		}
		fp := fingerprint(root, proj.Environment)
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
	When            string `json:"when"`
	Status          string `json:"status"`
	EffectiveStatus string `json:"effective_status"`
	ExitCode        *int   `json:"exit_code"`
	Cwd             string `json:"cwd"`
	LogPath         string `json:"log_path,omitempty"`
	LogBytes        int    `json:"log_bytes"`
	Preview         string `json:"preview,omitempty"`
}

type problemView struct {
	ID            string `json:"id"`
	Summary       string `json:"summary"`
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason"`
	Scope         string `json:"scope"`
	VerifiedFixed bool   `json:"verified_fixed"`
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
	ReleaseIntent   bool          `json:"release_intent,omitempty"`
	Block           string        `json:"block,omitempty"`
	Candidate       Fingerprint   `json:"candidate,omitempty"`
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
		ReleaseIntent: ch.Release, Candidate: fp, DeclaredVersion: currentVersion(ch.DeclaredVersion, ch.Generation, fp), ToolVersion: currentVersion(ch.ToolVersion, ch.Generation, fp),
		Next:  nextActions(ch, proj, fp),
		Trust: "本地状态文件可以被改写。这个 CLI 不认证人类，也不能防止恶意篡改。合并是否可信看仓库 CI 重跑；交付是否接受看来源被明确记下的接受记录。",
	}
	if ch.Block != nil && ch.Status == StatusBlocked {
		p.Block = ch.Block.Reason
	}
	seen := map[string]bool{}
	for i := len(ch.Runs) - 1; i >= 0; i-- {
		r := ch.Runs[i]
		if seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		p.Checks = append([]checkView{{
			Name: r.Name, Layer: r.Layer, When: r.When, Status: r.Status,
			EffectiveStatus: ch.effective(r, fp), ExitCode: r.ExitCode, Cwd: r.Cwd,
			LogPath: r.LogPath, LogBytes: r.LogBytes, Preview: r.Preview,
		}}, p.Checks...)
	}
	product := true
	if len(proj.Requirements) == 0 {
		product = false
	}
	for _, req := range proj.Requirements {
		if !req.Required {
			continue
		}
		ok, _ := ch.requirementMet(req, proj, fp)
		if !ok {
			product = false
		}
	}
	releaseVerified := false
	needTool := false
	for _, chs := range proj.Checks {
		if chs.When == "release" && chs.Required {
			needTool = true
		}
	}
	if needTool {
		releaseVerified = len(ch.requiredFresh(proj, "release", fp)) == 0 && ch.latestRun(firstName(proj, "release")) != nil
	}
	p.Facts = &facts{
		ResultsRecorded:  len(ch.Runs) > 0,
		DeliveryAccepted: ch.liveAccept("delivery", fp) != nil,
		ReleaseDeclared:  ch.liveAccept("release", fp) != nil,
		ReleaseVerified:  releaseVerified,
		ProductVerified:  product,
	}
	for _, pb := range ch.Problems {
		p.Problems = append(p.Problems, problemView{
			ID: pb.ID, Summary: pb.Summary, Disposition: pb.Disposition, Reason: pb.Reason,
			Scope: pb.Scope, VerifiedFixed: ch.verifiedFixed(pb, fp),
		})
	}
	return p
}

func currentVersion(v *VersionFact, gen int, fp Fingerprint) *VersionFact {
	if v.current(gen, fp) {
		return v
	}
	return nil
}

func firstName(p Project, when string) string {
	for _, c := range p.Checks {
		if c.When == when {
			return c.Name
		}
	}
	return ""
}

func writeHuman(w io.Writer, p statusPayload) {
	if p.ID == "" {
		fmt.Fprintln(w, "没有进行中的变更")
		fmt.Fprintln(w, "下一合法动作:")
		for _, n := range p.Next {
			fmt.Fprintln(w, "  - "+n)
		}
		return
	}
	fmt.Fprintf(w, "变更: %s\n目标: %s\n阶段: %s\n状态: %s\n发布意图: %t\n", p.ID, p.Goal, p.Stage, p.Status, p.ReleaseIntent)
	if p.Block != "" {
		fmt.Fprintf(w, "阻塞: %s\n", p.Block)
	}
	fmt.Fprintf(w, "候选: %s %s\n", p.Candidate.Kind, p.Candidate.Hash)
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
			fmt.Fprintf(w, "  %s %s %s 记录=%s 有效=%s %s\n", c.Name, c.When, c.Layer, c.Status, c.EffectiveStatus, exit)
		}
	}
	if len(p.Problems) > 0 {
		fmt.Fprintln(w, "问题:")
		for _, pb := range p.Problems {
			fmt.Fprintf(w, "  %s %s 已验证修复=%t 范围=%s\n", pb.ID, pb.Disposition, pb.VerifiedFixed, pb.Scope)
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
