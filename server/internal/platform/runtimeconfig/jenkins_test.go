package runtimeconfig

import (
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
	"testing"
)

func TestJenkinsConfigurationDisabledOrFailsClosed(t *testing.T) {
	calls := 0
	read := func(string, int64) ([]byte, error) { calls++; return nil, errors.New("sensitive path or contents") }
	sources, e := JenkinsSources(func(string) string { return "" }, read)
	if e != nil || len(sources) != 0 || calls != 0 {
		t.Fatal(sources, e, calls)
	}
	_, e = JenkinsSources(func(name string) string {
		if name != "RADISHNEXUS_JENKINS_SOURCES_FILE" {
			t.Fatal(name)
		}
		return "/synthetic/missing"
	}, read)
	if e != jenkins.ErrConfiguration || calls != 1 {
		t.Fatal(e, calls)
	}
}
