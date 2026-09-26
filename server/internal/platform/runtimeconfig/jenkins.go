package runtimeconfig

import (
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

// JenkinsSources is opt-in; malformed configured input never disables auth.
func JenkinsSources(getenv func(string) string, read jenkins.FileReader) ([]jenkins.Source, error) {
	path := getenv("RADISHNEXUS_JENKINS_SOURCES_FILE")
	if path == "" {
		return nil, nil
	}
	body, err := read(path, jenkins.MaxConfig)
	if err != nil {
		return nil, jenkins.ErrConfiguration
	}
	return jenkins.LoadSources(body, read)
}
