package app

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

func CurrentBuild() BuildInfo {
	return BuildInfo{Version: Version, Commit: Commit, BuildDate: BuildDate}
}

func (info BuildInfo) String() string {
	return fmt.Sprintf("skyfeed version=%s commit=%s build_date=%s", info.Version, info.Commit, info.BuildDate)
}
