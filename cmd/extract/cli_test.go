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
	cases := []struct {
		args    []string
		want    CLI
		wantErr bool
	}{
		{nil, CLI{Config: def, Timeout: 60}, false},
		{[]string{"-vvv"}, CLI{Config: def, Timeout: 60, Verbose: 3}, false},
		{[]string{"-v", "-v"}, CLI{Config: def, Timeout: 60, Verbose: 2}, false},
		{[]string{"--verbose", "--verbose"}, CLI{Config: def, Timeout: 60, Verbose: 2}, false},
		{[]string{"-c", "x.conf", "-t", "5", "-n", "h1", "u@h2"},
			CLI{Config: "x.conf", Timeout: 5, Noop: true, Hosts: []string{"h1", "u@h2"}}, false},
		{[]string{"-t5"}, CLI{Config: def, Timeout: 5}, false},
		{[]string{"--bogus"}, CLI{}, true},
		{[]string{"-t", "abc"}, CLI{}, true},
	}
	for _, c := range cases {
		var cli CLI
		p, err := newParser(&cli, io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Parse(c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("%v: err = %v, wantErr %v", c.args, err, c.wantErr)
			continue
		}
		if !c.wantErr && !reflect.DeepEqual(cli, c.want) {
			t.Errorf("%v: got %+v, want %+v", c.args, cli, c.want)
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
	if !strings.Contains(out.String(), "--config") {
		t.Errorf("stdout missing --config: %q", out.String())
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
