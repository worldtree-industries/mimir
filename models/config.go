package models

// Type is the type of an Item's value.
type Type string

// The types an Item can have.
const (
	TypeString      Type = "string"
	TypeInt         Type = "int"
	TypeBool        Type = "bool"
	TypeFloat       Type = "float"
	TypeStringArray Type = "string_array"
	TypeIntArray    Type = "int_array"
	TypeBoolArray   Type = "bool_array"
	TypeFloatArray  Type = "float_array"
	TypeStringSet   Type = "string_set"
	TypeIntSet      Type = "int_set"
	TypeBoolSet     Type = "bool_set"
	TypeFloatSet    Type = "float_set"
)

// Item is one value in a Config, with its Default.
type Item struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        Type   `json:"type"`
	Default     string `json:"default"`
}

// Config is a named, typed set of Items. TypeName is the Go type name of
// the generated Config.
type Config struct {
	TypeName string `json:"type_name"`
	Items    []Item `json:"items"`
}
