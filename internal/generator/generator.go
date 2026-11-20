// Package generator turns a config file into the Go source of a typed Config.
package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"strings"
	"text/template"

	"github.com/worldtree-industries/mimir/models"
)

// Item is one Item as written in a config file.
type Item struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Type        models.Type `json:"type"`
	Value       string      `json:"value"`
}

// File is the contents of a config file: one Config and its Items.
type File struct {
	Namespace   string `json:"namespace"`
	DisplayName string `json:"display_name"`
	Items       []Item `json:"items"`
}

type templateData struct {
	PackageName    string
	ConfigTypeName string
	ConfigName     string
	DisplayName    string
	Items          []Item
	NeedsSet       bool
	NeedsJSON      bool
}

// ConfigName is the globally unique name of the Config type typeName declared
// in the Go package at importPath: "<import path>.<type name>".
func ConfigName(importPath, typeName string) string {
	return importPath + "." + typeName
}

func checkUniqueItemNames(items []Item) error {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if seen[item.Name] {
			return fmt.Errorf("duplicate Item name %q", item.Name)
		}
		seen[item.Name] = true
	}

	return nil
}

// Generate returns the formatted Go source for the Config defined by the
// config file contents in input, declared in package packageName whose import
// path is importPath. The generated code exposes the derived Config name as
// the constant <Type>Name and the optional display name as <Type>DisplayName.
func Generate(input []byte, packageName, importPath string) ([]byte, error) {
	var cfg File
	if err := json.Unmarshal(input, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := checkUniqueItemNames(cfg.Items); err != nil {
		return nil, err
	}

	data := templateData{
		ConfigName:     ConfigName(importPath, cfg.Namespace),
		DisplayName:    cfg.DisplayName,
		PackageName:    packageName,
		ConfigTypeName: cfg.Namespace,
		Items:          cfg.Items,
	}
	for _, item := range cfg.Items {
		if isSet(item.Type) {
			data.NeedsSet = true
		}
		if needsUnmarshal(item.Type) {
			data.NeedsJSON = true
		}
	}

	tmpl, err := template.New("config").Funcs(template.FuncMap{
		"goType":      goType,
		"elemType":    elemType,
		"setterName":  setterName,
		"getterName":  getterName,
		"parseFunc":   parseFunc,
		"isSet":       isSet,
		"isArray":     isArray,
		"escapeValue": escapeValue,
	}).Parse(configTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err = tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to format generated code: %w\n%s", err, buf.String())
	}

	return formatted, nil
}

func isSet(t models.Type) bool {
	switch t {
	case models.TypeStringSet, models.TypeIntSet, models.TypeBoolSet, models.TypeFloatSet:
		return true
	case models.TypeString, models.TypeInt, models.TypeBool, models.TypeFloat,
		models.TypeStringArray, models.TypeIntArray, models.TypeBoolArray, models.TypeFloatArray:
		return false
	}
	return false
}

func isArray(t models.Type) bool {
	switch t {
	case models.TypeStringArray, models.TypeIntArray, models.TypeBoolArray, models.TypeFloatArray:
		return true
	case models.TypeString, models.TypeInt, models.TypeBool, models.TypeFloat,
		models.TypeStringSet, models.TypeIntSet, models.TypeBoolSet, models.TypeFloatSet:
		return false
	}
	return false
}

func needsUnmarshal(t models.Type) bool {
	return isArray(t) || isSet(t)
}

// elemType is the Go type of one element of a collection Item.
func elemType(t models.Type) string {
	switch t {
	case models.TypeStringArray, models.TypeStringSet:
		return "string"
	case models.TypeIntArray, models.TypeIntSet:
		return "int64"
	case models.TypeBoolArray, models.TypeBoolSet:
		return "bool"
	case models.TypeFloatArray, models.TypeFloatSet:
		return "float64"
	case models.TypeString, models.TypeInt, models.TypeBool, models.TypeFloat:
		return ""
	}
	return ""
}

func goType(t models.Type) string {
	switch t {
	case models.TypeString:
		return "string"
	case models.TypeInt:
		return "int64"
	case models.TypeBool:
		return "bool"
	case models.TypeFloat:
		return "float64"
	case models.TypeStringArray, models.TypeIntArray, models.TypeBoolArray, models.TypeFloatArray:
		return "[]" + elemType(t)
	case models.TypeStringSet, models.TypeIntSet, models.TypeBoolSet, models.TypeFloatSet:
		return "set.Set[" + elemType(t) + "]"
	}
	return "any"
}

func setterName(name string) string {
	return "set" + capitalize(name)
}

func getterName(name string) string {
	return "Get" + capitalize(name)
}

func capitalize(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func parseFunc(t models.Type) string {
	switch t {
	case models.TypeInt:
		return "strconv.ParseInt(val, 10, 64)"
	case models.TypeBool:
		return "strconv.ParseBool(val)"
	case models.TypeFloat:
		return "strconv.ParseFloat(val, 64)"
	case models.TypeString, models.TypeStringArray, models.TypeIntArray, models.TypeBoolArray,
		models.TypeFloatArray, models.TypeStringSet, models.TypeIntSet, models.TypeBoolSet, models.TypeFloatSet:
		return ""
	}
	return ""
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
{{if .NeedsSet}}
	"github.com/dfryer1193/mjolnir/utils/set"
{{- end}}
	"github.com/worldtree-industries/mimir/client"
)

// {{.ConfigTypeName}}Name is the globally unique name of the Config, derived
// from its Go package path and type.
const {{.ConfigTypeName}}Name = "{{escapeValue .ConfigName}}"

// {{.ConfigTypeName}}DisplayName is the optional short display name from the
// config file; empty when unset.
const {{.ConfigTypeName}}DisplayName = "{{escapeValue .DisplayName}}"

// {{.ConfigTypeName}} holds the Items of the {{.ConfigTypeName}} Config.
type {{.ConfigTypeName}} struct {
	rwMux sync.RWMutex
{{- range .Items}}
	{{.Name}} {{goType .Type}}
{{- end}}
}

// New{{.ConfigTypeName}} returns the Config with every Item at its Default,
// or at its Override once the Mimir client has read one. The client keeps the
// values current in the background until ctx is done.
func New{{.ConfigTypeName}}(ctx context.Context, opts ...client.Option) *{{.ConfigTypeName}} {
	cfg := &{{.ConfigTypeName}}{}
	client.Start(ctx, client.Definition{
		Config:      {{.ConfigTypeName}}Name,
		DisplayName: {{.ConfigTypeName}}DisplayName,
		Items: []client.Item{
{{- range .Items}}
			{
				Name:        {{printf "%q" .Name}},
				Type:        {{printf "%q" .Type}},
				Description: {{printf "%q" .Description}},
				Default:     {{printf "%q" .Value}},
				Set:         cfg.{{setterName .Name}},
			},
{{- end}}
		},
	}, opts...)

	return cfg
}
{{range .Items}}
func (c *{{$.ConfigTypeName}}) {{setterName .Name}}(val string) error {
	c.rwMux.Lock()
	defer c.rwMux.Unlock()
{{if eq .Type "string"}}
	c.{{.Name}} = val
{{- else if isArray .Type}}
	tmp := make([]{{elemType .Type}}, 0)
	if err := json.Unmarshal([]byte(val), &tmp); err != nil {
		return err
	}

	c.{{.Name}} = tmp
{{- else if isSet .Type}}
	tmp := make([]{{elemType .Type}}, 0)
	if err := json.Unmarshal([]byte(val), &tmp); err != nil {
		return err
	}

	c.{{.Name}} = set.New(tmp...)
{{- else}}
	parsed, err := {{parseFunc .Type}}
	if err != nil {
		return err
	}

	c.{{.Name}} = parsed
{{- end}}

	return nil
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
