// Package version exposes build identity injected by the release pipeline.
package version

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

func Current() Info {
	return Info{
		Version:   fallback(Version, "dev"),
		Commit:    fallback(Commit, "unknown"),
		BuildDate: fallback(BuildDate, "unknown"),
	}
}

func (info Info) String() string {
	return fmt.Sprintf(
		"bazUSOP hub %s (%s, %s)",
		fallback(info.Version, "dev"),
		fallback(info.Commit, "unknown"),
		fallback(info.BuildDate, "unknown"),
	)
}

func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
