package client

import (
	"errors"
	"fmt"
	"path"
	"runtime/debug"

	"github.com/dfryer1193/mjolnir/utils/environment"

	"github.com/worldtree-industries/mimir/models"
)

const environmentVar = "MIMIR_ENVIRONMENT"

var (
	errNoDeployable = errors.New("can't derive the Deployable name from build info; set MIMIR_DEPLOYABLE")
)

// discoverNamespace determines the Namespace this program reads config in:
// the Deployable is MIMIR_DEPLOYABLE, or else derived from buildPath, and the
// Environment is MIMIR_ENVIRONMENT, taken as-is.
func discoverNamespace(config string, getenv func(string) string, buildPath string) (models.Namespace, error) {
	// MIMIR_ENVIRONMENT is read through the getenv option, so the client
	// checks it with mjolnir's Parse rather than FromEnv: a missing or
	// unknown Environment is never guessed.
	raw := getenv(environmentVar)
	if raw == "" {
		return models.Namespace{}, fmt.Errorf("%s is not set: %w", environmentVar, environment.ErrMissing)
	}
	env, err := environment.Parse(raw)
	if err != nil {
		return models.Namespace{}, fmt.Errorf("%s: %w", environmentVar, err)
	}

	ns := models.Namespace{
		Config:      config,
		Deployable:  getenv("MIMIR_DEPLOYABLE"),
		Environment: env.String(),
	}
	if ns.Deployable == "" {
		ns.Deployable = deployableFromBuildPath(buildPath)
	}

	if ns.Deployable == "" {
		return models.Namespace{}, errNoDeployable
	}

	return ns, nil
}

// deployableFromBuildPath derives a Deployable name from a main package
// path: its last element, which is <name> for an Ivaldi binary in cmd/<name>.
// A binary built from files ("go run main.go") has no usable path.
func deployableFromBuildPath(buildPath string) string {
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
