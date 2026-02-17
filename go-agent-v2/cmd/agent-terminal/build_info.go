package main

import (
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    = ""
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	Runtime   string `json:"runtime"`
}

func readGoBuildVCS() (revision, vcsTime string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", "", false
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.time":
			vcsTime = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value) == "true"
		}
	}
	return revision, vcsTime, modified
}

func shortCommit(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) > 12 {
		return revision[:12]
	}
	return revision
}

func currentBuildInfo() BuildInfo {
	vcsRevision, vcsTime, vcsModified := readGoBuildVCS()

	version := strings.TrimSpace(buildVersion)
	if version == "" || version == "dev" {
		if vcsRevision != "" {
			version = "dev+" + shortCommit(vcsRevision)
			if vcsModified {
				version += "-dirty"
			}
		} else {
			version = "dev"
		}
	}

	commit := strings.TrimSpace(buildCommit)
	if commit == "" || commit == "unknown" {
		if vcsRevision != "" {
			commit = shortCommit(vcsRevision)
			if vcsModified {
				commit += "-dirty"
			}
		} else {
			commit = "unknown"
		}
	}

	formattedBuildTime := strings.TrimSpace(buildTime)
	if formattedBuildTime == "" || formattedBuildTime == "unknown" {
		formattedBuildTime = vcsTime
	}
	if formattedBuildTime == "" {
		formattedBuildTime = "unknown"
	} else if t, err := time.Parse(time.RFC3339, formattedBuildTime); err == nil {
		formattedBuildTime = t.Local().Format("2006-01-02 15:04:05 MST")
	}

	return BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: formattedBuildTime,
		Runtime:   runtime.GOOS + "/" + runtime.GOARCH,
	}
}
