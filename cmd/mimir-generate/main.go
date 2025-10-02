package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dfryer1193/mimir/api/config"
	"github.com/rs/zerolog/log"
)

type ConfigItem struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Type        config.Type `json:"type"`
	Value       string      `json:"value"`
}

type ConfigFile struct {
	Namespace string       `json:"namespace"`
	Items     []ConfigItem `json:"items"`
}

type MimirSettings struct {
	ConfigServerURL string `json:"config_server_url"`
}

type TemplateData struct {
	PackageName     string
	Namespace       string
	ConfigTypeName  string
	Items           []ConfigItem
	ConfigServerURL string
	NeedsSet        bool
	NeedsJSON       bool
}

func main() {
	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("failed to generate configs")
	}
}

func run() error {
	mimirSettings, err := loadMimirSettings()
	if err != nil {
		return fmt.Errorf("failed to load .mimir.json: %w", err)
	}

	configFiles, err := findConfigFiles(".")
	if err != nil {
		return fmt.Errorf("failed to find config files: %w", err)
	}

	log.Info().Int("count", len(configFiles)).Msg("found config files")

	for _, configPath := range configFiles {
		if err := processConfigFile(configPath, mimirSettings); err != nil {
			log.Err(err).Str("file", configPath).Msg("failed to process config file")
			continue
		}
		log.Info().Str("file", configPath).Msg("generated config")
	}

	return nil
}

func loadMimirSettings() (*MimirSettings, error) {
	data, err := os.ReadFile(".mimir.json")
	if err != nil {
		return nil, err
	}

	var settings MimirSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return &settings, nil
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

func processConfigFile(configPath string, settings *MimirSettings) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	packageName, err := getPackageName(configPath)
	if err != nil {
		return fmt.Errorf("failed to determine package name: %w", err)
	}

	outputPath := strings.TrimSuffix(configPath, ".json") + "_gen.go"

	if err := generateConfigFile(outputPath, packageName, &cfg, settings); err != nil {
		return fmt.Errorf("failed to generate config file: %w", err)
	}

	return nil
}

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

		filePath := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "package ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1], nil
				}
			}
		}
	}

	if dir == "." {
		return "main", nil
	}

	return filepath.Base(dir), nil
}

func generateConfigFile(outputPath, packageName string, cfg *ConfigFile, settings *MimirSettings) error {
	needsSet := false
	needsJSON := false
	for _, item := range cfg.Items {
		if item.Type == config.TypeStringSet || item.Type == config.TypeIntSet ||
			item.Type == config.TypeBoolSet || item.Type == config.TypeFloatSet {
			needsSet = true
		}
		if item.Type == config.TypeStringArray || item.Type == config.TypeIntArray ||
			item.Type == config.TypeBoolArray || item.Type == config.TypeFloatArray ||
			item.Type == config.TypeStringSet || item.Type == config.TypeIntSet ||
			item.Type == config.TypeBoolSet || item.Type == config.TypeFloatSet {
			needsJSON = true
		}
	}

	data := TemplateData{
		PackageName:     packageName,
		Namespace:       cfg.Namespace,
		ConfigTypeName:  cfg.Namespace,
		Items:           cfg.Items,
		ConfigServerURL: settings.ConfigServerURL,
		NeedsSet:        needsSet,
		NeedsJSON:       needsJSON,
	}

	tmpl, err := template.New("config").Funcs(template.FuncMap{
		"goType":         goType,
		"configType":     configType,
		"setterName":     setterName,
		"getterName":     getterName,
		"parseFunc":      parseFunc,
		"capitalize":     capitalize,
		"needsUnmarshal": needsUnmarshal,
		"escapeValue":    escapeValue,
	}).Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to format generated code: %w\n%s", err, buf.String())
	}

	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

func goType(t config.Type) string {
	switch t {
	case config.TypeString:
		return "string"
	case config.TypeInt:
		return "int64"
	case config.TypeBool:
		return "bool"
	case config.TypeFloat:
		return "float64"
	case config.TypeStringArray:
		return "[]string"
	case config.TypeIntArray:
		return "[]int64"
	case config.TypeBoolArray:
		return "[]bool"
	case config.TypeFloatArray:
		return "[]float64"
	case config.TypeStringSet:
		return "set.Set[string]"
	case config.TypeIntSet:
		return "set.Set[int64]"
	case config.TypeBoolSet:
		return "set.Set[bool]"
	case config.TypeFloatSet:
		return "set.Set[float64]"
	default:
		return "interface{}"
	}
}

func configType(t config.Type) string {
	return fmt.Sprintf("config.Type%s", capitalize(string(t)))
}

func setterName(name string) string {
	return "set" + capitalize(name)
}

