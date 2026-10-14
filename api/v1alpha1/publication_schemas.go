package v1alpha1

import (
	"fmt"
	"regexp"
)

var publicationSchemaIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// EffectivePublicationSchemas validates direct Go input with the same contract as
// CRD admission. Only an omitted (nil) list receives the public default.
func (p *SupabaseProject) EffectivePublicationSchemas() ([]string, error) {
	if p.Spec.Powersync == nil || p.Spec.Powersync.PublicationSchemas == nil {
		return []string{"public"}, nil
	}
	schemas := p.Spec.Powersync.PublicationSchemas
	if len(schemas) == 0 {
		return nil, fmt.Errorf("powersync.publicationSchemas must contain at least one schema")
	}
	seen := make(map[string]bool, len(schemas))
	for i, schema := range schemas {
		if len(schema) > 63 || !publicationSchemaIdentifier.MatchString(schema) {
			return nil, fmt.Errorf("powersync.publicationSchemas[%d] must be a lowercase ASCII identifier of 1–63 characters", i)
		}
		if seen[schema] {
			return nil, fmt.Errorf("powersync.publicationSchemas contains duplicate schema %q", schema)
		}
		seen[schema] = true
	}
	return schemas, nil
}
