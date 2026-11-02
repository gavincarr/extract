package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// run execs a command and returns combined output and exit code.
func run(t *testing.T, dir string, name string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("running %s: %v", name, err)
	return "", -1
}

func gitEnv(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "T")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "T")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	t.Setenv("GIT", gitPath)
	return home
}

func commitCount(t *testing.T, repo string) int {
	t.Helper()
	out, code := run(t, repo, os.Getenv("GIT"), "log", "--oneline")
	if code != 0 {
		t.Fatalf("git log failed (%d): %s", code, out)
	}
	return len(strings.Split(strings.TrimSpace(out), "\n"))
}

func setupGit(t *testing.T, mode string) (root, cfg, repo string) {
	gitEnv(t)
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "h1/etc/hosts"), "one\n", 0o644)
	cfg = filepath.Join(t.TempDir(), "extract.conf")
	writeFile(t, cfg, "EXTRACT_GIT_REPO_MODE="+mode+"\n", 0o644)
	repo = root
	if mode == "HOST" {
		repo = filepath.Join(root, "h1")
	}
	return
}

func TestPostExtractGitNoChangesExitsZero(t *testing.T) {
	for _, mode := range []string{"ALL", "HOST"} {
		t.Run(mode, func(t *testing.T) {
			root, cfg, repo := setupGit(t, mode)
			for i := 0; i < 2; i++ {
				out, code := run(t, "", "bash", "./post_extract_git", root, cfg, "h1", "")
				if code != 0 {
					t.Fatalf("run %d: exit %d: %s", i+1, code, out)
				}
			}
			if n := commitCount(t, repo); n != 1 {
				t.Errorf("commits = %d, want 1", n)
			}
		})
	}
}

func TestPostExtractGitCommitsChanges(t *testing.T) {
	for _, mode := range []string{"ALL", "HOST"} {
		t.Run(mode, func(t *testing.T) {
			root, cfg, repo := setupGit(t, mode)
			out, code := run(t, "", "bash", "./post_extract_git", root, cfg, "h1", "")
			if code != 0 {
				t.Fatalf("run 1: exit %d: %s", code, out)
			}
			writeFile(t, filepath.Join(root, "h1/etc/hosts"), "two\n", 0o644)
			out, code = run(t, "", "bash", "./post_extract_git", root, cfg, "h1", "")
			if code != 0 {
				t.Fatalf("run 2: exit %d: %s", code, out)
			}
			if n := commitCount(t, repo); n != 2 {
				t.Errorf("commits = %d, want 2", n)
			}
		})
	}
}

func sshEnv(t *testing.T, addL string) (calls string) {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".ssh/id_rsa"), "key\n", 0o600)
	bin := t.TempDir()
	calls = filepath.Join(t.TempDir(), "calls")
	writeFile(t, filepath.Join(bin, "ssh-add"),
		"#!/bin/bash\nif [ \"$1\" = -l ]; then\n"+addL+"\nelse\necho \"$*\" >> "+calls+"\nfi\n", 0o755)
	writeFile(t, filepath.Join(bin, "pgrep"), "#!/bin/bash\necho $SSH_AGENT_PID\n", 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("SSH_AGENT_PID", "4242")
	t.Setenv("USER", "x")
	t.Setenv("HOSTNAME", "y")
	return calls
}

func TestLoadSshKeysAlreadyLoadedExitsZero(t *testing.T) {
	calls := sshEnv(t, "echo '256 SHA256:abc key (ED25519)'; exit 0")
	out, code := run(t, "", "bash", "./pre_extract_load_ssh_keys", "/root", "/cfg")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Error("ssh-add was called to add a key although one was loaded")
	}
}

func TestLoadSshKeysNoIdentitiesAddsKey(t *testing.T) {
	calls := sshEnv(t, "echo 'The agent has no identities.'; exit 1")
	out, code := run(t, "", "bash", "./pre_extract_load_ssh_keys", "/root", "/cfg")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("ssh-add not called: %v", err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(b)), "\n")); n != 1 {
		t.Errorf("ssh-add calls = %d, want 1", n)
	}
}

func TestPostExtractTarballMissingConfigDies(t *testing.T) {
	out, code := run(t, "", "bash", "./post_extract_tarball", t.TempDir(), "/nonexistent", "h1", "")
	if code != 99 {
		t.Errorf("exit = %d, want 99", code)
	}
	if !strings.Contains(out, "Missing config file") {
		t.Errorf("output lacks 'Missing config file': %q", out)
	}
	if strings.Contains(out, "command not found") {
		t.Errorf("output has 'command not found': %q", out)
	}
}
