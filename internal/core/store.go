package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func runtimeDir(root string) string {
	return filepath.Join(root, ".lauren", "runtime")
}

func statePath(root string) string {
	return filepath.Join(runtimeDir(root), "state.json")
}

func WithState(root string, fn func(*State) error) error {
	dir := runtimeDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, "lock")
	lf, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	st, err := readState(statePath(root))
	if err != nil {
		return err
	}
	if err := fn(st); err != nil {
		return err
	}
	return writeAtomic(statePath(root), st)
}

func readState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return &State{}, nil
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("状态文件损坏，未覆盖：%w", err)
	}
	return &st, nil
}

func writeAtomic(path string, st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
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
	if p.Profile == "" {
		p.Profile = "full"
	}
	if p.Environment == "" {
		p.Environment = "local"
	}
	return p, nil
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
