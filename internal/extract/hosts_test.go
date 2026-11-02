package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gavincarr/extract/internal/config"
)

func TestParseHost(t *testing.T) {
	ok := map[string]Host{
		"h1":             {"", "h1"},
		"u@h1":           {"u", "h1"},
		"h1.example.com": {"", "h1.example.com"},
	}
	for in, want := range ok {
		got, err := ParseHost(in)
		if err != nil || got != want {
			t.Errorf("ParseHost(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "u@", "@h", "-h", "u@-h", "a/b", ".", "..", "u@h@x",
		"-o@h", "-oProxyCommand=x@h", "u:x@h", "u/x@h", "u x@h", "h:1", "h 1", "h\t1", "h\x01",
		".extract-state", ".x", "u@.x"} {
		if _, err := ParseHost(in); err == nil {
			t.Errorf("ParseHost(%q): expected error", in)
		}
	}
}

func TestParseHostLeadingDot(t *testing.T) {
	for _, in := range []string{".extract-state", ".x", "u@.x", ".", ".."} {
		_, err := ParseHost(in)
		want := fmt.Sprintf("invalid host %q: name must not start with '.'", in)
		if err == nil || err.Error() != want {
			t.Errorf("ParseHost(%q) error %v, want %q", in, err, want)
		}
	}
}

func TestHostRemoteString(t *testing.T) {
	if s := (Host{"u", "h"}).Remote(); s != "u@h" {
		t.Errorf("got %q", s)
	}
	if s := (Host{"", "h"}).String(); s != "h" {
		t.Errorf("got %q", s)
	}
}

func names(hs []Host) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.String())
	}
	return out
}

func TestResolveHostsPrecedence(t *testing.T) {
	cfg := &config.Config{Hosts: []string{"h1"}, HostCmd: "echo cmdhost"}
	hs, err := ResolveHosts(context.Background(), []string{"h9"}, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h9"}) {
		t.Fatalf("args: %v %v", hs, err)
	}
	cfg.Hosts = []string{"h1", "u@h2"}
	cfg.HostCmd = ""
	hs, err = ResolveHosts(context.Background(), nil, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h1", "u@h2"}) {
		t.Fatalf("cfg hosts: %v %v", hs, err)
	}
}

func TestResolveHostsMerge(t *testing.T) {
	cfg := &config.Config{
		Hosts:   []string{"h1", "u@h2", "h3"},
		HostCmd: `printf 'h4\nh1\nh5 h3\n'`,
	}
	hs, err := ResolveHosts(context.Background(), nil, cfg)
	want := []string{"h1", "u@h2", "h3", "h4", "h5"}
	if err != nil || !reflect.DeepEqual(names(hs), want) {
		t.Fatalf("got %v %v; want %v", names(hs), err, want)
	}
}

func TestResolveHostsCmdNotRunWhenArgsGiven(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	cfg := &config.Config{Hosts: []string{"h1"}, HostCmd: "touch " + marker + "; echo h2"}
	hs, err := ResolveHosts(context.Background(), []string{"h9"}, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h9"}) {
		t.Fatalf("got %v %v", hs, err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("HostCmd was run")
	}
}

func TestResolveHostsCmdProblemsFatalWithHosts(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{`true`, "host command returned no hosts"},
		{`printf ' \n\t'`, "host command returned no hosts"},
		{`echo boom >&2; exit 1`, "host command failed: boom"},
	}
	for _, c := range cases {
		hs, err := ResolveHosts(context.Background(), nil, &config.Config{Hosts: []string{"h1"}, HostCmd: c.cmd})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: hosts %v err %v; want contains %q", c.cmd, hs, err, c.want)
		}
	}
}

func TestResolveHostsFromCmd(t *testing.T) {
	cfg := &config.Config{HostCmd: `printf 'h1\nh2\r\n u@h3 '`}
	hs, err := ResolveHosts(context.Background(), nil, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h1", "h2", "u@h3"}) {
		t.Fatalf("got %v %v", hs, err)
	}
}

func TestResolveHostsCmdFailures(t *testing.T) {
	cases := []struct {
		cmd     string
		timeout time.Duration
		want    string
	}{
		{`echo boom >&2; exit 1`, 0, "boom"},
		{`true`, 0, "host command returned no hosts"},
		{`sleep 5`, 200 * time.Millisecond, "timed out"},
		{`echo 'ok -bad'`, 0, "-bad"},
	}
	for _, c := range cases {
		_, err := ResolveHosts(context.Background(), nil, &config.Config{HostCmd: c.cmd, HookTimeout: c.timeout})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v; want contains %q", c.cmd, err, c.want)
		}
	}
}

func TestResolveHostsNone(t *testing.T) {
	_, err := ResolveHosts(context.Background(), nil, &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "no hosts") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveHostsDuplicates(t *testing.T) {
	hs, err := ResolveHosts(context.Background(), []string{"h1", "h1"}, &config.Config{})
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h1"}) {
		t.Fatalf("got %v %v", hs, err)
	}
	if _, err := ResolveHosts(context.Background(), []string{"h1", "u@h1"}, &config.Config{}); err == nil {
		t.Fatal("expected error for conflicting entries")
	}
}

func TestResolveHostsCmdInterrupted(t *testing.T) {
	start := time.Now()
	_, err := ResolveHosts(cancelAfter(200*time.Millisecond), nil, &config.Config{HostCmd: "sleep 5"})
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took too long: %v", d)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "host command interrupted") {
		t.Errorf("err = %q", err)
	}
}
