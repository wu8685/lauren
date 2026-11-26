package core

import (
	"os"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "core.quotepath", "true")
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "init")
}

func fpHash(t *testing.T, dir string) string {
	t.Helper()
	proj, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp := fingerprint(dir, proj)
	if fp.Kind != "ok" {
		t.Fatalf("fingerprint kind=%s note=%s", fp.Kind, fp.Note)
	}
	return fp.Hash
}

func TestSpecialPathsChangeFingerprint(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	names := []string{
		"中文.txt",
		"space name.txt",
		"quote\"x.txt",
		"back\\slash.txt",
		"line\nname.txt",
	}
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		h1 := fpHash(t, dir)
		h1b := fpHash(t, dir)
		if h1 != h1b {
			t.Fatalf("%q 同一状态指纹不稳定", name)
		}
		if err := os.WriteFile(path, []byte("v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		h2 := fpHash(t, dir)
		if h1 == h2 {
			t.Fatalf("%q 内容变了，指纹却没变", name)
		}
		os.Remove(path)
	}
}

func TestRenameDeleteSymlinkAndReadError(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, filepath.Join(dir, "old name.txt"), "body\n")
	runGit(t, dir, "add", "old name.txt")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "add")
	runGit(t, dir, "mv", "old name.txt", "new name.txt")
	before := fpHash(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "new name.txt"), []byte("body changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fpHash(t, dir) == before {
		t.Fatal("重命名后的文件内容变化应改变指纹")
	}

	writeFile(t, filepath.Join(dir, "gone.txt"), "x\n")
	runGit(t, dir, "add", "gone.txt")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "gone")
	present := fpHash(t, dir)
	os.Remove(filepath.Join(dir, "gone.txt"))
	if fpHash(t, dir) == present {
		t.Fatal("删除应改变指纹")
	}

	if err := os.Symlink("keep.txt", filepath.Join(dir, "the-link")); err != nil {
		t.Fatal(err)
	}
	linked := fpHash(t, dir)
	os.Remove(filepath.Join(dir, "the-link"))
	if err := os.Symlink("gone.txt", filepath.Join(dir, "the-link")); err != nil {
		t.Fatal(err)
	}
	if fpHash(t, dir) == linked {
		t.Fatal("符号链接目标变化应改变指纹")
	}

	writeFile(t, filepath.Join(dir, "not-file.txt"), "z\n")
	runGit(t, dir, "add", "not-file.txt")
	os.Remove(filepath.Join(dir, "not-file.txt"))
	if err := os.Mkdir(filepath.Join(dir, "not-file.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp := fingerprint(dir, proj)
	if fp.Kind == "ok" {
		t.Fatalf("索引里的文件被换成目录时不能静默当成删除: %s", fp.Note)
	}
}

func TestOldFingerprintDoesNotStayValid(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, filepath.Join(dir, "lauren.project.json"), `{
	  "schema": 1,
	  "environments": ["development", "integration", "staging", "production"],
	  "defaults": {"environment": "development", "release_stage": "excluded", "artifact_level": "standard", "integration_branch": "main"},
	  "checks": [{"name": "unit", "layer": "unit", "gate": "self_verification", "argv": ["./checks/unit.sh"], "required": true}],
	  "requirements": []
	}`)
	writeFile(t, filepath.Join(dir, "checks", "unit.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(dir, "artifacts", "demand.json"), `{"goal":"g","acceptance":["a"],"baseline":"b"}`)
	writeFile(t, filepath.Join(dir, "artifacts", "plan.json"), `{"approach":"a","risks":"r","prechecks":["p"]}`)
	writeFile(t, filepath.Join(dir, "artifacts", "impl.json"), `{"summary":"s"}`)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=dev@example.com", "-c", "user.name=dev", "commit", "-m", "checks")
	must(t, dir, ExitOK, "change", "create", "--goal", "旧指纹")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/demand.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/plan.json")
	must(t, dir, ExitOK, "change", "advance")
	must(t, dir, ExitOK, "change", "submit", "--artifact", "artifacts/impl.json")
	must(t, dir, ExitOK, "check", "--name", "unit")
	if namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["effective_status"] != "pass" {
		t.Fatal("当前算法下应通过")
	}
	id := must(t, dir, ExitOK, "status")["id"].(string)
	err := WithChange(dir, id, func(ch *Change) error {
		for i := range ch.Runs {
			ch.Runs[i].Fingerprint = "legacy-v1"
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if namedCheck(t, must(t, dir, ExitOK, "status"), "unit")["effective_status"] == "pass" {
		t.Fatal("旧算法留下的指纹不能继续当作通过")
	}
	_, m, errb := runAt(t, dir, "change", "advance")
	if errText(m, errb) == "" {
		t.Fatal("旧指纹不能推进")
	}
}
