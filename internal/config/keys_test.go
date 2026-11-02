package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUnknownKeys(t *testing.T) {
	cases := []struct {
		name, conf, want string
	}{
		{"typo hint", "EXTRACT_HOSTS_CMD=x\n", "unknown key EXTRACT_HOSTS_CMD (did you mean EXTRACT_HOST_CMD?)"},
		{"hook typo hint", "PRE_EXTRACT_HOSTS=x\n", "unknown key PRE_EXTRACT_HOSTS (did you mean PRE_EXTRACT_HOST?)"},
		{"post typo", "POST_EXTRACT_TEARDOWNS=x\n", "(did you mean POST_EXTRACT_TEARDOWN?)"},
		{"export", "export EXTRACT_HOSTS_CMD=x\n", "unknown key EXTRACT_HOSTS_CMD"},
		{"allowed key hint", "EXTRACT_GIT_REPO_MOD=x\n", "(did you mean EXTRACT_GIT_REPO_MODE?)"},
		{"no close match", "EXTRACT_FROBNICATE=x\n", "unknown key EXTRACT_FROBNICATE"},
		{"far away", "EXTRACT_SOMETHING_ENTIRELY_DIFFERENT=x\n", "unknown key EXTRACT_SOMETHING_ENTIRELY_DIFFERENT"},
		{"line number", "# c\n\nEXTRACT_HOSTS_CMD=x\n", "line 5: unknown key"},
		{"several sorted", "EXTRACT_ZZZ_BAD=1\nEXTRACT_AAA_BAD=1\n", "unknown key EXTRACT_AAA_BAD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmp := t.TempDir()
			_, err := Load(writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_TREE=/etc\n"+c.conf))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "config file ") {
				t.Errorf("error %q should name the config file", err)
			}
			wantErr(t, err, c.want)
			if strings.Contains(err.Error(), "did you mean") != strings.Contains(c.want, "did you mean") && c.name != "line number" && c.name != "export" {
				t.Errorf("hint mismatch: %q", err)
			}
		})
	}
}

func TestUnknownKeysAllReported(t *testing.T) {
	tmp := t.TempDir()
	_, err := Load(writeConf(t, tmp, "EXTRACT_ROOT="+tmp+"\nEXTRACT_TREE=/etc\nEXTRACT_ZZZ_BAD=1\nEXTRACT_AAA_BAD=1\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	a, z := strings.Index(s, "EXTRACT_AAA_BAD"), strings.Index(s, "EXTRACT_ZZZ_BAD")
	if a < 0 || z < 0 || a > z {
		t.Errorf("want both keys, sorted: %q", s)
	}
}

func TestOtherKeysAllowed(t *testing.T) {
	tmp := t.TempDir()
	conf := "EXTRACT_ROOT=" + tmp + "\nEXTRACT_TREE=/etc\nEXTRACT_GIT_REPO_MODE=ALL\nGIT=/usr/bin/git\nMYHOOK_FOO=1\nFOO=2\nEXTRACTED=1\n"
	if _, err := Load(writeConf(t, tmp, conf)); err != nil {
		t.Fatal(err)
	}
}

func TestShippedDistKeysAllKnown(t *testing.T) {
	b, err := os.ReadFile("../../conf/extract.conf.dist")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^#?\s*((?:PRE_|POST_)?EXTRACT_[A-Z_]+)=`)
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			n++
			if !isKnownOrAllowed(m[1]) {
				t.Errorf("dist key %s is neither known nor allowed", m[1])
			}
		}
	}
	if n == 0 {
		t.Fatal("no keys found in dist")
	}
}

// TestKnownKeysMatchSource keeps knownKeys in step with the keys the loader
// reads: every EXTRACT_ literal in the non-test sources must be known, and
// every known key must be mentioned there.
func TestKnownKeysMatchSource(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`"((?:PRE_|POST_)?EXTRACT_[A-Z_]+)"`)
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keys.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			seen[m[1]] = true
			if !knownKeys[m[1]] {
				t.Errorf("%s reads %s, missing from knownKeys", f, m[1])
			}
		}
	}
	for k := range knownKeys {
		if !seen[k] {
			t.Errorf("knownKeys has %s, which no source file reads", k)
		}
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		d    int
	}{{"", "abc", 3}, {"abc", "abc", 0}, {"kitten", "sitting", 3}, {"EXTRACT_HOSTS_CMD", "EXTRACT_HOST_CMD", 1}} {
		if d := editDistance(c.a, c.b); d != c.d {
			t.Errorf("editDistance(%q,%q)=%d want %d", c.a, c.b, d, c.d)
		}
	}
}
