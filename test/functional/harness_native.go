//go:build functional && !functional_docker

package functional

import "testing"

// isDockerBuild is false for native binary builds.
const isDockerBuild = false

// DockerRunner is a compile-time stub so that resolveRunner (which references
// DockerRunner directly) compiles under the functional tag. Never instantiated
// at runtime — resolveRunner only creates it when isFunctionalDocker() is true.
type DockerRunner struct {
	ImageTag      string
	UpstreamDir   string
	DownstreamDir string
}

func (r *DockerRunner) Run(t *testing.T, _ []string, _ string) (string, int) {
	t.Helper()
	t.Fatal("DockerRunner must never be used in native builds")
	panic("unreachable")
}
