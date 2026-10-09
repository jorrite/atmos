package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/cloudposse/atmos/pkg/downloader"
)

// TestRenderExternalTemplate_LocalJSON proves a local source is resolved
// relative to the Processor's sourceDir (set via SetSourceDir), rendered
// against data, and decoded as JSON by its .json extension.
func TestRenderExternalTemplate_LocalJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.json"), []byte(
		`{"tier": "{{ .tier }}", "count": {{ len .environments }}}`,
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./sizing.json", map[string]interface{}{
		"tier":         "standard",
		"environments": []interface{}{"dev", "staging"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "standard", "count": float64(2)}, got)
}

// TestRenderExternalTemplate_LocalYAML proves the same for a .yaml source.
func TestRenderExternalTemplate_LocalYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.yaml"), []byte(
		"tier: {{ .tier }}\n",
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./sizing.yaml", map[string]interface{}{"tier": "standard"})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "standard"}, got)
}

// TestRenderExternalTemplate_ListData proves data doesn't have to be a
// map -- a bare "answers.<path>" dot-path (resolved by
// config.resolveAnswersDotPath) may resolve to a list, and that list is
// bound to the template's own "." directly.
func TestRenderExternalTemplate_ListData(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "regions.json"), []byte(
		`{"count": {{ len . }}, "first": "{{ index . 0 }}"}`,
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./regions.json", []interface{}{"us-east-1", "us-west-2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"count": float64(2), "first": "us-east-1"}, got)
}

// TestRenderExternalTemplate_TmplSuffixStripped proves a trailing .tmpl is
// stripped before extension-based decoding: "sizing.json.tmpl" decodes as
// JSON the same way "sizing.json" would.
func TestRenderExternalTemplate_TmplSuffixStripped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.json.tmpl"), []byte(
		`{"tier": "{{ .tier }}"}`,
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./sizing.json.tmpl", map[string]interface{}{"tier": "standard"})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "standard"}, got)
}

// TestRenderExternalTemplate_BareTmplExtensionReturnsRawString proves a
// source named just "*.tmpl" with no preceding format extension (nothing
// left to decode by) falls back to the plain rendered string, the same as
// any other unrecognized extension.
func TestRenderExternalTemplate_BareTmplExtensionReturnsRawString(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.tmpl"), []byte("Hello, {{ .name }}!"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./sizing.tmpl", map[string]interface{}{"name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", got)
}

// TestRenderExternalTemplate_UnknownExtensionReturnsRawString proves a
// source with no recognized extension is returned as the plain rendered
// string, mirroring !include's own "plain text for unrecognized
// extensions" fallback rather than erroring.
func TestRenderExternalTemplate_UnknownExtensionReturnsRawString(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notice.txt"), []byte("Hello, {{ .name }}!"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	got, err := p.RenderExternalTemplate("./notice.txt", map[string]interface{}{"name": "World"})
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", got)
}

// TestRenderExternalTemplate_MissingLocalFile proves a missing local
// source fails loudly rather than rendering empty/nil data.
func TestRenderExternalTemplate_MissingLocalFile(t *testing.T) {
	p := NewProcessor()
	p.SetSourceDir(t.TempDir())

	_, err := p.RenderExternalTemplate("./does-not-exist.tmpl", nil)
	require.Error(t, err)
}

// TestRenderExternalTemplate_ParseError proves a syntactically invalid Go
// template in source fails loudly with source context, not a silent empty
// result.
func TestRenderExternalTemplate_ParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tmpl"), []byte("{{ .unterminated"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	_, err := p.RenderExternalTemplate("./broken.tmpl", nil)
	require.Error(t, err)
}

// TestRenderExternalTemplate_ExecuteError proves a template that parses
// fine but fails at execution (e.g. calling a function with the wrong
// argument count) surfaces as a real error.
func TestRenderExternalTemplate_ExecuteError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tmpl"), []byte("{{ len }}"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	_, err := p.RenderExternalTemplate("./broken.tmpl", nil)
	require.Error(t, err)
}

// TestRenderExternalTemplate_DecodeErrors proves malformed rendered output
// for a recognized extension surfaces as a real decode error, for both
// JSON and YAML.
func TestRenderExternalTemplate_DecodeErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not valid json"), 0o600))
		p := NewProcessor()
		p.SetSourceDir(dir)
		_, err := p.RenderExternalTemplate("./bad.json", nil)
		require.Error(t, err)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(": : :not valid"), 0o600))
		p := NewProcessor()
		p.SetSourceDir(dir)
		_, err := p.RenderExternalTemplate("./bad.yaml", nil)
		require.Error(t, err)
	})
}

