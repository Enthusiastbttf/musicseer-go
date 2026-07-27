// Package deploy holds no code — these tests guard the shipped deployment
// assets, which are otherwise only exercised by running them on a Proxmox host.
package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Security review item 15. The unit the LXC provisioner actually installs had
// drifted weaker than the reference unit in this repo: it was missing
// ProtectKernelTunables, ProtectControlGroups and RestrictSUIDSGID, so the
// hardening documented in the repo was not the hardening running on the box.
//
// Making them identical once does not stop it happening again, so assert it.
func TestLXCUnitMatchesReference(t *testing.T) {
	ref, err := os.ReadFile("systemd/musicseer.service")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("proxmox-create-lxc.sh")
	if err != nil {
		t.Fatal(err)
	}

	re := regexp.MustCompile(`(?s)cat > /etc/systemd/system/musicseer\.service <<"EOF"\n(.*?)\nEOF\n`)
	m := re.FindSubmatch(script)
	if m == nil {
		t.Fatal("no systemd unit heredoc found in proxmox-create-lxc.sh")
	}

	got, want := string(m[1]), strings.TrimRight(string(ref), "\n")
	if got == want {
		return
	}
	// Report the drift as directive sets, which is what actually matters and is
	// far easier to read than a whole-file diff.
	only := func(a, b string) []string {
		in := map[string]bool{}
		for _, l := range strings.Split(b, "\n") {
			in[strings.TrimSpace(l)] = true
		}
		var out []string
		for _, l := range strings.Split(a, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") || in[l] {
				continue
			}
			out = append(out, l)
		}
		return out
	}
	t.Errorf("the unit installed by proxmox-create-lxc.sh has drifted from systemd/musicseer.service\n"+
		"  only in the script:    %v\n"+
		"  only in the reference: %v",
		only(got, want), only(want, got))
}

// The heredoc is nested inside a single-quoted `pct exec ... bash -c '...'`
// string, so an apostrophe anywhere in the unit silently truncates the command
// and the container ends up with a half-written service file. Cheap to check,
// and the failure mode is very hard to spot by eye.
func TestLXCUnitHasNoApostrophes(t *testing.T) {
	ref, err := os.ReadFile("systemd/musicseer.service")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(ref), "\n") {
		if strings.Contains(line, "'") {
			t.Errorf("systemd/musicseer.service:%d contains an apostrophe, which would break the "+
				"single-quoted heredoc in proxmox-create-lxc.sh: %s", i+1, line)
		}
	}
}

// The provisioner must not install a binary it has not verified unless the
// operator explicitly opts out. Guards against the fail-open branch being
// reintroduced by a later edit.
func TestLXCInstallerVerifiesDownload(t *testing.T) {
	script, err := os.ReadFile("proxmox-create-lxc.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	for _, want := range []string{
		"sha256sum -c -",           // the verification itself
		"checksums.txt",            // sourced from the published digest
		"--insecure-skip-checksum", // the only way past it, and it is explicit
		"refusing to install",      // and a mismatch aborts
	} {
		if !strings.Contains(s, want) {
			t.Errorf("proxmox-create-lxc.sh no longer contains %q — download verification may have regressed", want)
		}
	}
	if strings.Contains(s, "/releases/latest/download/musicseer-linux-amd64\"\nCHECKSUM_URL=\"\"") {
		t.Error("latest-download path must still resolve a checksum URL")
	}
}
