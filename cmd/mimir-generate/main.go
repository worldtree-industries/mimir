package mimir_generate

import (
	"github.com/dfryer1193/mimir/api/config"
)

func main() {
}

type configValue struct {
	Type        config.Type `json:"type"`
	Value       string      `json:"value"`
	Description string      `json:"description"`
}

type configProto struct {
	Package string                  `json:"package"`
	Name    string                  `json:"name"`
	Values  map[string]*configValue `json:"values"`
}
