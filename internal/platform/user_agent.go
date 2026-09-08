package platform

import "strings"

const userAgentProduct = "Nahida Desktop"

const devVersionSuffix = "-dev"

// UserAgent is the default product identity for application HTTP requests.
// Unpackaged builds append -dev so servers can tell development traffic from
// a release of the same line. AppVersion itself stays unchanged for updater
// comparison, GetAppStatus, and the local HTTP bridge.
func UserAgent() string {
	version := AppVersion
	if !Packaged() && !strings.HasSuffix(version, devVersionSuffix) {
		version += devVersionSuffix
	}
	return userAgentProduct + "/" + version
}
