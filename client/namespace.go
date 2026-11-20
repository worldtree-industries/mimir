package client

import (
	"errors"
	"path"
	"runtime/debug"
	"strings"

	"github.com/worldtree-industries/mimir/models"
)

var (
	errNoEnvironment = errors.New("MIMIR_ENVIRONMENT is not set")
	errNoDeployable  = errors.New("can't derive the Deployable name from build info; set MIMIR_DEPLOYABLE")
)

// discoverNamespace determines the Namespace this program reads config in:
// the Deployable is MIMIR_DEPLOYABLE, or else derived from buildPath, and the
// Environment is MIMIR_ENVIRONMENT, taken as-is.
func discoverNamespace(config string, getenv func(string) string, buildPath string) (models.Namespace, error) {
	ns := models.Namespace{
		Config:      config,
		Deployable:  getenv("MIMIR_DEPLOYABLE"),
		Environment: getenv("MIMIR_ENVIRONMENT"),
	}
	if ns.Deployable == "" {
		ns.Deployable = deployableFromBuildPath(buildPath)
	}

	switch {
	case ns.Environment == "":
		return models.Namespace{}, errNoEnvironment
	case ns.Deployable == "":
		return models.Namespace{}, errNoDeployable
	}

	return ns, nil
}

// deployableFromBuildPath derives a Deployable name from a main package path:
// "<name>" in ".../cmd/<name>", or else the path's last element.
func deployableFromBuildPath(buildPath string) string {
	const cmdDir = "/cmd/"
	if _, after, found := strings.CutLast(buildPath, cmdDir); found {
		name, _, _ := strings.Cut(after, "/")
		return name
	}
	if buildPath == "" || buildPath == "command-line-arguments" {
		return ""
	}

	return path.Base(buildPath)
}

// mainPackagePath is the running binary's main package path, from its build
// info, or empty if there is none.
func mainPackagePath() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return info.Path
}
