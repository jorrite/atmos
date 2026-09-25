package config

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errUtils "github.com/cloudposse/atmos/errors"
	"github.com/cloudposse/atmos/pkg/condition"
)

// fakeComputedRenderer builds a ComputedFieldRenderer test double that
// looks up expr in a fixed table, so tests can exercise ComputeFields'
// own orchestration (ordering, When gating, error propagation) without
// depending on engine.Processor.RenderAnswersExpression's real Go-template
// evaluation.
func fakeComputedRenderer(t *testing.T, table map[string]any) ComputedFieldRenderer {
	t.Helper()
	return func(expr string, _ map[string]interface{}, _ []string) (any, error) {
		value, ok := table[expr]
		if !ok {
			return nil, errors.New("no fake render entry for expression: " + expr)
		}
		return value, nil
	}
}

func TestComputeFields_WritesValueIntoValues(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "regions", Type: "multiselect"},
		{Name: "primary_region", Type: fieldTypeComputed, Value: "{{ answers.regions }}"},
	}}}
	values := map[string]interface{}{"regions": []string{"us-east-1"}}
	render := fakeComputedRenderer(t, map[string]any{"{{ answers.regions }}": "us-east-1"})

	err := ComputeFields(cfg, values, render, nil)
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", values["primary_region"])
}

// TestComputeFields_LaterComputedFieldSeesEarlierResult proves a computed
// field's own result is visible to a later computed field declared after
// it, matching the declared-order dependency rule computed fields document.
func TestComputeFields_LaterComputedFieldSeesEarlierResult(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "first_computed", Type: fieldTypeComputed, Value: "expr-first"},
		{Name: "second_computed", Type: fieldTypeComputed, Value: "expr-second"},
	}}}
	values := map[string]interface{}{}

	var secondSawFirst any
	render := ComputedFieldRenderer(func(expr string, answers map[string]interface{}, _ []string) (any, error) {
		if expr == "expr-first" {
			return "first-value", nil
		}
		secondSawFirst = answers["first_computed"]
		return "second-value", nil
	})

	err := ComputeFields(cfg, values, render, nil)
	require.NoError(t, err)
	assert.Equal(t, "first-value", secondSawFirst)
	assert.Equal(t, "second-value", values["second_computed"])
}

// TestComputeFields_LiteralValue proves a non-string Value (a hand-authored
// literal, or one produced by a YAML function such as !include before this
// field was ever unmarshaled) is stored as-is, with no renderer call at all
// -- ComputeFields must not require a ComputedFieldRenderer for a field
// that has nothing to render.
func TestComputeFields_LiteralValue(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "regions_list", Type: fieldTypeComputed, Value: []any{"eastasia", "westeurope"}},
		{Name: "retry_count", Type: fieldTypeComputed, Value: 3},
		{Name: "lookup", Type: fieldTypeComputed, Value: map[string]any{"eastasia": "eas"}},
	}}}
	values := map[string]interface{}{}

	err := ComputeFields(cfg, values, func(string, map[string]interface{}, []string) (any, error) {
		t.Fatal("render must not be called for a literal (non-string) Value")
		return nil, nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"eastasia", "westeurope"}, values["regions_list"])
	assert.Equal(t, 3, values["retry_count"])
	assert.Equal(t, map[string]any{"eastasia": "eas"}, values["lookup"])
}

func TestComputeFields_SkipsNonComputedFields(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "regular", Type: "input"},
	}}}
	values := map[string]interface{}{"regular": "unchanged"}

	err := ComputeFields(cfg, values, func(string, map[string]interface{}, []string) (any, error) {
		t.Fatal("render must not be called for a non-computed field")
		return nil, nil
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "unchanged", values["regular"])
}

// TestComputeFields_SkipsWhenFalse proves a computed field whose When
// evaluates false is left unset entirely, mirroring how a hidden regular
// field is never prompted for.
func TestComputeFields_SkipsWhenFalse(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "hidden_computed", Type: fieldTypeComputed, Value: "expr", When: condition.Must("answers.enabled == true")},
	}}}
	values := map[string]interface{}{"enabled": false}

	err := ComputeFields(cfg, values, func(string, map[string]interface{}, []string) (any, error) {
		t.Fatal("render must not be called when When evaluates false")
		return nil, nil
	}, nil)
	require.NoError(t, err)
	_, exists := values["hidden_computed"]
	assert.False(t, exists)
}

