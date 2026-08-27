package app

import "github.com/j4v3l/SkyFeed/internal/buildinfo"

type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

func CurrentBuild() BuildInfo {
	current := buildinfo.Current()
	return BuildInfo{Version: current.Version, Commit: current.Commit, BuildDate: current.BuildDate}
}

func (info BuildInfo) String() string {
	return buildinfo.Info(info).String("skyfeed")
}
