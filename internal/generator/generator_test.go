package generator_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/worldtree-industries/mimir/internal/generator"
)

// TestGoldenCases generates Go from each testdata/<case>/config.json, compares
// it to the checked-in testdata/<case>/config_gen.go, and builds and tests that
// case's package, so generated code must compile and behave. Set
// UPDATE_GOLDEN=1 to rewrite the golden files.
func TestGoldenCases(t *testing.T) {
	cases, err := filepath.Glob("testdata/*/config.json")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no golden cases found: %v", err)
	}

	for _, configPath := range cases {
		dir := filepath.Dir(configPath)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			goldenPath := filepath.Join(dir, "config_gen.go")

			got := generate(t, configPath, filepath.Base(dir))
			if os.Getenv("UPDATE_GOLDEN") != "" {
				writeFile(t, goldenPath, got)
			}

			if want := readFile(t, goldenPath); string(got) != string(want) {
				t.Errorf("generated code differs from %s (rerun with UPDATE_GOLDEN=1 to accept)", goldenPath)
			}

			out, testErr := exec.CommandContext(t.Context(), "go", "test", "./"+dir).CombinedOutput()
			if testErr != nil {
				t.Errorf("go test %s: %v\n%s", dir, testErr, out)
			}
		})
	}
}

func generate(t *testing.T, configPath, packageName string) []byte {
	t.Helper()

	got, err := generator.Generate(readFile(t, configPath), packageName)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	return got
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
