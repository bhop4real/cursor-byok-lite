package buildinfo

import "strings"

const (
	OriginalReleaseRepo = "leookun/cursor-byok"
	ModifiedReleaseRepo = "bhop4real/cursor-byok-lite"

	OriginalUpdateBaseURL = "https://github.com/" + OriginalReleaseRepo + "/releases/latest/download/"
	ModifiedUpdateBaseURL = "https://github.com/" + ModifiedReleaseRepo + "/releases/latest/download/"

	ReleaseRepo    = ModifiedReleaseRepo
	UpdateBaseURL  = ModifiedUpdateBaseURL
	ReleasePageURL = "https://github.com/" + ModifiedReleaseRepo + "/releases"
)

// Version is injected at build time from build/config.yml; direct Go builds use the current release.
var Version = "0.0.40.1-lite"

func CurrentVersion() string {
	version := strings.TrimSpace(strings.TrimPrefix(Version, "v"))
	if version == "" {
		return "0.0.0"
	}
	return version
}

func ReleaseTag() string {
	return "v" + CurrentVersion()
}
