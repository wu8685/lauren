package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func runtimeDir(root string) string {
	return filepath.Join(root, ".lauren", "runtime")
}

func changesDir(root string) string {
	return filepath.Join(runtimeDir(root), "changes")
}

func changeDir(root, id string) string {
	return filepath.Join(changesDir(root), id)
}

func pointerPath(root string) string {
	return filepath.Join(runtimeDir(root), "worktree.json")
}

func validChangeID(id string) error {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		return fmt.Errorf("变更 ID 不合法")
	}
	return nil
}

func WithChange(root, id string, fn func(*Change) error) error {
	if err := validChangeID(id); err != nil {
		return err
	}
	dir := changeDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	ch, err := readChange(root, id)
	if err != nil {
		return err
	}
	if ch == nil {
		return fmt.Errorf("找不到变更 %s", id)
	}
	if err := fn(ch); err != nil {
		return err
	}
	return writeChange(root, ch)
}

func readChange(root, id string) (*Change, error) {
	b, err := os.ReadFile(filepath.Join(changeDir(root, id), "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ch Change
	if err := json.Unmarshal(b, &ch); err != nil {
		return nil, fmt.Errorf("变更 %s 的状态损坏，未覆盖：%w", id, err)
	}
	return &ch, nil
}

func writeChange(root string, ch *Change) error {
	return writeAtomicJSON(filepath.Join(changeDir(root, ch.ID), "state.json"), ch)
}

func listChangeIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(changesDir(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func localChanges(root string) ([]*Change, error) {
	ids, err := listChangeIDs(root)
	if err != nil {
		return nil, err
	}
	var out []*Change
	for _, id := range ids {
		ch, err := readChange(root, id)
		if err != nil {
			return nil, err
		}
		if ch != nil {
			out = append(out, ch)
		}
	}
	return out, nil
}

func readPointer(root string) (string, error) {
	b, err := os.ReadFile(pointerPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var p WorktreePointer
	if err := json.Unmarshal(b, &p); err != nil {
		return "", err
	}
	return p.ChangeID, nil
}

func writePointer(root, id string) error {
	if err := os.MkdirAll(runtimeDir(root), 0o755); err != nil {
		return err
	}
	return writeAtomicJSON(pointerPath(root), WorktreePointer{ChangeID: id})
}

func withWorktreeLock(root string, fn func() error) error {
	if err := os.MkdirAll(runtimeDir(root), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(runtimeDir(root), "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)
	return fn()
}

func createChange(root string, ch *Change) error {
	return withWorktreeLock(root, func() error {
		existing, err := localChanges(root)
		if err != nil {
			return err
		}
		for _, c := range existing {
			if c.Status == StatusActive || c.Status == StatusBlocked {
				return fmt.Errorf("这个工作区已有进行中的变更 %s。取消或关闭之后才能在这里创建下一个。别的工作区可以各自有一个", c.ID)
			}
		}
		if err := os.MkdirAll(changeDir(root, ch.ID), 0o755); err != nil {
			return err
		}
		if err := writeChange(root, ch); err != nil {
			return err
		}
		return writePointer(root, ch.ID)
	})
}

func resolveChangeID(root, id string, need bool) (string, error) {
	if id != "" {
		if err := validChangeID(id); err != nil {
			return "", err
		}
		ch, err := readChange(root, id)
		if err != nil {
			return "", err
		}
		if ch == nil {
			return "", fmt.Errorf("找不到变更 %s", id)
		}
		return id, nil
	}
	ptr, err := readPointer(root)
	if err != nil {
		return "", err
	}
	if ptr != "" {
		ch, err := readChange(root, ptr)
		if err != nil {
			return "", err
		}
		if ch != nil && (ch.Status == StatusActive || ch.Status == StatusBlocked || !need) {
			return ptr, nil
		}
	}
	all, err := localChanges(root)
	if err != nil {
		return "", err
	}
	var opens []string
	for _, c := range all {
		if c.Status == StatusActive || c.Status == StatusBlocked {
			opens = append(opens, c.ID)
		}
	}
	if len(opens) == 1 {
		_ = writePointer(root, opens[0])
		return opens[0], nil
	}
	if !need {
		if len(all) > 0 {
			return all[len(all)-1].ID, nil
		}
		return "", nil
	}
	if len(opens) == 0 {
		return "", fmt.Errorf("这个工作区没有进行中的变更")
	}
	return "", fmt.Errorf("这个工作区有多个进行中的变更，请使用 --id")
}

func writeAtomicJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadProject(root string) (Project, error) {
	b, err := os.ReadFile(filepath.Join(root, "lauren.project.json"))
	if err != nil {
		return Project{}, fmt.Errorf("读取 lauren.project.json：%w", err)
	}
	var p Project
	if err := json.Unmarshal(b, &p); err != nil {
		return Project{}, fmt.Errorf("lauren.project.json 无法解析：%w", err)
	}
	if p.Schema == 0 {
		p.Schema = 1
	}
	if err := p.validate(); err != nil {
		return Project{}, err
	}
	return p, nil
}

func (p Project) promoteThreshold() int {
	if p.Defaults.PromoteThreshold <= 0 {
		return 2
	}
	return p.Defaults.PromoteThreshold
}

const defaultCheckTimeoutSeconds int64 = 600

// maxCheckTimeoutSeconds 是 time.Duration 能表示的最大整秒。再大就会在换算时溢出。
var maxCheckTimeoutSeconds = int64(math.MaxInt64 / int64(time.Second))

func (p Project) projectTimeoutSeconds() int64 {
	if p.Defaults.CheckTimeoutSeconds <= 0 {
		return defaultCheckTimeoutSeconds
	}
	return p.Defaults.CheckTimeoutSeconds
}

func (p Project) effectiveTimeoutSeconds(spec CheckSpec) int64 {
	if spec.TimeoutSeconds > 0 {
		return spec.TimeoutSeconds
	}
	return p.projectTimeoutSeconds()
}

func validateTimeoutSeconds(field string, seconds int64) error {
	if seconds < 0 {
		return fmt.Errorf("%s 不能为负", field)
	}
	if seconds > maxCheckTimeoutSeconds {
		return fmt.Errorf("%s 超过 %d 秒，时间无法表示", field, maxCheckTimeoutSeconds)
	}
	return nil
}

func (p Project) validate() error {
	if len(p.Environments) == 0 {
		return fmt.Errorf("项目配置缺少 environments")
	}
	d := p.Defaults
	if !containsStr(p.Environments, d.Environment) {
		return fmt.Errorf("defaults.environment 不在 environments 里")
	}
	if d.ReleaseStage != ReleaseIncluded && d.ReleaseStage != ReleaseExcluded {
		return fmt.Errorf("defaults.release_stage 只能是 included 或 excluded")
	}
	if d.ArtifactLevel != LevelStandard && d.ArtifactLevel != LevelAbbreviated {
		return fmt.Errorf("defaults.artifact_level 只能是 standard 或 abbreviated")
	}
	if strings.TrimSpace(d.IntegrationBranch) == "" {
		return fmt.Errorf("defaults.integration_branch 不能为空")
	}
	if d.PromoteThreshold < 0 {
		return fmt.Errorf("defaults.promote_threshold 不能为负")
	}
	if err := validateTimeoutSeconds("defaults.check_timeout_seconds", d.CheckTimeoutSeconds); err != nil {
		return err
	}
	for _, ch := range p.Checks {
		switch ch.Gate {
		case GateSelf, GateAcceptance, GateRelease:
		default:
			return fmt.Errorf("检查 %s 的 gate 不合法", ch.Name)
		}
		if layerRank[ch.Layer] == 0 {
			return fmt.Errorf("检查 %s 的 layer 不合法", ch.Name)
		}
		for _, env := range ch.Environments {
			if !containsStr(p.Environments, env) {
				return fmt.Errorf("检查 %s 的环境 %s 不在项目 environments 里", ch.Name, env)
			}
		}
		if err := validateTimeoutSeconds(fmt.Sprintf("检查 %s 的 timeout_seconds", ch.Name), ch.TimeoutSeconds); err != nil {
			return err
		}
	}
	return nil
}

func findRoot(start string) (string, error) {
	d, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "lauren.project.json")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("从 %s 向上未找到 lauren.project.json", start)
		}
		d = parent
	}
}

func worktreeRoots(root string) []string {
	out, err := gitOut(root, "worktree", "list", "--porcelain")
	if err != nil {
		return []string{root}
	}
	var roots []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			roots = append(roots, strings.TrimPrefix(line, "worktree "))
		}
	}
	if len(roots) == 0 {
		return []string{root}
	}
	return roots
}

func findSharedHold(root string) (string, string, bool) {
	for _, wt := range worktreeRoots(root) {
		chs, err := localChanges(wt)
		if err != nil {
			continue
		}
		for _, ch := range chs {
			if ch.ReleaseHold != nil && ch.ReleaseHold.Active {
				return wt, ch.ID, true
			}
		}
	}
	return "", "", false
}
