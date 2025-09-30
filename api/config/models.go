package config

type Key string

type Type string

//go:generate enumgen -type ConfigType
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

type Item struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        Type   `json:"type"`
	Value       string `json:"value"`
}

type Config struct {
	Namespace string `json:"namespace"`
	Items     []Item `json:"items"`
}

type Update struct {
	Namespace string
	FieldName string
	Value     string
}
