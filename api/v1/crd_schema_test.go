package v1

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// CRDSchema represents the structure we need from the CRD YAML
type CRDSchema struct {
	Spec struct {
		Versions []struct {
			Schema struct {
				OpenAPIV3Schema struct {
					Properties map[string]SchemaProperty `yaml:"properties"`
				} `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

type SchemaProperty struct {
	Properties map[string]SchemaProperty `yaml:"properties,omitempty"`
	Items      *SchemaProperty           `yaml:"items,omitempty"`
}

// getJSONFieldNames extracts JSON field names from a struct type using reflection
func getJSONFieldNames(t reflect.Type) map[string]bool {
	fields := make(map[string]bool)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		// Extract field name from json tag (before comma for omitempty etc)
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName != "" && jsonName != "inline" {
			fields[jsonName] = true
		}
	}
	return fields
}

// TestCRDSchemaMatchesGoTypes verifies that all Go struct fields are defined in the CRD schema
func TestCRDSchemaMatchesGoTypes(t *testing.T) {
	// Try multiple possible CRD locations
	crdPaths := []string{
		"../../test/crds/vpamanager-crd.yaml",
		"../../charts/vpa-operator/templates/crds/vpamanager-crd.yaml",
	}

	var crdData []byte
	var err error
	var usedPath string

	for _, path := range crdPaths {
		crdData, err = os.ReadFile(path)
		if err == nil {
			usedPath = path
			break
		}
	}

	if crdData == nil {
		t.Fatalf("Could not read CRD from any known location: %v", err)
	}

	t.Logf("Using CRD from: %s", usedPath)

	// Handle Helm template conditionals by stripping them
	crdContent := string(crdData)
	crdContent = strings.ReplaceAll(crdContent, "{{- if .Values.crds.install -}}", "")
	crdContent = strings.ReplaceAll(crdContent, "{{- end }}", "")
	// Remove any remaining Helm template expressions for labels
	lines := strings.Split(crdContent, "\n")
	var cleanLines []string
	for _, line := range lines {
		if !strings.Contains(line, "{{") {
			cleanLines = append(cleanLines, line)
		}
	}
	crdContent = strings.Join(cleanLines, "\n")

	var crd CRDSchema
	if err := yaml.Unmarshal([]byte(crdContent), &crd); err != nil {
		t.Fatalf("Failed to parse CRD YAML: %v", err)
	}

	if len(crd.Spec.Versions) == 0 {
		t.Fatal("No versions found in CRD")
	}

	schema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties

	// Test spec fields
	t.Run("VpaManagerSpec fields match CRD", func(t *testing.T) {
		goSpecFields := getJSONFieldNames(reflect.TypeOf(VpaManagerSpec{}))
		crdSpecFields := getSchemaFieldNames(schema["spec"])

		for field := range goSpecFields {
			if !crdSpecFields[field] {
				t.Errorf("Go VpaManagerSpec has field %q but CRD spec schema does not", field)
			}
		}

		for field := range crdSpecFields {
			if !goSpecFields[field] {
				t.Errorf("CRD spec schema has field %q but Go VpaManagerSpec does not", field)
			}
		}
	})

	// Test status fields
	t.Run("VpaManagerStatus fields match CRD", func(t *testing.T) {
		goStatusFields := getJSONFieldNames(reflect.TypeOf(VpaManagerStatus{}))
		crdStatusFields := getSchemaFieldNames(schema["status"])

		for field := range goStatusFields {
			if !crdStatusFields[field] {
				t.Errorf("Go VpaManagerStatus has field %q but CRD status schema does not", field)
			}
		}

		for field := range crdStatusFields {
			if !goStatusFields[field] {
				t.Errorf("CRD status schema has field %q but Go VpaManagerStatus does not", field)
			}
		}
	})

	// Test WorkloadReference fields (used in status arrays)
	t.Run("WorkloadReference fields match CRD managedDeployments items", func(t *testing.T) {
		goWorkloadFields := getJSONFieldNames(reflect.TypeOf(WorkloadReference{}))

		statusProps := schema["status"]
		if managedDeps, ok := statusProps.Properties["managedDeployments"]; ok {
			if managedDeps.Items != nil {
				crdWorkloadFields := getSchemaFieldNames(*managedDeps.Items)

				for field := range goWorkloadFields {
					if !crdWorkloadFields[field] {
						t.Errorf("Go WorkloadReference has field %q but CRD managedDeployments items do not", field)
					}
				}
			}
		}
	})

	// Test ContainerResourcePolicy fields
	t.Run("ContainerResourcePolicy fields match CRD", func(t *testing.T) {
		goContainerFields := getJSONFieldNames(reflect.TypeOf(ContainerResourcePolicy{}))

		specProps := schema["spec"]
		if resourcePolicy, ok := specProps.Properties["resourcePolicy"]; ok {
			if containerPolicies, ok := resourcePolicy.Properties["containerPolicies"]; ok {
				if containerPolicies.Items != nil {
					crdContainerFields := getSchemaFieldNames(*containerPolicies.Items)

					for field := range goContainerFields {
						if !crdContainerFields[field] {
							t.Errorf("Go ContainerResourcePolicy has field %q but CRD containerPolicies items do not", field)
						}
					}

					for field := range crdContainerFields {
						if !goContainerFields[field] {
							t.Errorf("CRD containerPolicies has field %q but Go ContainerResourcePolicy does not", field)
						}
					}
				}
			}
		}
	})
}

func getSchemaFieldNames(prop SchemaProperty) map[string]bool {
	fields := make(map[string]bool)
	for name := range prop.Properties {
		fields[name] = true
	}
	return fields
}
