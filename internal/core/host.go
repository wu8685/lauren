package core

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	hostHookTimeout = 5
	hostRunNote     = "本次只把纯文本写到 stdout。Claude Code 和 Codex 的 SessionStart 会把这段 stdout 加进会话。没有启动这两个宿主，所以 host_injected 仍是 false。"
	hostPlanNote    = "用户级配置，只授权当前 Git 仓库。不改项目文件，不自动初始化、推进或跑检查。不写 Codex 的 config.toml，也不把 hooks 写进插件清单。Codex 的非托管钩子要在 /hooks 里信任后才会运行。配置文件写好不等于宿主已经注入。"
)

var hostRunTimeout = 4 * time.Second

var hostRunBody = func(ctx context.Context, a *app, host, home, start string) int {
	if ctx.Err() != nil {
		return ExitInternal
	}
	return a.deliverHost(host, home, start)
}

type hostPlan struct {
	OK      bool     `json:"ok"`
	Command string   `json:"command"`
	Applied bool     `json:"applied"`
	Error   string   `json:"error,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Scope   string   `json:"scope,omitempty"`
	Note    string   `json:"note,omitempty"`
}

type hostRunView struct {
	OK               bool   `json:"ok"`
	Command          string `json:"command"`
	Host             string `json:"host"`
	Protocol         string `json:"protocol"`
	ContextDelivered bool   `json:"context_delivered"`
	HostInjected     bool   `json:"host_injected"`
	Injection        string `json:"injection,omitempty"`
	Error            string `json:"error,omitempty"`
}

type hostRegistry struct {
	Schema int        `json:"schema"`
	Repos  []hostRepo `json:"repos,omitempty"`
}

type hostRepo struct {
	GitCommonDir string `json:"git_common_dir"`
}

func (a *app) cmdHost(args []string) int {
	if len(args) == 0 {
		return a.fail(ExitUsage, "用法：lauren host preview|install|uninstall|run")
	}
	switch args[0] {
	case "preview":
		return a.hostWrite(args[1:], false, false)
	case "install":
		return a.hostWrite(args[1:], true, false)
	case "uninstall":
		return a.hostWrite(args[1:], true, true)
	case "run":
		return a.cmdHostRun(args[1:])
	default:
		return a.fail(ExitUsage, "未知 host 子命令 "+args[0])
	}
}

func (a *app) hostWrite(args []string, allowApply, remove bool) int {
	fs := flag.NewFlagSet("host", flag.ContinueOnError)
	fs.SetOutput(a.err)
	apply := fs.Bool("apply", false, "写入宿主配置")
	homeFlag := fs.String("home", "", "宿主配置家目录")
	binFlag := fs.String("bin", "", "钩子调用的 lauren 绝对路径")
	hostFlag := fs.String("host", "", "claude、codex，或留空表示两个都处理")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	home, err := resolveHome(*homeFlag)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	bin, err := resolveBin(*binFlag)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	hosts, err := selectedHosts(*hostFlag)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	start, err := a.startDir()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	if _, _, err := locate(start); err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	common, err := gitCommon(start)
	if err != nil {
		return a.fail(ExitUsage, "目标路径不是 git 工作区")
	}
	claudePath := claudeSettings(home)
	codexPath := codexHooks(home)
	regPath := hostRegistryPath(home)
	claudeDoc, claudeOld, err := loadHostJSON(claudePath)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	codexDoc, codexOld, err := loadHostJSON(codexPath)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	reg, err := readRegistry(regPath)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	doApply := allowApply && *apply
	cmdName := "host preview"
	if remove {
		cmdName = "host uninstall"
	} else if allowApply {
		cmdName = "host install"
	}
	if doApply {
		for _, host := range hosts {
			doc := claudeDoc
			if host == "codex" {
				doc = codexDoc
			}
			if remove {
				removeOurHook(doc, host)
			} else if err := upsertOurHook(doc, host, hookCommand(bin, host, home)); err != nil {
				return a.fail(ExitUsage, err.Error())
			}
		}
		if remove {
			reg.Repos = dropRepo(reg.Repos, common)
		} else {
			reg.Repos = addRepo(reg.Repos, common)
		}
		if err := commitHostEdits(claudePath, claudeOld, claudeDoc, codexPath, codexOld, codexDoc, regPath, reg); err != nil {
			return a.fail(ExitUsage, err.Error())
		}
	}
	plan := hostPlan{
		OK: true, Command: cmdName, Applied: doApply,
		Paths: []string{claudePath, codexPath, regPath}, Scope: common, Note: hostPlanNote,
	}
	if a.json {
		return a.emit(ExitOK, plan)
	}
	fmt.Fprintf(a.out, "宿主配置: %s\nCodex 钩子: %s\n授权登记: %s\n仓库: %s\n", claudePath, codexPath, regPath, common)
	if doApply {
		fmt.Fprintln(a.out, "已写入。这次没有启动宿主，配置写好不等于已经注入。")
	} else {
		fmt.Fprintln(a.out, "只展示，没有写入。确认后加 --apply。")
	}
	fmt.Fprintln(a.out, hostPlanNote)
	return ExitOK
}

func (a *app) cmdHostRun(args []string) int {
	fs := flag.NewFlagSet("host run", flag.ContinueOnError)
	fs.SetOutput(a.err)
	host := fs.String("host", "", "claude 或 codex")
	homeFlag := fs.String("home", "", "宿主配置家目录")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *host != "claude" && *host != "codex" {
		return a.fail(ExitUsage, "--host 只能是 claude 或 codex")
	}
	home, err := resolveHome(*homeFlag)
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	start, err := a.startDir()
	if err != nil {
		return a.fail(ExitUsage, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostRunTimeout)
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- hostRunBody(ctx, a, *host, home, start)
	}()
	select {
	case code := <-done:
		if ctx.Err() != nil && code != ExitOK {
			fmt.Fprintln(a.err, "session-context 超时，会话继续")
			return ExitOK
		}
		return code
	case <-ctx.Done():
		fmt.Fprintln(a.err, "session-context 超时，会话继续")
		return ExitOK
	}
}

func (a *app) deliverHost(host, home, start string) int {
	ok, err := repoAuthorized(home, start)
	if err != nil {
		return a.finishHostRun(host, false, err.Error())
	}
	if !ok {
		return a.finishHostRun(host, false, "")
	}
	view, code := assembleSession(start)
	if code != ExitOK {
		return a.finishHostRun(host, false, view.Error)
	}
	if !view.Applicable {
		return a.finishHostRun(host, false, "")
	}
	if a.json {
		return a.emit(ExitOK, hostRunView{
			OK: true, Command: "host run", Host: host, Protocol: "plain-text-stdout",
			ContextDelivered: true, HostInjected: false, Injection: hostRunNote,
		})
	}
	writeSession(a.out, view)
	return ExitOK
}

func (a *app) finishHostRun(host string, delivered bool, diagnosis string) int {
	if diagnosis != "" {
		fmt.Fprintln(a.err, diagnosis)
	}
	if a.json {
		view := hostRunView{
			OK: true, Command: "host run", Host: host, Protocol: "plain-text-stdout",
			ContextDelivered: delivered, HostInjected: false, Injection: hostRunNote, Error: diagnosis,
		}
		return a.emit(ExitOK, view)
	}
	return ExitOK
}

func repoAuthorized(home, start string) (bool, error) {
	common, err := gitCommon(start)
	if err != nil {
		return false, nil
	}
	reg, err := readRegistry(hostRegistryPath(home))
	if err != nil {
		return false, err
	}
	for _, repo := range reg.Repos {
		if samePath(repo.GitCommonDir, common) {
			return true, nil
		}
	}
	return false, nil
}

func selectedHosts(host string) ([]string, error) {
	switch host {
	case "":
		return []string{"claude", "codex"}, nil
	case "claude", "codex":
		return []string{host}, nil
	default:
		return nil, fmt.Errorf("--host 只能是 claude 或 codex")
	}
}

func resolveHome(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return filepath.Abs(flag)
	}
	return os.UserHomeDir()
}

func resolveBin(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return filepath.Abs(flag)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

func claudeSettings(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

func codexHooks(home string) string {
	if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
		if user, err := os.UserHomeDir(); err == nil && samePath(home, user) {
			return filepath.Join(env, "hooks.json")
		}
	}
	return filepath.Join(home, ".codex", "hooks.json")
}

func hostRegistryPath(home string) string {
	return filepath.Join(home, ".lauren", "host-registry.json")
}

func hookCommand(bin, host, home string) string {
	return shellQuote(bin) + " host run --host " + host + " --home " + shellQuote(home)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func isOurCommand(cmd, host string) bool {
	return strings.Contains(cmd, " host run --host "+host)
}

func loadHostJSON(path string) (map[string]any, []byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(string(b)) == "" {
		return map[string]any{}, b, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, b, fmt.Errorf("宿主配置无法解析，没有改写：%s", path)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, b, nil
}

func readRegistry(path string) (hostRegistry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return hostRegistry{Schema: 1}, nil
	}
	if err != nil {
		return hostRegistry{}, err
	}
	var reg hostRegistry
	if err := json.Unmarshal(b, &reg); err != nil {
		return hostRegistry{}, fmt.Errorf("授权登记无法解析，没有改写：%s", path)
	}
	if reg.Schema == 0 {
		reg.Schema = 1
	}
	return reg, nil
}

func addRepo(repos []hostRepo, common string) []hostRepo {
	for _, repo := range repos {
		if samePath(repo.GitCommonDir, common) {
			return repos
		}
	}
	return append(repos, hostRepo{GitCommonDir: common})
}

func dropRepo(repos []hostRepo, common string) []hostRepo {
	var out []hostRepo
	for _, repo := range repos {
		if samePath(repo.GitCommonDir, common) {
			continue
		}
		out = append(out, repo)
	}
	return out
}

func upsertOurHook(doc map[string]any, host, command string) error {
	hooks, err := hookMap(doc)
	if err != nil {
		return err
	}
	groups, err := sessionGroups(hooks)
	if err != nil {
		return err
	}
	found := false
	for _, group := range groups {
		gm, ok := group.(map[string]any)
		if !ok {
			continue
		}
		list, _ := gm["hooks"].([]any)
		for _, item := range list {
			hm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if isOurCommand(cmd, host) {
				hm["type"] = "command"
				hm["command"] = command
				hm["timeout"] = hostHookTimeout
				found = true
			}
		}
	}
	if !found {
		groups = append(groups, map[string]any{
			"hooks": []any{map[string]any{
				"type": "command", "command": command, "timeout": hostHookTimeout,
			}},
		})
	}
	hooks["SessionStart"] = groups
	doc["hooks"] = hooks
	return nil
}

func removeOurHook(doc map[string]any, host string) {
	hooks, err := hookMap(doc)
	if err != nil || hooks == nil {
		return
	}
	groups, err := sessionGroups(hooks)
	if err != nil {
		return
	}
	var keptGroups []any
	for _, group := range groups {
		gm, ok := group.(map[string]any)
		if !ok {
			keptGroups = append(keptGroups, group)
			continue
		}
		list, _ := gm["hooks"].([]any)
		var kept []any
		for _, item := range list {
			hm, ok := item.(map[string]any)
			if !ok {
				kept = append(kept, item)
				continue
			}
			cmd, _ := hm["command"].(string)
			if isOurCommand(cmd, host) {
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			continue
		}
		gm["hooks"] = kept
		keptGroups = append(keptGroups, gm)
	}
	if len(keptGroups) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = keptGroups
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
}

func hookMap(doc map[string]any) (map[string]any, error) {
	if doc["hooks"] == nil {
		return map[string]any{}, nil
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("宿主配置的 hooks 不是对象，没有改写")
	}
	return hooks, nil
}

func sessionGroups(hooks map[string]any) ([]any, error) {
	if hooks["SessionStart"] == nil {
		return nil, nil
	}
	groups, ok := hooks["SessionStart"].([]any)
	if !ok {
		return nil, fmt.Errorf("宿主配置的 SessionStart 不是数组，没有改写")
	}
	return groups, nil
}

func commitHostEdits(claudePath string, claudeOld []byte, claudeDoc map[string]any, codexPath string, codexOld []byte, codexDoc map[string]any, regPath string, reg hostRegistry) error {
	claudeBytes, err := marshalHost(claudeDoc)
	if err != nil {
		return err
	}
	codexBytes, err := marshalHost(codexDoc)
	if err != nil {
		return err
	}
	regBytes, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	regBytes = append(regBytes, '\n')
	if len(reg.Repos) == 0 {
		regBytes = nil
	}
	if err := putHostFile(claudePath, claudeBytes); err != nil {
		return err
	}
	if err := putHostFile(codexPath, codexBytes); err != nil {
		_ = putHostFile(claudePath, claudeOld)
		return err
	}
	if err := putHostFile(regPath, regBytes); err != nil {
		_ = putHostFile(claudePath, claudeOld)
		_ = putHostFile(codexPath, codexOld)
		return err
	}
	return nil
}

func marshalHost(doc map[string]any) ([]byte, error) {
	if len(doc) == 0 {
		return nil, nil
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func putHostFile(path string, data []byte) error {
	if data == nil {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
