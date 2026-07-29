package definition

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse decodes the YAML content of a Cardigann definition.
func Parse(data []byte) (IndexerDefinition, error) {
	var def IndexerDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return IndexerDefinition{}, fmt.Errorf("parsing cardigann yaml: %w", err)
	}
	return def, nil
}
