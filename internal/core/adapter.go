package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func installAdapter(root, name string, proj *Project) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("适配器名称不合法")
	}
	src, err := findAdapter(name)
	if err != nil {
		return err
	}
	dst := filepath.Join(root, ".lauren", "adapters", name)
	if err := copyTree(src, dst); err != nil {
		return err
	}
	rel := ".lauren/adapters/" + name
	proj.Checks = append(proj.Checks, CheckSpec{
		Name:            name,
		Layer:           "unit",
		Gate:            GateSelf,
		Argv:            []string{"go", "run", "./" + rel, "--", "./..."},
		Required:        true,
		RequireExecuted: true,
	})
	return nil
}

func findAdapter(name string) (string, error) {
	if root := strings.TrimSpace(os.Getenv("LAUREN_ADAPTERS")); root != "" {
		cand := filepath.Join(root, name)
		if isAdapterDir(cand) {
			return cand, nil
		}
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for {
			cand := filepath.Join(dir, "adapters", name)
			if isAdapterDir(cand) {
				return cand, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("找不到适配器 %s。在 lauren 仓库里执行，或设置 LAUREN_ADAPTERS 指向 adapters 目录", name)
}

func isAdapterDir(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "main.go"))
	return err == nil && !st.IsDir()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