func TestComputeFields_RenderErrorPropagates(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "broken_computed", Type: fieldTypeComputed, Value: "expr"},
	}}}
	values := map[string]interface{}{}
	renderErr := errors.New("boom")

	err := ComputeFields(cfg, values, func(string, map[string]interface{}, []string) (any, error) {
		return nil, renderErr
	}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, renderErr)
}

func TestComputeFields_NilRendererErrors(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "broken_computed", Type: fieldTypeComputed, Value: "expr"},
	}}}

	err := ComputeFields(cfg, map[string]interface{}{}, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldInvalid)
}

func TestRejectComputedFieldOverrides(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "regular", Type: "input"},
		{Name: "computed_field", Type: fieldTypeComputed, Value: "expr"},
	}}}

	t.Run("no overrides", func(t *testing.T) {
		err := RejectComputedFieldOverrides(cfg, map[string]interface{}{})
		require.NoError(t, err)
	})

	t.Run("override for a regular field is fine", func(t *testing.T) {
		err := RejectComputedFieldOverrides(cfg, map[string]interface{}{"regular": "value"})
		require.NoError(t, err)
	})

	t.Run("override for a computed field is rejected", func(t *testing.T) {
		err := RejectComputedFieldOverrides(cfg, map[string]interface{}{"computed_field": "value"})
		require.Error(t, err)
		assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldNotSettable)
	})
}

