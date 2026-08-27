package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string
	Commit    string
	BuildDate string
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate}
}

func (info Info) String(product string) string {
	return fmt.Sprintf("%s version=%s commit=%s build_date=%s", product, info.Version, info.Commit, info.BuildDate)
}
