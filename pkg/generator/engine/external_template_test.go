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

// TestRenderExternalTemplate_LocalJSON proves a local template.source is
// resolved relative to the Processor's sourceDir (set via SetSourceDir),
// rendered against args, and decoded as JSON by its .json extension.
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

// TestRenderExternalTemplate_TmplSuffixStripped proves a trailing .tmpl is
// stripped before extension-based decoding: "sizing.json.tmpl" decodes as
// JSON the same way "sizing.json" would -- found via a /field-test pass:
// the original design's own motivating example used exactly this
// "<name>.<format>.tmpl" naming, but decoding by the literal, unstripped
// ".tmpl" extension silently fell back to the plain-rendered-string
// branch instead of decoding.
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

// TestRenderExternalTemplate_LocalSourceRecordedInTemplateConsumedPaths
// proves a local template.source is recorded (by its original, relative
// form) for exclusion from generation output -- see
// Processor.TemplateConsumedPaths -- while a remote source is not (it's
// never part of the local file walk to begin with).
func TestRenderExternalTemplate_LocalSourceRecordedInTemplateConsumedPaths(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.json"), []byte(`{"ok": true}`), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	_, err := p.RenderExternalTemplate("./sizing.json", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"./sizing.json"}, p.TemplateConsumedPaths())
}

func TestRenderExternalTemplate_RemoteSourceNotRecordedInTemplateConsumedPaths(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().FetchAndParseRaw(gomock.Any()).Return(`{"ok": true}`, nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	_, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.json", nil)
	require.NoError(t, err)
	assert.Empty(t, p.TemplateConsumedPaths())
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

// TestRenderExternalTemplate_ParseError proves a syntactically invalid Go
// template in template.source fails loudly with source context, not a
// silent empty result.
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

// TestValidateTemplateSource_ValidSource proves a valid, reachable local
// source passes without executing it (no args needed, and a template that
// would fail to execute without real args -- e.g. referencing a required
// field -- still validates fine, since validate never runs Execute at
// all).
func TestValidateTemplateSource_ValidSource(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sizing.json.tmpl"), []byte(
		`{"tier": "{{ .tier }}"}`,
	), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	require.NoError(t, p.ValidateTemplateSource("./sizing.json.tmpl"))
}

// TestValidateTemplateSource_MissingLocalFile proves a missing local
// source fails validation -- found via a /field-test pass: this was
// exactly the gap atmos scaffold validate had before ValidateTemplateSource
// existed, silently reporting "valid" for a template.source that
// generate would immediately fail on.
func TestValidateTemplateSource_MissingLocalFile(t *testing.T) {
	p := NewProcessor()
	p.SetSourceDir(t.TempDir())

	err := p.ValidateTemplateSource("./does-not-exist.tmpl")
	require.Error(t, err)
}

// TestValidateTemplateSource_ParseError proves invalid Go template syntax
// fails validation.
func TestValidateTemplateSource_ParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tmpl"), []byte("{{ .unterminated"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(dir)

	err := p.ValidateTemplateSource("./broken.tmpl")
	require.Error(t, err)
}

// TestValidateTemplateSource_Remote proves a remote source is validated
// through the same injected FileDownloader remote dispatch uses, not real
// network access.
func TestValidateTemplateSource_Remote(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().
		FetchAndParseRaw("git::https://example.com/acme/templates.git//sizing.json").
		Return(`{"tier": "{{ .tier }}"}`, nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	require.NoError(t, p.ValidateTemplateSource("git::https://example.com/acme/templates.git//sizing.json"))
}

// TestValidateTemplateSource_RemoteFetchError proves an unreachable remote
// source fails validation.
func TestValidateTemplateSource_RemoteFetchError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().FetchAndParseRaw(gomock.Any()).Return(nil, assert.AnError)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	err := p.ValidateTemplateSource("git::https://example.com/acme/templates.git//sizing.json")
	require.Error(t, err)
}

// TestRenderExternalTemplate_MissingLocalFile proves a genuinely missing
// local source fails loudly and clearly.
func TestRenderExternalTemplate_MissingLocalFile(t *testing.T) {
	p := NewProcessor()
	p.SetSourceDir(t.TempDir())

	_, err := p.RenderExternalTemplate("./does-not-exist.yaml", nil)
	require.Error(t, err)
}

// TestRenderExternalTemplate_AbsoluteLocalPath proves an absolute
// template.source is used as-is, not joined with sourceDir.
func TestRenderExternalTemplate_AbsoluteLocalPath(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "sizing.yaml")
	require.NoError(t, os.WriteFile(absPath, []byte("tier: fixed\n"), 0o600))

	p := NewProcessor()
	p.SetSourceDir(t.TempDir()) // Deliberately a different directory.

	got, err := p.RenderExternalTemplate(absPath, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "fixed"}, got)
}

// TestRenderExternalTemplate_Remote proves a remote (git::/oci://https://)
// template.source dispatches through the injected FileDownloader's
// FetchAndParseRaw, not real network access.
func TestRenderExternalTemplate_Remote(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().
		FetchAndParseRaw("git::https://example.com/acme/templates.git//sizing.yaml").
		Return("tier: {{ .tier }}\n", nil)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	got, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.yaml", map[string]interface{}{
		"tier": "standard",
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"tier": "standard"}, got)
}

// TestRenderExternalTemplate_RemoteFetchError proves a remote fetch
// failure surfaces as a real error, not a silent empty result.
func TestRenderExternalTemplate_RemoteFetchError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDownloader := downloader.NewMockFileDownloader(ctrl)
	mockDownloader.EXPECT().
		FetchAndParseRaw(gomock.Any()).
		Return(nil, assert.AnError)

	p := NewProcessor()
	p.fileDownloader = mockDownloader

	_, err := p.RenderExternalTemplate("git::https://example.com/acme/templates.git//sizing.yaml", nil)
	require.Error(t, err)
}

// TestIsRemoteTemplateSource covers the local-vs-remote dispatch directly.
func TestIsRemoteTemplateSource(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		{source: "./lib/sizing.tmpl", want: false},
		{source: "/abs/lib/sizing.tmpl", want: false},
		{source: "git::https://example.com/acme/templates.git//sizing.yaml", want: true},
		{source: "oci://ghcr.io/acme/templates:latest", want: true},
		{source: "https://example.com/sizing.yaml", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			assert.Equal(t, tt.want, isRemoteTemplateSource(tt.source))
		})
	}
}
