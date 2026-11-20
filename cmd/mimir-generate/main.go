// Command mimir-generate finds config files (*_config.json) under the current
// directory and writes a typed Go Config next to each as *_config_gen.go.
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dfryer1193/mjolnir/utils/log"

	"github.com/worldtree-industries/mimir/internal/generator"
)

const outputPerm = 0o644

func main() {
	lg := log.GetLogger("mimir-generate")

	configFiles, err := findConfigFiles(".")
	if err != nil {
		lg.Error("Failed to find config files", "error", err)
		os.Exit(1)
	}

	lg.Info("Found config files", "count", len(configFiles))

	for _, configPath := range configFiles {
		if err = processConfigFile(configPath); err != nil {
			lg.Error("Failed to process config file", "file", configPath, "error", err)
			continue
		}
		lg.Info("Generated config", "file", configPath)
	}
}

func findConfigFiles(root string) ([]string, error) {
	var configFiles []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && (info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), "_config.json") {
			configFiles = append(configFiles, path)
		}

		return nil
	})

	return configFiles, err
}

func processConfigFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	packageName, err := getPackageName(configPath)
	if err != nil {
		return fmt.Errorf("failed to determine package name: %w", err)
	}

	importPath, err := importPathFor(configPath)
	if err != nil {
		return fmt.Errorf("failed to determine import path: %w", err)
	}

	generated, err := generator.Generate(data, packageName, importPath)
	if err != nil {
		return err
	}

	// Confine the write to the working directory the config files were found in.
	root, err := os.OpenRoot(".")
	if err != nil {
		return fmt.Errorf("failed to open working directory: %w", err)
	}
	defer root.Close()

	outputPath := strings.TrimSuffix(configPath, ".json") + "_gen.go"
	if err = root.WriteFile(outputPath, generated, outputPerm); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

// getPackageName returns the package of the Go files next to configPath, or
// the directory's name when there are none.
func getPackageName(configPath string) (string, error) {
	dir := filepath.Dir(configPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}

		content, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}

		for line := range strings.SplitSeq(string(content), "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
				if fields := strings.Fields(name); len(fields) > 0 {
					return fields[0], nil
				}
			}
		}
	}

	if dir == "." {
		return "main", nil
	}

	return filepath.Base(dir), nil
}

// importPathFor returns the Go import path of the package holding configPath:
// the module path from the nearest go.mod above it plus the directory relative
// to that go.mod.
func importPathFor(configPath string) (string, error) {
	absDir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return "", err
	}

	for dir := absDir; ; dir = filepath.Dir(dir) {
		content, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		if readErr == nil {
			modulePath, ok := parseModulePath(string(content))
			if !ok {
				return "", fmt.Errorf("no module directive in %s", filepath.Join(dir, "go.mod"))
			}

			rel, relErr := filepath.Rel(dir, absDir)
			if relErr != nil {
				return "", relErr
			}

			return path.Join(modulePath, filepath.ToSlash(rel)), nil
		}

		if !os.IsNotExist(readErr) {
			return "", readErr
		}

		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("no go.mod found above %s", absDir)
		}
	}
}

func parseModulePath(goMod string) (string, bool) {
	for line := range strings.SplitSeq(goMod, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok {
			if fields := strings.Fields(rest); len(fields) > 0 {
				return strings.Trim(fields[0], "\"`"), true
			}
		}
	}

	return "", false
}