func getterName(name string) string {
	return "Get" + capitalize(name)
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func needsUnmarshal(t config.Type) bool {
	switch t {
	case config.TypeStringArray, config.TypeIntArray, config.TypeBoolArray, config.TypeFloatArray,
		config.TypeStringSet, config.TypeIntSet, config.TypeBoolSet, config.TypeFloatSet:
		return true
	default:
		return false
	}
}

func parseFunc(t config.Type) string {
	switch t {
	case config.TypeString:
		return ""
	case config.TypeInt:
		return "strconv.ParseInt(val, 10, 64)"
	case config.TypeBool:
		return "strconv.ParseBool(val)"
	case config.TypeFloat:
		return "strconv.ParseFloat(val, 64)"
	default:
		return ""
	}
}

func escapeValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

const configTemplate = `// Code generated by mimir-generate; DO NOT EDIT.
package {{.PackageName}}

import (
	"context"
{{- if .NeedsJSON}}
	"encoding/json"
{{- end}}
	"strconv"
	"sync"

	"github.com/dfryer1193/mimir/api/config"
	"github.com/dfryer1193/mimir/client"
{{- if .NeedsSet}}
	"github.com/dfryer1193/mjolnir/utils/set"
{{- end}}
	"github.com/rs/zerolog/log"
)

const namespace = "{{.Namespace}}"

type {{.ConfigTypeName}} struct {
	client client.MimirClient

	rwMux sync.RWMutex
{{- range .Items}}
	{{.Name}} {{goType .Type}}
{{- end}}

	setFuncs map[string]client.SetFunc
}

func New{{.ConfigTypeName}}(ctx context.Context) *{{.ConfigTypeName}} {
	c, err := client.NewClient("{{.ConfigServerURL}}")
	if err != nil {
		panic(err)
	}

	cfg := &{{.ConfigTypeName}}{
		client: c,
	}

	curCfg, err := cfg.client.Start(ctx, namespace, &config.Config{
		Namespace: namespace,
		Items: []config.Item{
{{- range .Items}}
			{
				Name:        "{{.Name}}",
				Description: "{{escapeValue .Description}}",
				Type:        {{configType .Type}},
				Value:       "{{escapeValue .Value}}",
			},
{{- end}}
		},
	})
	if err != nil {
		log.Err(err).Msg("Failed to start client, using defaults...")
	}

	cfg.setFuncs = map[string]client.SetFunc{
{{- range .Items}}
		"{{.Name}}": cfg.{{setterName .Name}},
{{- end}}
	}

	cfg.setFromRawConfig(curCfg)
	go cfg.handleUpdates()

	return cfg
}

func (c *{{.ConfigTypeName}}) setFromRawConfig(cfg *config.Config) error {
	for _, item := range cfg.Items {
		if setFunc, ok := c.setFuncs[item.Name]; ok {
			setFunc(item.Value)
		} else {
			log.Warn().Str("config_key", item.Name).Msg("Config key missing. You may need to redeploy.")
		}
	}

	return nil
}

func (c *{{.ConfigTypeName}}) handleUpdates() {
	for {
		select {
		case update := <-c.client.UpdateCh():
			if update.Namespace != namespace {
				continue
			}
			c.handleUpdate(update)
		}
	}
}

func (c *{{.ConfigTypeName}}) handleUpdate(update *config.Update) {
	if setFunc, ok := c.setFuncs[update.FieldName]; ok {
		setFunc(update.Value)
	} else {
		log.Warn().Str("config_key", update.FieldName).Msg("Config key missing. You may need to redeploy.")
	}
}
{{range .Items}}
func (c *{{$.ConfigTypeName}}) {{setterName .Name}}(val string) {
	c.rwMux.Lock()
	defer c.rwMux.Unlock()
{{if eq .Type "string"}}
	c.{{.Name}} = val
{{- else if needsUnmarshal .Type}}
{{if eq .Type "string_array"}}
	tmp := make([]string, 0)
{{- else if eq .Type "int_array"}}
	tmp := make([]int64, 0)
{{- else if eq .Type "bool_array"}}
	tmp := make([]bool, 0)
{{- else if eq .Type "float_array"}}
	tmp := make([]float64, 0)
{{- else if eq .Type "string_set"}}
	tmp := make([]string, 0)
{{- else if eq .Type "int_set"}}
	tmp := make([]int64, 0)
{{- else if eq .Type "bool_set"}}
	tmp := make([]bool, 0)
{{- else if eq .Type "float_set"}}
	tmp := make([]float64, 0)
{{- end}}
	if err := json.Unmarshal([]byte(val), &tmp); err != nil {
		return
	}
{{if or (eq .Type "string_set") (eq .Type "int_set") (eq .Type "bool_set") (eq .Type "float_set")}}
	c.{{.Name}} = set.New(tmp...)
{{- else}}
	c.{{.Name}} = tmp
{{- end}}
{{- else}}
	parsed, err := {{parseFunc .Type}}
	if err != nil {
		return
	}

	c.{{.Name}} = parsed
{{- end}}
}
{{end}}
{{range .Items}}
// {{getterName .Name}} returns the value of {{.Name}}.
// {{.Name}}: {{.Description}}
func (c *{{$.ConfigTypeName}}) {{getterName .Name}}() {{goType .Type}} {
	c.rwMux.RLock()
	defer c.rwMux.RUnlock()

	return c.{{.Name}}
}
{{end}}
`
