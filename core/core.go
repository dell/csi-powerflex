//go:generate go run semver/semver.go -f semver.tpl -o core_generated.go

package core

import "time"

var (
	// SemVer is the semantic version.
	SemVer = "unknown"

	// CommitSha7 is the short version of the commit hash from which
	// this program was built.
	CommitSha7 string

	// CommitSha32 is the long version of the commit hash from which
	// this program was built.
	CommitSha32 string

	// CommitTime is the commit timestamp of the commit from which
	// this program was built.
	CommitTime time.Time
)
