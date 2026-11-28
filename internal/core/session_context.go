package core

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// trustNote 是 status 和 session-context 共用的信任边界。来源声明不是身份认证。
const trustNote = "本地状态文件可以被改写。这个 CLI 不认证人类，也不能防止恶意篡改。合并是否可信看仓库 CI 重跑；交付是否接受看来源被明确记下的接受记录。不会自动 merge、push、部署或通知。"

const (
	contextLimitBytes = 4000
	contextClipRunes  = 180
)

type sessionView struct {
	OK             bool           `json:"ok"`
	Command        string         `json:"command"`
	Applicable     bool           `json:"applicable"`
	Kind           string         `json:"kind,omitempty"`
	Error          string         `json:"error,omitempty"`
	Workspace      *workspaceView `json:"workspace,omitempty"`
	EntryReady     *bool          `json:"entry_ready,omitempty"`
	RuntimePresent *bool          `json:"runtime_present,omitempty"`
	HostInjected   bool           `json:"host_injected"`
	ID             string         `json:"id,omitempty"`
	Goal           string         `json:"goal,omitempty"`
	Stage          string         `json:"stage,omitempty"`
	Status         string         `json:"status,omitempty"`
	Block          string         `json:"block,omitempty"`
	Next           []string       `json:"next,omitempty"`
	Red            *redState      `json:"red,omitempty"`
	Guidance       []string       `json:"guidance,omitempty"`
	Trust          string         `json:"trust,omitempty"`
}

func (a *app) cmdSessionContext(args []string) int {
	if len(args) != 0 {
		return a.fail(ExitUsage, "用法：lauren session-context")
	}
	start := a.root
	if strings.TrimSpace(start) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return a.contextFail("config", err.Error())
		}
		start = wd
	}
	lay, proj, err := locate(start)
	if err != nil {
		kind, quiet := contextErrorKind(err)
		if quiet {
			view := sessionView{OK: true, Command: "session-context", Applicable: false, Kind: "not_applicable"}
			if a.json {
				return a.emit(ExitOK, view)
			}
			fmt.Fprintln(a.out, "不适用")
			return ExitOK
		}
		return a.contextFail(kind, err.Error())
	}
	ch, err := readCurrentChange(lay.Worktree)
	if err != nil {
		return a.contextFail("state", err.Error())
	}
	ready := entryReady(lay)
	present := runtimePresent(lay.Runtime)
	view := sessionView{
		OK: true, Command: "session-context", Applicable: true,
		Workspace:      workspaceViewOf(lay.Worktree),
		EntryReady:     boolPtr(ready),
		RuntimePresent: boolPtr(present),
		Trust:          trustNote,
	}
	var next []string
	if ch == nil {
		next = []string{"创建变更"}
	} else {
		fp := fingerprint(lay.Worktree, ch.projectView(proj))
		next = nextActions(lay.Worktree, ch, proj, fp, libsFor(lay.Worktree, ch))
		view.ID = ch.ID
		view.Goal = clipText(ch.Goal, contextClipRunes)
		view.Stage = ch.Stage
		view.Status = ch.Status
		if ch.Block != nil && ch.Status == StatusBlocked {
			view.Block = clipText(ch.Block.Reason, contextClipRunes)
		}
		view.Red = &redState{Required: proj.redRequired(), Recorded: ch.redRecorded()}
		if ch.RedSkip != nil && strings.TrimSpace(ch.RedSkip.Reason) != "" {
			view.Red.Skipped = true
			view.Red.SkipReason = clipText(ch.RedSkip.Reason, contextClipRunes)
		}
	}
	view.Next = next
	view.Guidance = sessionGuidance(ready, ch, proj, next)
	if a.json {
		return a.emit(ExitOK, view)
	}
	writeSession(a.out, view)
	return ExitOK
}

func (a *app) contextFail(kind, msg string) int {
	if a.json {
		return a.emit(ExitUsage, sessionView{
			OK: false, Command: "session-context", Applicable: false, Kind: kind, Error: msg,
		})
	}
	fmt.Fprintln(a.err, msg)
	return ExitUsage
}

func contextErrorKind(err error) (string, bool) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "配置入口断了") || strings.Contains(msg, "共享配置不存在"):
		return "broken_link", false
	case strings.Contains(msg, "无法定位主工作区"):
		return "git", false
	case strings.Contains(msg, "旧的单层"),
		strings.Contains(msg, "不在 worktree 根"),
		strings.Contains(msg, "没有 .lauren/project/lauren.project.json"),
		strings.Contains(msg, "配置冲突"),
		strings.Contains(msg, "无法解析"),
		strings.Contains(msg, "Git 仓库之外"),
		strings.Contains(msg, "不能覆盖"),
		strings.Contains(msg, "不能解析共享配置引用"):
		return "config", false
	case strings.Contains(msg, "未初始化"):
		return "not_applicable", true
	default:
		return "config", false
	}
}

func entryReady(lay Layout) bool {
	switch lay.ConfigMode {
	case modeLegacy, modeSymlink, modeReference:
		return true
	default:
		return lay.Entry != ""
	}
}