// TestRenderExternalTemplate_Remote proves a remote source is fetched
// through the injected FileDownloader, not real network access, rendered,
// and decoded.
func TestRenderExternalTemplate_Remote(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().
		FetchAndParseRaw("git::https://example.com/acme/templates.git//sizing.json").
		Return(`{"tier": "{{ .tier }}"}`, nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	got, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.json", map[string]interface{}{"tier": "standard"})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "standard"}, got)
}

// TestRenderExternalTemplate_RemoteFetchError proves an unreachable remote
// source surfaces as a real error.
func TestRenderExternalTemplate_RemoteFetchError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().FetchAndParseRaw(gomock.Any()).Return(nil, assert.AnError)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	_, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.json", nil)
	require.Error(t, err)
}

// TestRenderExternalTemplate_RemoteNonStringResult proves a FileDownloader
// that resolves a raw fetch to a non-string (e.g. FetchAndParseRaw somehow
// returning already-decoded data) is rejected clearly rather than passed
// through to the template parser as garbage.
func TestRenderExternalTemplate_RemoteNonStringResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().FetchAndParseRaw(gomock.Any()).Return(map[string]any{"unexpected": true}, nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	_, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.json", nil)
	require.Error(t, err)
}

// TestValidateIncludeTemplateSource_ValidSource proves a valid, reachable
// local source passes without executing it (nil data, and a template that
// would fail to execute without real data -- e.g. referencing a required
// field -- still validates fine, since validate never runs Execute at
// all).
func TestValidateIncludeTemplateSource_ValidSource(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.json.tmpl"), []byte(
		`{"tier": "{{ .tier }}"}`,
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	require.NoError(t, p.ValidateIncludeTemplateSource("./sizing.json.tmpl"))
}

// TestValidateIncludeTemplateSource_MissingLocalFile proves a missing
// local source fails validation.
func TestValidateIncludeTemplateSource_MissingLocalFile(t *testing.T) {
	p := NewProcessor()
	p.SetSourceDir(t.TempDir())

	err := p.ValidateIncludeTemplateSource("./does-not-exist.tmpl")
	require.Error(t, err)
}

// TestValidateIncludeTemplateSource_ParseError proves invalid Go template
// syntax fails validation.
func TestValidateIncludeTemplateSource_ParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tmpl"), []byte("{{ .unterminated"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	err := p.ValidateIncludeTemplateSource("./broken.tmpl")
	require.Error(t, err)
}

// TestValidateIncludeTemplateSource_Remote proves a remote source is
// validated through the same injected FileDownloader remote dispatch
// uses, not real network access.
func TestValidateIncludeTemplateSource_Remote(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().
		FetchAndParseRaw("git::https://example.com/acme/templates.git//sizing.json").
		Return(`{"tier": "{{ .tier }}"}`, nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	require.NoError(t, p.ValidateIncludeTemplateSource("git::https://example.com/acme/templates.git//sizing.json"))
}

// TestValidateIncludeTemplateSource_RemoteFetchError proves an unreachable
// remote source fails validation.
func TestValidateIncludeTemplateSource_RemoteFetchError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().FetchAndParseRaw(gomock.Any()).Return(nil, assert.AnError)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	err := p.ValidateIncludeTemplateSource("git::https://example.com/acme/templates.git//sizing.json")
	require.Error(t, err)
}

// TestIsRemoteIncludeTemplateSource proves the remote-scheme detection
// mirrors !include's own (pkg/utils's unexported isRemoteURL): known
// scheme prefixes and go-getter "forced getter" ("<getter>::...") syntax
// are remote; a plain relative or absolute local path is not.
func TestIsRemoteIncludeTemplateSource(t *testing.T) {
	remote := []string{
		"http://example.com/x.json",
		"https://example.com/x.json",
		"s3://bucket/x.json",
		"git::https://example.com/acme/templates.git//sizing.json",
		"oci://registry.example.com/templates:latest",
	}
	for _, source := range remote {
		assert.True(t, isRemoteIncludeTemplateSource(source), "expected %q to be remote", source)
	}

	local := []string{"./lib/sizing.json.tmpl", "lib/sizing.json.tmpl", "/abs/path/sizing.json.tmpl"}
	for _, source := range local {
		assert.False(t, isRemoteIncludeTemplateSource(source), "expected %q to be local", source)
	}
}
