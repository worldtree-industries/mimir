package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dfryer1193/mjolnir/utils/environment"
)

// localConfigFile is where Local Values are set, relative to the root of the
// repository the Deployable runs from.
const localConfigFile = ".mimir/config"

// localValues are the Local Values read from the local config file: for each
// Config, each Item's value by Item name. found is whether there was a file
// to read at all.
type localValues struct {
	found  bool
	path   string
	values map[string]map[string]string
}

// localLine is one line of the local config file: one Local Value.
type localLine struct {
	Config string  `json:"config"`
	Item   string  `json:"item"`
	Value  *string `json:"value"`
}

var errLocalLine = errors.New(`want {"config": "...", "item": "...", "value": "..."}`)

// readLocalValues reads the local config file at the root of the repository
// workDir is in, when the process may use Local Values: outside a checkout
// of a repository, or in staging, prod or an Environment it doesn't know, it
// reads nothing. A missing file is no Local Values; a file or line that
// can't be read is logged, and the rest still apply.
func readLocalValues(ctx context.Context, logger *slog.Logger, getenv func(string) string, workDir string) localValues {
	if !localValuesAllowed(getenv) {
		return localValues{}
	}
	root, ok := repositoryRoot(workDir)
	if !ok {
		return localValues{}
	}

	path := filepath.Join(root, localConfigFile)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return localValues{}
	case err != nil:
		logger.ErrorContext(ctx, "Can't read the local config file; using no Local Values", "path", path, "error", err)
		return localValues{found: true, path: path}
	}

	return parseLocalValues(ctx, logger.With("path", path), path, data)
}

// localValuesAllowed is whether the Environment lets the process use Local
// Values: only when MIMIR_ENVIRONMENT is unset or dev.
func localValuesAllowed(getenv func(string) string) bool {
	raw := getenv(environmentVar)

	return raw == "" || raw == environment.Dev.String()
}

// repositoryRoot is the nearest directory, from dir upwards, holding .git (a
// directory, or a file in a worktree).
func repositoryRoot(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err = os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// parseLocalValues reads the JSONL of the local config file, one Local Value
// per line. Blank lines are skipped; a bad line is logged and skipped; when
// an Item is set twice, the last line wins.
func parseLocalValues(ctx context.Context, logger *slog.Logger, path string, data []byte) localValues {
	local := localValues{found: true, path: path, values: map[string]map[string]string{}}

	n := 0
	for raw := range bytes.Lines(data) {
		n++
		text := bytes.TrimSpace(raw)
		if len(text) == 0 {
			continue
		}
		var line localLine
		err := json.Unmarshal(text, &line)
		if err == nil && (line.Config == "" || line.Item == "" || line.Value == nil) {
			err = errLocalLine
		}
		if err != nil {
			logger.ErrorContext(ctx, "Skipping a bad line of the local config file", "line", n, "error", err)
			continue
		}

		items := local.values[line.Config]
		if items == nil {
			items = map[string]string{}
			local.values[line.Config] = items
		}
		if _, dup := items[line.Item]; dup {
			logger.WarnContext(ctx, "Item set twice in the local config file; the last line wins",
				"line", n, "config", line.Config, "item", line.Item)
		}
		items[line.Item] = *line.Value
	}

	return local
}

// takeLocalValues gives the Config the Local Values set for it, each of
// which then replaces both its start value and any Override. One that
// doesn't parse is logged and the start value kept; one for an Item the
// Config doesn't declare is logged. The Local Values taken are logged
// together, since they hide any Override.
func (p *configPoll) takeLocalValues(ctx context.Context, local localValues) {
	values := local.values[p.def.Config]
	if len(values) == 0 {
		return
	}

	declared := make(map[string]bool, len(p.def.Items))
	var taken []any
	for _, item := range p.def.Items {
		declared[item.Name] = true
		value, ok := values[item.Name]
		if !ok {
			continue
		}
		if err := item.Set(value); err != nil {
			p.logger.ErrorContext(ctx, "Ignoring a bad Local Value; keeping the start value",
				"path", local.path, "item", item.Name, "type", item.Type, "value", value, "error", err)
			continue
		}
		key := item.ref()
		p.start[key] = value
		p.local[key] = true
		taken = append(taken, slog.String(item.Name, value))
	}

	for name := range values {
		if !declared[name] {
			p.logger.WarnContext(ctx, "The local config file sets an Item this Config doesn't declare",
				"path", local.path, "item", name)
		}
	}

	if len(taken) > 0 {
		p.logger.WarnContext(ctx, "Using Local Values, which replace any Override",
			"path", local.path, slog.Group("values", taken...))
	}
}