func runtimePresent(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func readCurrentChange(root string) (*Change, error) {
	ptr, err := readPointer(root)
	if err != nil {
		return nil, err
	}
	if ptr != "" {
		ch, err := readChange(root, ptr)
		if err != nil {
			return nil, err
		}
		if ch != nil && (ch.Status == StatusActive || ch.Status == StatusBlocked) {
			return ch, nil
		}
	}
	all, err := localChanges(root)
	if err != nil {
		return nil, err
	}
	var open *Change
	n := 0
	for _, ch := range all {
		if ch.Status == StatusActive || ch.Status == StatusBlocked {
			n++
			open = ch
		}
	}
	if n > 1 {
		return nil, fmt.Errorf("这个工作区有多个进行中的变更，请使用 --id")
	}
	return open, nil
}

func sessionGuidance(ready bool, ch *Change, proj Project, next []string) []string {
	var g []string
	if !ready {
		g = append(g, "本地还没有配置入口。执行 lauren init-worktree，然后再次读取 session-context 确认入口已就绪。")
	}
	if ch == nil {
		if ready {
			g = append(g, "没有进行中的变更。按需求执行 lauren change create --goal \"<目标>\"。不会自动创建，也不会补造历史。")
		}
		g = append(g, "这条命令的成功不表示宿主已经注入了上下文。")
		return g
	}
	if ch.Status == StatusBlocked {
		reason := ""
		if ch.Block != nil {
			reason = clipText(ch.Block.Reason, contextClipRunes)
		}
		g = append(g, "当前阻塞："+reason+"。先处理这个原因，再由人决定是否恢复。不要自动 resume。")
	}
	if redGap(proj, ch) {
		g = append(g, "Red 还没有先失败再通过的记录。要真实执行检查，让它先失败并写出分类，再跑通过。不要跳过 Red。")
	}
	var safe []string
	over := false
	for _, n := range next {
		switch n {
		case "恢复", "记录交付接受", "记录合入集成分支", "记录发布声明", "人确认后释放共享环境占用":
			over = true
		default:
			safe = append(safe, n)
		}
	}
	if len(safe) > 0 {
		g = append(g, "可继续的动作："+strings.Join(safe, "、")+"。")
	}
	if over || ch.Status == StatusBlocked {
		g = append(g, "交付接受、合入、发布和解除阻塞要由人决定。不会自动接受、合并、发布或 resume。")
	}
	g = append(g, "这条命令的成功不表示宿主已经注入了上下文。")
	return g
}

func redGap(proj Project, ch *Change) bool {
	if ch == nil || ch.Status != StatusActive || !proj.redRequired() {
		return false
	}
	if ch.RedSkip != nil && strings.TrimSpace(ch.RedSkip.Reason) != "" {
		return false
	}
	if ch.redRecorded() {
		return false
	}
	return stageIndex(ch.Stage) >= stageIndex(StageBuild)
}

func boolPtr(v bool) *bool { return &v }

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func writeSession(w io.Writer, v sessionView) {
	var b strings.Builder
	if !v.Applicable {
		fmt.Fprintln(&b, "不适用")
		io.WriteString(w, b.String())
		return
	}
	fmt.Fprintln(&b, "session-context")
	if v.Workspace != nil {
		fmt.Fprintf(&b, "工作区: %s\n配置来源: %s (%s)\n", v.Workspace.Worktree, v.Workspace.ConfigPath, v.Workspace.ConfigMode)
		if v.Workspace.Entry != "" {
			fmt.Fprintf(&b, "本地入口文件: %s\n", v.Workspace.Entry)
		}
	}
	if v.EntryReady != nil && *v.EntryReady {
		fmt.Fprintln(&b, "本地入口: 就绪")
	} else {
		fmt.Fprintln(&b, "本地入口: 还没有")
	}
	if v.RuntimePresent != nil && *v.RuntimePresent {
		fmt.Fprintln(&b, "运行目录: 已有")
	} else {
		fmt.Fprintln(&b, "运行目录: 还没有。这不等于未初始化")
	}
	if v.ID == "" {
		fmt.Fprintln(&b, "变更: 没有")
	} else {
		fmt.Fprintf(&b, "变更: %s\n阶段: %s\n状态: %s\n", v.ID, v.Stage, v.Status)
	}
	if v.Block != "" {
		fmt.Fprintf(&b, "阻塞: %s\n", v.Block)
	}
	fmt.Fprintln(&b, "下一步:")
	for _, n := range v.Next {
		fmt.Fprintln(&b, "  - "+n)
	}
	fmt.Fprintln(&b, "指引:")
	for _, line := range v.Guidance {
		fmt.Fprintln(&b, "  - "+line)
	}
	if v.Trust != "" {
		fmt.Fprintln(&b, "信任: "+v.Trust)
	}
	out := b.String()
	if len(out) > contextLimitBytes {
		out = out[:contextLimitBytes] + "\n输出已截断\n"
	}
	io.WriteString(w, out)
}
