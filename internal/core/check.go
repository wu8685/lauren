package core

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var secretRE = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|credential)(\s*[:=]\s*)(\S+)`)
var bearerRE = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]+`)

// checkTimeout 是单次子进程上限。超时记为 fail，不是 pass。
var checkTimeout = 120 * time.Second

func redact(s string) string {
	s = secretRE.ReplaceAllString(s, "${1}${2}[redacted]")
	s = bearerRE.ReplaceAllString(s, "Bearer [redacted]")
	return s
}

func runCheck(root, changeID string, spec CheckSpec, fp Fingerprint, gen int, env, integrationBranch, integrationSHA string) CheckRun {
	now := time.Now().UTC()
	bind := BindWorktree
	if spec.Layer == "release_readback" || spec.Gate == GateRelease {
		bind = BindIntegration
	}
	run := CheckRun{
		ID: newID("r-"), Name: spec.Name, Layer: spec.Layer, Gate: spec.Gate,
		Argv: append([]string{}, spec.Argv...), Cwd: root, Environment: env,
		Started: now, Ended: now, EvidenceGeneration: gen, BindKind: bind,
		Status: "blocked", CandidateKind: fp.Kind,
	}
	if bind == BindWorktree {
		run.Fingerprint = fp.Hash
		if fp.Kind != "ok" {
			run.Note = fp.Note
			run.Ended = time.Now().UTC()
			return run
		}
	} else {
		run.CandidateKind = "ok"
		run.IntegrationBranch = integrationBranch
		run.IntegrationSHA = integrationSHA
		if integrationSHA == "" {
			run.Note = "发布读回必须绑到已记录的集成 SHA，不能用未合入的工作区候选"
			run.Ended = time.Now().UTC()
			return run
		}
	}
	if len(spec.Argv) == 0 || strings.Contains(spec.Argv[0], "..") {
		run.Status = "fail"
		run.Note = "检查命令为空或路径越界"
		run.Ended = time.Now().UTC()
		return run
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = root
	if strings.Contains(spec.Argv[0], "/") && !filepath.IsAbs(spec.Argv[0]) {
		cmd.Path = filepath.Join(root, spec.Argv[0])
		cmd.Args[0] = cmd.Path
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	raw := redact(buf.String())
	if len(raw) > 1<<20 {
		raw = raw[:1<<20] + "\n[truncated]\n"
	}
	evDir := filepath.Join(changeDir(root, changeID), "evidence")
	_ = os.MkdirAll(evDir, 0o755)
	logPath := filepath.Join(evDir, run.ID+".log")
	_ = os.WriteFile(logPath, []byte(raw), 0o644)
	run.LogPath = logPath
	run.LogBytes = len(raw)
	run.Preview = preview(raw, 180)
	run.Ended = time.Now().UTC()
	if ctx.Err() == context.DeadlineExceeded {
		run.Status = "fail"
		run.Note = "检查超时"
		return run
	}
	if err == nil {
		run.Status = "pass"
		code := 0
		run.ExitCode = &code
		return run
	}
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		run.ExitCode = &code
		if code == 75 {
			run.Status = "partial"
			return run
		}
		run.Status = "fail"
		return run
	}
	run.Status = "fail"
	run.Note = err.Error()
	return run
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func toolVersionFrom(run CheckRun) string {
	if run.LogPath == "" {
		return ""
	}
	b, err := os.ReadFile(run.LogPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "version=") {
			return strings.TrimPrefix(line, "version=")
		}
	}
	return ""
}
