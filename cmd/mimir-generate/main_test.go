package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportPathFor(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "// comment\nmodule example.com/acme/app\n\ngo 1.22\n")
	write("kafka/kafka_config.json", "{}")
	write("top_config.json", "{}")
	write("nested/mod/go.mod", "module \"example.com/other\"\n")
	write("nested/mod/deep/x_config.json", "{}")

	tests := []struct {
		name, file, want string
	}{
		{"subdirectory of the module", "kafka/kafka_config.json", "example.com/acme/app/kafka"},
		{"module root", "top_config.json", "example.com/acme/app"},
		{"nearest go.mod wins", "nested/mod/deep/x_config.json", "example.com/other/deep"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := importPathFor(filepath.Join(root, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("importPathFor = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("no go.mod", func(t *testing.T) {
		if _, err := importPathFor(filepath.Join(t.TempDir(), "x_config.json")); err == nil {
			t.Error("expected an error when no go.mod is found")
		}
	})
}
