package extract

import "github.com/gavincarr/extract/internal/config"

// RsyncArgs builds the full rsync argv for pulling from host h into a local
// directory named after the host. filesFrom is the files list for this host
// (see HostResult.FilesFrom); it is ignored in ModeTree. rsync is verbose
// only at verbosity >= 3; lower levels are consumed by extract's own logging.
func RsyncArgs(cfg *config.Config, h Host, filesFrom string, noop bool, verbosity int) []string {
	argv := []string{"rsync", "--archive", "--copy-links", "--delete", "--rsh=ssh -o BatchMode=yes"}
	if verbosity >= 3 {
		argv = append(argv, "--verbose")
	} else {
		argv = append(argv, "--quiet")
	}
	if noop {
		argv = append(argv, "--dry-run")
	}
	src := cfg.Source
	switch cfg.Mode {
	case config.ModeFilesRemote:
		argv = append(argv, "--files-from=:"+filesFrom)
		src = "/"
	case config.ModeFilesLocal:
		argv = append(argv, "--files-from="+filesFrom)
		src = "/"
	}
	return append(argv, h.Remote()+":"+src, h.Name)
}
