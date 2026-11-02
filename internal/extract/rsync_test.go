package extract

import (
	"reflect"
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
			got := RsyncArgs(&config.Config{Mode: c.mode, Source: c.src}, c.host, c.noop, c.v)
			want := append(append([]string{}, base...), c.tail...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %q\nwant %q", got, want)
			}
		})
	}
}
