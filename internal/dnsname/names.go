package dnsname

import "strings"

// ProjectZone returns the project zone name, e.g. "autojoin-sandbox-measurement-lab-org".
func ProjectZone(project string) string {
	return "autojoin-" + strings.TrimPrefix(project, "mlab-") + "-measurement-lab-org"
}

// OrgZone returns the organization zone name based on the given organization and
// project, e.g. "autojoin-foo-sandbox-measurement-lab-org".
func OrgZone(org, project string) string {
	// NOTE: prefix prevents name collision with existing zones when the org is "mlab".
	return "autojoin-" + org + "-" + strings.TrimPrefix(project, "mlab-") + "-measurement-lab-org"
}

// OrgDNS returns the DNS name for the given org and project, e.g. "foo.autojoin.measurement-lab.org."
func OrgDNS(org, project string) string {
	return org + "." + strings.TrimPrefix(project, "mlab-") + ".measurement-lab.org."
}
