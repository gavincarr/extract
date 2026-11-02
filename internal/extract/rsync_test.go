package extract

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gavincarr/extract/internal/config"
)

func TestRsyncArgsGolden(t *testing.T) {
	base := []string{"rsync", "--archive", "--copy-links", "--delete", "--rsh=ssh -o BatchMode=yes"}
	cases := []struct {
		name string
		mode config.SourceMode
		src  string
		host Host
		noop bool
		v    int
		tail []string
	}{
		{"tree", config.ModeTree, "/etc", Host{"", "h1"}, false, 0, []string{"--quiet", "h1:/etc", "h1"}},
		{"tree-user-noop-vvv", config.ModeTree, "/etc", Host{"u", "h1"}, true, 3, []string{"--verbose", "--dry-run", "u@h1:/etc", "h1"}},
		{"tree-vv-still-quiet", config.ModeTree, "/etc", Host{"", "h1"}, false, 2, []string{"--quiet", "h1:/etc", "h1"}},
		{"tree-trailing-slash-kept", config.ModeTree, "/etc/", Host{"", "h1"}, false, 0, []string{"--quiet", "h1:/etc/", "h1"}},
		{"remote", config.ModeFilesRemote, "/var/cache/x.txt", Host{"", "h1"}, false, 0, []string{"--quiet", "--files-from=:/var/cache/x.txt", "h1:/", "h1"}},
		{"local-user-noop", config.ModeFilesLocal, "/etc/extract/synclist.txt", Host{"u", "h1"}, true, 1, []string{"--quiet", "--dry-run", "--files-from=/etc/extract/synclist.txt", "u@h1:/", "h1"}},
		{"remote-user-noop-vvv", config.ModeFilesRemote, "/var/cache/x.txt", Host{"u", "h1"}, true, 3, []string{"--verbose", "--dry-run", "--files-from=:/var/cache/x.txt", "u@h1:/", "h1"}},
		{"local-vvv", config.ModeFilesLocal, "/etc/extract/synclist.txt", Host{"", "h1"}, false, 3, []string{"--verbose", "--files-from=/etc/extract/synclist.txt", "h1:/", "h1"}},
		{"remote-vv-quiet", config.ModeFilesRemote, "/p", Host{"", "h1"}, false, 2, []string{"--quiet", "--files-from=:/p", "h1:/", "h1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{Mode: c.mode, Source: c.src}
			got := RsyncArgs(cfg, c.host, cfg.FilesArg(), c.noop, c.v)
			want := append(append([]string{}, base...), c.tail...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %q\nwant %q", got, want)
			}
		})
	}
}

func TestRsyncArgsChosenFilesList(t *testing.T) {
	cfg := &config.Config{Mode: config.ModeFilesRemote, Source: "/var/rpm.txt /var/deb.txt"}
	got := RsyncArgs(cfg, Host{"u", "h1"}, "/var/deb.txt", false, 0)
	want := []string{"rsync", "--archive", "--copy-links", "--delete", "--rsh=ssh -o BatchMode=yes",
		"--quiet", "--files-from=:/var/deb.txt", "u@h1:/", "h1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestProbeArgs(t *testing.T) {
	got := ProbeArgs(Host{"u", "h1"}, []string{"/var/rpm.txt", "/var/deb list's", "~/x"})
	want := []string{"ssh", "-o", "BatchMode=yes", "u@h1",
		`for f in /var/rpm.txt '/var/deb list'\''s' '~/x'; do if test -s "$f"; then printf "%s\n" "$f"; exit 0; fi; done; exit 3`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if strings.Contains(got[4], "!") || strings.Contains(got[4], "\n") {
		t.Errorf("script must be one line without '!': %q", got[4])
	}
}
