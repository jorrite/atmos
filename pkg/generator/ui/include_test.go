package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudposse/atmos/pkg/generator/templates"
)

// TestExecuteWithSetup_IncludedLocalFileExcludedFromOutput proves a file
// consumed by a computed field's !include (a local reference table that
// exists solely to be included) is excluded from generation output, while
// an unrelated file in the same template still generates normally -- see
// config.WithIncludedPaths and ui.go's includedSet check.
func TestExecuteWithSetup_IncludedLocalFileExcludedFromOutput(t *testing.T) {
	ui := createTestUI(t)
	targetDir := t.TempDir()

	// !include's local resolution needs a real source directory, unlike the
	// rest of this package's in-memory-only Configuration fixtures.
	sourceDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "lib", "licenses.yaml"), []byte("MIT: {}\n"), 0o600))

	configuration := &templates.Configuration{
		Name:   "include-exclusion",
		Source: sourceDir,
		Files: []templates.File{
			{Path: "scaffold.yaml", Content: `apiVersion: atmos/v1
kind: AtmosScaffoldConfig
metadata:
  name: include-exclusion
spec:
  fields:
    - name: license_lookup
      type: computed
      value: !include ./lib/licenses.yaml
  files:
    - path: lib/licenses.yaml
    - path: output.txt
`, Permissions: 0o644},
			{Path: "lib/licenses.yaml", Content: "MIT: {}\n", Permissions: 0o644},
			{Path: "output.txt", Content: "lookup: {{ .Config.license_lookup }}\n", IsTemplate: true, Permissions: 0o644},
		},
	}

	err := ui.executeWithSetup(configuration, targetDir, false, false, true, "", map[string]interface{}{}, []string{"{{", "}}"})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(targetDir, "lib", "licenses.yaml"))
	assert.True(t, os.IsNotExist(err), "the !include-consumed file must not be generated as output")

	generated, err := os.ReadFile(filepath.Join(targetDir, "output.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(generated), "lookup: map[MIT:map[]]")
}

// TestExecuteWithSetup_TemplateSourceExcludedFromOutput proves a file
// consumed by a computed field's template: source (a local file rendered
// with args, then decoded) is excluded from generation output the same
// way an !include-consumed file already is -- see
// Processor.TemplateConsumedPaths and ui.go's includedSet check. Found via
// a /field-test pass: unlike !include (resolved at load time),
// template.source is only known once ComputeFields actually calls
// RenderExternalTemplate, so it needs its own accumulator merged in after
// setup runs, not alongside config.WithIncludedPaths' load-time result.
func TestExecuteWithSetup_TemplateSourceExcludedFromOutput(t *testing.T) {
	ui := createTestUI(t)
	targetDir := t.TempDir()

	sourceDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "lib", "sizing.json.tmpl"), []byte(
		`{"count": {{ len .environments }}}`,
	), 0o600))

	configuration := &templates.Configuration{
		Name:   "template-source-exclusion",
		Source: sourceDir,
		Files: []templates.File{
			{Path: "scaffold.yaml", Content: `apiVersion: atmos/v1
kind: AtmosScaffoldConfig
metadata:
  name: template-source-exclusion
spec:
  fields:
    - name: environments
      type: multiselect
      options: [dev, staging]
    - name: sizing
      type: computed
      template:
        source: ./lib/sizing.json.tmpl
        args:
          environments: answers.environments
  files:
    - path: lib/sizing.json.tmpl
    - path: output.txt
`, Permissions: 0o644},
			{Path: "lib/sizing.json.tmpl", Content: `{"count": {{ len .environments }}}`, Permissions: 0o644},
			{Path: "output.txt", Content: "sizing: {{ .Config.sizing }}\n", IsTemplate: true, Permissions: 0o644},
		},
	}

	err := ui.executeWithSetup(
		configuration, targetDir, false, false, true, "",
		map[string]interface{}{"environments": []string{"dev", "staging"}}, []string{"{{", "}}"},
	)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(targetDir, "lib", "sizing.json.tmpl"))
	assert.True(t, os.IsNotExist(err), "the template.source-consumed file must not be generated as output")

	generated, err := os.ReadFile(filepath.Join(targetDir, "output.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(generated), "sizing: map[count:2]")
}
