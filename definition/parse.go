package definition

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse décode le contenu YAML d'une définition Cardigann.
func Parse(data []byte) (IndexerDefinition, error) {
	var def IndexerDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return IndexerDefinition{}, fmt.Errorf("parsing cardigann yaml: %w", err)
	}
	return def, nil
}