func TestValidateComputedFieldDefinition(t *testing.T) {
	tests := []struct {
		name    string
		field   FieldDefinition
		wantErr bool
	}{
		{name: "valid computed field", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: "expr"}},
		{name: "valid computed field with a literal list value", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: []any{"a", "b"}}},
		{name: "valid computed field with a literal scalar value", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: 3}},
		{name: "valid regular field", field: FieldDefinition{Name: "f", Type: "input"}},
		{name: "computed without value", field: FieldDefinition{Name: "f", Type: fieldTypeComputed}, wantErr: true},
		{name: "computed with required", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: "expr", Required: true}, wantErr: true},
		{name: "computed with default", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: "expr", Default: "x"}, wantErr: true},
		{name: "non-computed with value", field: FieldDefinition{Name: "f", Type: "input", Value: "expr"}, wantErr: true},
		{name: "non-computed with a literal value", field: FieldDefinition{Name: "f", Type: "input", Value: 3}, wantErr: true},
		{name: "valid computed field with template", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}}},
		{name: "computed with template missing source", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{}}, wantErr: true},
		{name: "computed with both value and template", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Value: "expr", Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}}, wantErr: true},
		{name: "non-computed with template", field: FieldDefinition{Name: "f", Type: "input", Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}}, wantErr: true},
		{name: "computed with template and required", field: FieldDefinition{Name: "f", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}, Required: true}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComputedFieldDefinition(&tt.field)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValueReferencesAnswer(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want bool
	}{
		{name: "direct reference", expr: "{{ answers.regions }}", want: true},
		{name: "nested field selection", expr: "{{ answers.regions.foo }}", want: true},
		{name: "function argument", expr: "{{ ternary answers.other answers.regions (gt 1 0) }}", want: true},
		{name: "no reference", expr: "{{ answers.other }}", want: false},
		{name: "prefix collision is not a match", expr: "{{ answers.regionsx }}", want: false},
		{name: "literal string containing the name is not a match", expr: `{{ printf "regions" }}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueReferencesAnswer(tt.expr, "regions")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateComputedFieldOrdering(t *testing.T) {
	tests := []struct {
		name    string
		fields  []FieldDefinition
		wantErr bool
	}{
		{
			name: "earlier computed field reference is fine",
			fields: []FieldDefinition{
				{Name: "first", Type: fieldTypeComputed, Value: "expr-first"},
				{Name: "second", Type: fieldTypeComputed, Value: "{{ answers.first }}"},
			},
		},
		{
			name: "regular field reference regardless of order is fine",
			fields: []FieldDefinition{
				{Name: "computed_field", Type: fieldTypeComputed, Value: "{{ answers.later_regular }}"},
				{Name: "later_regular", Type: "input"},
			},
		},
		{
			name: "later computed field reference is rejected",
			fields: []FieldDefinition{
				{Name: "first", Type: fieldTypeComputed, Value: "{{ answers.second }}"},
				{Name: "second", Type: fieldTypeComputed, Value: "expr-second"},
			},
			wantErr: true,
		},
		{
			name: "self-reference is rejected",
			fields: []FieldDefinition{
				{Name: "selfref", Type: fieldTypeComputed, Value: "{{ answers.selfref }}"},
			},
			wantErr: true,
		},
		{
			name: "a literal (non-string) value has nothing to scan and is never rejected",
			fields: []FieldDefinition{
				{Name: "first", Type: fieldTypeComputed, Value: []any{"a", "b"}},
				{Name: "second", Type: fieldTypeComputed, Value: "expr-second"},
			},
		},
		{
			name: "template.args referencing an earlier computed field is fine",
			fields: []FieldDefinition{
				{Name: "first", Type: fieldTypeComputed, Value: "expr-first"},
				{Name: "second", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{
					Source: "./lib/sizing.tmpl",
					Args:   map[string]string{"x": "answers.first"},
				}},
			},
		},
		{
			name: "template.args referencing a later computed field is rejected",
			fields: []FieldDefinition{
				{Name: "first", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{
					Source: "./lib/sizing.tmpl",
					Args:   map[string]string{"x": "answers.second"},
				}},
				{Name: "second", Type: fieldTypeComputed, Value: "expr-second"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComputedFieldOrdering(tt.fields)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateOptionsNotComputed(t *testing.T) {
	tests := []struct {
		name    string
		fields  []FieldDefinition
		wantErr bool
	}{
		{
			name: "no computed fields at all",
			fields: []FieldDefinition{
				{Name: "regions", Type: "multiselect", Options: []string{"a", "b"}},
				{Name: "picked", Type: "select", Options: "answers.regions"},
			},
		},
		{
			name: "options dot-path references a regular field",
			fields: []FieldDefinition{
				{Name: "regions", Type: "multiselect", Options: []string{"a", "b"}},
				{Name: "picked", Type: "select", Options: "answers.regions"},
				{Name: "derived", Type: fieldTypeComputed, Value: "{{ answers.regions }}"},
			},
		},
		{
			name: "options dot-path references a computed field",
			fields: []FieldDefinition{
				{Name: "derived", Type: fieldTypeComputed, Value: "expr"},
				{Name: "picked", Type: "select", Options: "answers.derived"},
			},
			wantErr: true,
		},
		{
			name: "options template expression references a computed field",
			fields: []FieldDefinition{
				{Name: "derived", Type: fieldTypeComputed, Value: "expr"},
				{Name: "picked", Type: "select", Options: "{{ splitList \",\" answers.derived }}"},
			},
			wantErr: true,
		},
		{
			name: "non-string options (a static list) is untouched",
			fields: []FieldDefinition{
				{Name: "derived", Type: fieldTypeComputed, Value: "expr"},
				{Name: "picked", Type: "select", Options: []string{"a", "b"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptionsNotComputed(tt.fields)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, errUtils.ErrScaffoldFieldOptionsInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestComputeFields_TemplateField proves a computed field's template:
// resolves each Args entry (both the bare answers.<path> dot-path form and
// the delimited-expression form, rendered via the same ComputedFieldRenderer
// Value expressions use) into a map, then calls renderTemplate with
// field.Template.Source and that map -- storing whatever renderTemplate
// returns, the same literal-storage path a non-string Value uses.
func TestComputeFields_TemplateField(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "tier", Type: "select"},
		{Name: "sizing", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{
			Source: "./lib/sizing.tmpl",
			Args: map[string]string{
				"tier":    "answers.tier",
				"doubled": "{{ mul 2 2 }}",
			},
		}},
	}}}
	values := map[string]interface{}{"tier": "standard"}

	render := ComputedFieldRenderer(func(expr string, _ map[string]interface{}, _ []string) (any, error) {
		if expr == "{{ mul 2 2 }}" {
			return 4, nil
		}
		return nil, fmt.Errorf("unexpected expression: %s", expr)
	})

	var gotSource string
	var gotArgs map[string]interface{}
	renderTemplate := ComputedTemplateRenderer(func(source string, args map[string]interface{}) (any, error) {
		gotSource = source
		gotArgs = args
		return map[string]interface{}{"rendered": true}, nil
	})

	err := ComputeFields(cfg, values, render, renderTemplate)
	require.NoError(t, err)
	assert.Equal(t, "./lib/sizing.tmpl", gotSource)
	assert.Equal(t, map[string]interface{}{"tier": "standard", "doubled": 4}, gotArgs)
	assert.Equal(t, map[string]interface{}{"rendered": true}, values["sizing"])
}

// TestComputeFields_TemplateField_NilRendererErrors proves a computed
// field's template: fails loudly (not silently no-op) when ComputeFields is
// called without a ComputedTemplateRenderer.
func TestComputeFields_TemplateField_NilRendererErrors(t *testing.T) {
	cfg := &ScaffoldConfig{Spec: ScaffoldSpec{Fields: []FieldDefinition{
		{Name: "sizing", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}},
	}}}

	err := ComputeFields(cfg, map[string]interface{}{}, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldInvalid)
}

// TestResolveTemplateArg covers both dispatch branches directly: a bare
// answers.<path> dot-path walked against values with no renderer needed,
// and a delimited expression routed through render.
func TestResolveTemplateArg(t *testing.T) {
	values := map[string]interface{}{"region": "us-east-1"}
	delimiters := defaultDelimiters(nil)

	t.Run("bare dot-path", func(t *testing.T) {
		got, err := resolveTemplateArg("answers.region", values, nil, delimiters)
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", got)
	})

	t.Run("delimited expression", func(t *testing.T) {
		render := ComputedFieldRenderer(func(expr string, _ map[string]interface{}, _ []string) (any, error) {
			assert.Equal(t, "{{ upper answers.region }}", expr)
			return "US-EAST-1", nil
		})
		got, err := resolveTemplateArg("{{ upper answers.region }}", values, render, delimiters)
		require.NoError(t, err)
		assert.Equal(t, "US-EAST-1", got)
	})

	t.Run("bare dot-path missing key", func(t *testing.T) {
		_, err := resolveTemplateArg("answers.missing", values, nil, delimiters)
		require.Error(t, err)
	})

	t.Run("bare path without answers prefix", func(t *testing.T) {
		_, err := resolveTemplateArg("region", values, nil, delimiters)
		require.Error(t, err)
	})

	t.Run("delimited expression with nil renderer errors", func(t *testing.T) {
		_, err := resolveTemplateArg("{{ upper answers.region }}", values, nil, delimiters)
		require.Error(t, err)
		assert.ErrorIs(t, err, errUtils.ErrScaffoldComputedFieldInvalid)
	})

	t.Run("dot-path segment is not a map", func(t *testing.T) {
		_, err := resolveTemplateArg("answers.region.nested", values, nil, delimiters)
		require.Error(t, err)
	})
}

// TestComputeTemplateField_ArgErrorPropagates proves an error resolving
// one Template.Args entry aborts the whole field with clear context (which
// field, which arg name), rather than silently skipping it.
func TestComputeTemplateField_ArgErrorPropagates(t *testing.T) {
	field := &FieldDefinition{Name: "sizing", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{
		Source: "./lib/sizing.tmpl",
		Args:   map[string]string{"x": "answers.missing"},
	}}

	_, err := computeTemplateField(field, map[string]interface{}{}, nil, func(string, map[string]interface{}) (any, error) {
		t.Fatal("renderTemplate must not be called when an arg fails to resolve")
		return nil, nil
	}, defaultDelimiters(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sizing")
	assert.Contains(t, err.Error(), `"x"`)
}

// TestComputeTemplateField_RenderTemplateErrorPropagates proves a
// renderTemplate failure surfaces with the field name for context.
func TestComputeTemplateField_RenderTemplateErrorPropagates(t *testing.T) {
	field := &FieldDefinition{Name: "sizing", Type: fieldTypeComputed, Template: &ComputedTemplateSpec{Source: "./lib/sizing.tmpl"}}
	renderErr := errors.New("boom")

	_, err := computeTemplateField(field, map[string]interface{}{}, nil, func(string, map[string]interface{}) (any, error) {
		return nil, renderErr
	}, defaultDelimiters(nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, renderErr)
	assert.Contains(t, err.Error(), "sizing")
}
