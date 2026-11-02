package extract

import (
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
		"-o@h", "-oProxyCommand=x@h", "u:x@h", "u/x@h", "u x@h", "h:1", "h 1", "h\t1", "h\x01"} {
		if _, err := ParseHost(in); err == nil {
			t.Errorf("ParseHost(%q): expected error", in)
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
	hs, err := ResolveHosts([]string{"h9"}, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h9"}) {
		t.Fatalf("args: %v %v", hs, err)
	}
	cfg.Hosts = []string{"h1", "u@h2"}
	hs, err = ResolveHosts(nil, cfg)
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h1", "u@h2"}) {
		t.Fatalf("cfg hosts: %v %v", hs, err)
	}
}

func TestResolveHostsFromCmd(t *testing.T) {
	cfg := &config.Config{HostCmd: `printf 'h1\nh2\r\n u@h3 '`}
	hs, err := ResolveHosts(nil, cfg)
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
		{`true`, 0, "no hosts"},
		{`sleep 5`, 200 * time.Millisecond, "timed out"},
		{`echo 'ok -bad'`, 0, "-bad"},
	}
	for _, c := range cases {
		_, err := ResolveHosts(nil, &config.Config{HostCmd: c.cmd, HookTimeout: c.timeout})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: err = %v; want contains %q", c.cmd, err, c.want)
		}
	}
}

func TestResolveHostsNone(t *testing.T) {
	_, err := ResolveHosts(nil, &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "no hosts") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveHostsDuplicates(t *testing.T) {
	hs, err := ResolveHosts([]string{"h1", "h1"}, &config.Config{})
	if err != nil || !reflect.DeepEqual(names(hs), []string{"h1"}) {
		t.Fatalf("got %v %v", hs, err)
	}
	if _, err := ResolveHosts([]string{"h1", "u@h1"}, &config.Config{}); err == nil {
		t.Fatal("expected error for conflicting entries")
	}
}
