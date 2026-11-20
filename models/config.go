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

// Item is one value in a Config. Value holds its Default.
type Item struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        Type   `json:"type"`
	Value       string `json:"value"`
}

// Config is a named, typed set of Items.
type Config struct {
	Namespace string `json:"namespace"`
	Items     []Item `json:"items"`
}
