package main

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/gavincarr/extract/internal/version"
)

func TestGrammar(t *testing.T) {
	const def = "/etc/extract/extract.conf"
	sync := func(s SyncCmd) SyncCmd {
		if s.Timeout == 0 {
			s.Timeout = 60
		}
		return s
	}
	cases := []struct {
		args    []string
		want    CLI
		wantErr bool
	}{
		// Bare and legacy (pre-subcommand) forms run sync by default.
		{nil, CLI{Config: def, Sync: sync(SyncCmd{})}, false},
		{[]string{"-vvv"}, CLI{Config: def, Verbose: 3, Sync: sync(SyncCmd{})}, false},
		{[]string{"-v", "-v"}, CLI{Config: def, Verbose: 2, Sync: sync(SyncCmd{})}, false},
		{[]string{"--verbose", "--verbose"}, CLI{Config: def, Verbose: 2, Sync: sync(SyncCmd{})}, false},
		{[]string{"-c", "x.conf", "-t", "5", "-n", "h1", "u@h2"},
			CLI{Config: "x.conf", Sync: SyncCmd{Timeout: 5, Noop: true, Hosts: []string{"h1", "u@h2"}}}, false},
		{[]string{"-t5"}, CLI{Config: def, Sync: SyncCmd{Timeout: 5}}, false},
		// Explicit sync, with global flags before or after it.
		{[]string{"sync"}, CLI{Config: def, Sync: sync(SyncCmd{})}, false},
		{[]string{"sync", "-vvv"}, CLI{Config: def, Verbose: 3, Sync: sync(SyncCmd{})}, false},
		{[]string{"sync", "-n", "h1"}, CLI{Config: def, Sync: sync(SyncCmd{Noop: true, Hosts: []string{"h1"}})}, false},
		{[]string{"-c", "x.conf", "sync", "h1"}, CLI{Config: "x.conf", Sync: sync(SyncCmd{Hosts: []string{"h1"}})}, false},
		{[]string{"sync", "-c", "x.conf", "-t", "0", "h1"}, CLI{Config: "x.conf", Sync: SyncCmd{Hosts: []string{"h1"}}}, false},
		// A word that isn't a subcommand is a host for the default sync.
		{[]string{"check"}, CLI{Config: def, Sync: sync(SyncCmd{Hosts: []string{"check"}})}, false},
		// A host named like a command needs the command spelled out.
		{[]string{"sync", "sync"}, CLI{Config: def, Sync: sync(SyncCmd{Hosts: []string{"sync"}})}, false},
		{[]string{"--bogus"}, CLI{}, true},
		{[]string{"-t", "abc"}, CLI{}, true},
		{[]string{"sync", "--bogus"}, CLI{}, true},
	}
	for _, c := range cases {
		var cli CLI
		p, err := newParser(&cli, io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err := p.Parse(c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("%v: err = %v, wantErr %v", c.args, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if !reflect.DeepEqual(cli, c.want) {
			t.Errorf("%v: got %+v, want %+v", c.args, cli, c.want)
		}
		if cmd := ctx.Command(); !strings.HasPrefix(cmd, "sync") {
			t.Errorf("%v: command = %q, want sync", c.args, cmd)
		}
	}
}

func TestSyncHelp(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"sync", "--help"}, &out, &errb); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	for _, s := range []string{"--timeout", "--noop", "--config"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("stdout missing %q: %q", s, out.String())
		}
	}
}

func TestUsageErrorGoesToStderr(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"--bogus"}, &out, &errb)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout not empty: %q", out.String())
	}
	for _, s := range []string{"--bogus", "Usage:"} {
		if !strings.Contains(errb.String(), s) {
			t.Errorf("stderr missing %q: %q", s, errb.String())
		}
	}
}

func TestHelpExitsZero(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--help"}, &out, &errb); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	for _, s := range []string{"--config", "sync"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("stdout missing %q: %q", s, out.String())
		}
	}
	if errb.Len() != 0 {
		t.Errorf("stderr not empty: %q", errb.String())
	}
}

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--version"}, &out, &errb); code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if want := "extract " + version.Version + "\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}
