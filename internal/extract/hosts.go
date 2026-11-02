// Package extract implements the extract run: host resolution, rsync
// invocation and per-host orchestration.
package extract

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gavincarr/extract/internal/config"
	"github.com/gavincarr/extract/internal/proc"
)

// Host is a remote host, optionally with an ssh user.
type Host struct{ User, Name string }

// Remote returns "user@name", or "name" when User is empty.
func (h Host) Remote() string {
	if h.User == "" {
		return h.Name
	}
	return h.User + "@" + h.Name
}

func (h Host) String() string { return h.Remote() }

// ParseHost parses "[user@]name", rejecting names that are unsafe as an
// ssh/rsync argument or as a local directory name.
func ParseHost(s string) (Host, error) {
	var h Host
	if user, name, ok := strings.Cut(s, "@"); ok {
		if user == "" {
			return Host{}, fmt.Errorf("invalid host %q: empty user", s)
		}
		if strings.HasPrefix(user, "-") || strings.ContainsAny(user, ":/") || hasSpaceOrControl(user) {
			return Host{}, fmt.Errorf("invalid host %q: bad user", s)
		}
		h = Host{User: user, Name: name}
	} else {
		h = Host{Name: s}
	}
	switch {
	case h.Name == "":
		return Host{}, fmt.Errorf("invalid host %q: empty host name", s)
	case strings.HasPrefix(h.Name, "-"):
		return Host{}, fmt.Errorf("invalid host %q: name must not start with '-'", s)
	case strings.ContainsAny(h.Name, "/@:") || hasSpaceOrControl(h.Name):
		return Host{}, fmt.Errorf("invalid host %q: name must not contain '/', '@', ':', whitespace or control characters", s)
	case h.Name == "." || h.Name == "..":
		return Host{}, fmt.Errorf("invalid host %q: bad host name", s)
	}
	return h, nil
}

// ResolveHosts picks the host list: command-line args, else cfg.Hosts, else
// the output of cfg.HostCmd. The result is validated and de-duplicated.
func ResolveHosts(args []string, cfg *config.Config) ([]Host, error) {
	var raw []string
	switch {
	case len(args) > 0:
		raw = args
	case len(cfg.Hosts) > 0:
		raw = cfg.Hosts
	case cfg.HostCmd != "":
		res := proc.Run([]string{"sh", "-c", cfg.HostCmd}, "", cfg.HookTimeout)
		switch {
		case res.TimedOut:
			return nil, fmt.Errorf("host command timed out after %s", cfg.HookTimeout)
		case res.Failed():
			return nil, fmt.Errorf("host command failed: %s", failDetail(res))
		}
		raw = strings.Fields(res.Stdout)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("no hosts specified (arguments, HOSTS or HOST_CMD)")
	}

	var hosts []Host
	byName := map[string]Host{}
	for _, s := range raw {
		h, err := ParseHost(s)
		if err != nil {
			return nil, err
		}
		if prev, ok := byName[h.Name]; ok {
			if prev != h {
				return nil, fmt.Errorf("hosts %q and %q share the same host name", prev, h)
			}
			continue
		}
		byName[h.Name] = h
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func hasSpaceOrControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
}

func failDetail(res proc.StepResult) string {
	if msg := strings.TrimSpace(res.Stderr); msg != "" {
		return msg
	}
	if res.Err != nil {
		return res.Err.Error()
	}
	return fmt.Sprintf("exit status %d", res.ExitCode)
}
