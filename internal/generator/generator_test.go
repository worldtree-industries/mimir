package generator_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

const modulePath = "github.com/worldtree-industries/mimir"

func generate(t *testing.T, configPath, packageName string) []byte {
	t.Helper()

	importPath := modulePath + "/internal/generator/testdata/" + packageName

	got, err := generator.Generate(readFile(t, configPath), packageName, importPath)
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

func TestDuplicateItemNamesAreRejected(t *testing.T) {
	input := []byte(`{"type_name":"DupConfig","items":[
		{"name":"a","description":"","type":"int","value":"1"},
		{"name":"b","description":"","type":"int","value":"1"},
		{"name":"a","description":"","type":"string","value":"x"}]}`)

	got, err := generator.Generate(input, "dup", "example.com/dup")
	if err == nil {
		t.Fatal("expected an error for duplicate Item names")
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("error should name the Item: %v", err)
	}
	if got != nil {
		t.Errorf("no output should be produced, got %d bytes", len(got))
	}
}

func TestOldNamespaceKeyStillNamesTheType(t *testing.T) {
	items := `"items":[{"name":"a","description":"","type":"int","value":"1"}]`
	current, err := generator.Generate([]byte(`{"type_name":"KeyConfig",`+items+`}`), "key", "example.com/key")
	if err != nil {
		t.Fatalf("Generate with type_name: %v", err)
	}
	old, err := generator.Generate([]byte(`{"namespace":"KeyConfig",`+items+`}`), "key", "example.com/key")
	if err != nil {
		t.Fatalf("Generate with the old namespace key: %v", err)
	}
	if !bytes.Equal(old, current) {
		t.Errorf("the old namespace key generated different code than type_name:\n%s", old)
	}
}
