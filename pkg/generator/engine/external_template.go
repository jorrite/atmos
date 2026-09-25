package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	errUtils "github.com/cloudposse/atmos/errors"
	"github.com/cloudposse/atmos/pkg/downloader"
	"github.com/cloudposse/atmos/pkg/perf"
	"github.com/cloudposse/atmos/pkg/schema"
)

// remoteSourcePrefixes mirrors pkg/utils's own (unexported) !include remote
// detection -- kept in sync deliberately rather than imported, the same
// duplication-over-cross-package-coupling tradeoff ComputedFieldRenderer's
// own doc comment explains.
var remoteSourcePrefixes = []string{
	"http://", "https://", "s3://", "gcs://", "git://", "oci://", "scp://", "sftp://", "github://",
}

// isRemoteTemplateSource reports whether source looks like a remote
// reference (a known scheme prefix, or a go-getter-style "forced getter"
// like "git::...") rather than a local path.
func isRemoteTemplateSource(source string) bool {
	for _, prefix := range remoteSourcePrefixes {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return strings.Contains(source, "::")
}

// SetSourceDir records the scaffold template's own real or nominal source
// directory, anchoring a local Template.Source's relative-path resolution
// the same way config.WithSourceDir anchors a local !include target --
// both features share the same "relative to the template, not the process's
// CWD" convention.
func (p *Processor) SetSourceDir(dir string) {
	defer perf.Track(nil, "engine.Processor.SetSourceDir")()

	p.sourceDir = dir
}

// RenderExternalTemplate fetches source (a local path, or a remote
// git::/oci://https:// reference -- the same forms !include accepts),
// renders it as an ordinary Go template against args with fixed default
// {{ }} delimiters (never the calling scaffold's own spec.delimiters --
// see ComputedTemplateSpec's doc comment for why), and decodes the
// rendered output by source's file extension (.json, .yaml/.yml; any other
// extension is returned as the rendered string as-is). Satisfies
// config.ComputedTemplateRenderer.
func (p *Processor) RenderExternalTemplate(source string, args map[string]interface{}) (any, error) {
	defer perf.Track(nil, "engine.Processor.RenderExternalTemplate")()

	tmpl, err := p.fetchAndParseTemplate(source, args)
	if err != nil {
		return nil, err
	}

	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, args); err != nil {
		return nil, errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
			WithCause(err).
			WithExplanationf("Failed to render template.source %q", source).
			WithContext("source", source).
			Err()
	}

	return decodeRenderedTemplate(source, rendered.String())
}

// ValidateTemplateSource fetches and parses source (the same fetch-and-
// parse steps RenderExternalTemplate performs before executing), confirming
// it's reachable -- local or remote -- and syntactically valid as a Go
// template, without executing it against real args (real answers don't
// exist yet at scaffold-validate time, the same reason !include's own
// resolution -- which also runs before any answers exist -- is already
// checked by validate today). Used by atmos scaffold validate so a missing
// file, an unreachable remote source, or invalid template syntax fails
// validation too, not just generation.
func (p *Processor) ValidateTemplateSource(source string) error {
	defer perf.Track(nil, "engine.Processor.ValidateTemplateSource")()

	_, err := p.fetchAndParseTemplate(source, nil)
	return err
}

// fetchAndParseTemplate fetches source and parses it as a Go template with
// fixed default delimiters -- the fetch-and-parse steps shared by
// RenderExternalTemplate (which also executes and decodes the result) and
// ValidateTemplateSource (which stops here, without executing).
func (p *Processor) fetchAndParseTemplate(source string, args map[string]interface{}) (*template.Template, error) {
	content, err := p.fetchTemplateSource(source)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("computed-template").
		Delims(defaultLeftDelimiter, defaultRightDelimiter).
		Funcs(buildTemplateFuncMap(args)).
		Parse(content)
	if err != nil {
		return nil, errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
			WithCause(err).
			WithExplanationf("Failed to parse template.source %q as a Go template", source).
			WithContext("source", source).
			Err()
	}
	return tmpl, nil
}

// fetchTemplateSource reads source's raw content -- a local file (resolved
// relative to p.sourceDir when source is a relative path), or a remote
// fetch via the downloader package's real FileDownloader (the same
// underlying mechanism !include's own remote dispatch uses).
func (p *Processor) fetchTemplateSource(source string) (string, error) {
	if !isRemoteTemplateSource(source) {
		path := source
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.sourceDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
				WithCause(err).
				WithExplanationf("Failed to read template.source %q", source).
				WithHintf("Checked %q", path).
				WithContext("source", source).
				Err()
		}
		// Recorded by its original (relative) form, not the joined
		// absolute path -- TemplateConsumedPaths' caller matches this
		// against a template-root-relative file list the same way
		// config.WithIncludedPaths' consumed paths are matched.
		p.templateConsumedPaths = append(p.templateConsumedPaths, source)
		return string(data), nil
	}

	if p.fileDownloader == nil {
		p.fileDownloader = downloader.NewGoGetterDownloader(&schema.AtmosConfiguration{
			BasePath:         p.sourceDir,
			BasePathAbsolute: p.sourceDir,
		})
	}

	result, err := p.fileDownloader.FetchAndParseRaw(source)
	if err != nil {
		return "", errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
			WithCause(err).
			WithExplanationf("Failed to fetch template.source %q", source).
			WithContext("source", source).
			Err()
	}
	text, ok := result.(string)
	if !ok {
		return "", errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
			WithExplanationf("template.source %q did not resolve to raw text", source).
			WithContext("source", source).
			Err()
	}
	return text, nil
}

// decodeRenderedTemplate decodes rendered by source's file extension --
// .json via encoding/json, .yaml/.yml via yaml.v3, anything else returned
// as the plain rendered string. See templateOutputExtension for how a
// trailing .tmpl is stripped first, so "sizing.json.tmpl" decodes as
// JSON the same way "sizing.json" does.
func decodeRenderedTemplate(source, rendered string) (any, error) {
	var decoded any
	switch templateOutputExtension(source) {
	case ".json":
		if err := json.Unmarshal([]byte(rendered), &decoded); err != nil {
			return nil, errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
				WithCause(err).
				WithExplanationf("Rendered output of template.source %q is not valid JSON", source).
				WithContext("source", source).
				Err()
		}
		return decoded, nil
	case ".yaml", ".yml":
		if err := yaml.Unmarshal([]byte(rendered), &decoded); err != nil {
			return nil, errUtils.Build(errUtils.ErrScaffoldComputedFieldInvalid).
				WithCause(err).
				WithExplanationf("Rendered output of template.source %q is not valid YAML", source).
				WithContext("source", source).
				Err()
		}
		return decoded, nil
	default:
		return rendered, nil
	}
}

// templateOutputExtension returns the file extension that determines how
// RenderExternalTemplate's output is decoded. A trailing ".tmpl" (any
// case) is stripped first and the extension of what remains is used
// instead -- e.g. "sizing.json.tmpl" decodes as JSON, matching the
// common "<name>.<format>.tmpl" convention for a file that is both a
// template and a declared output format, without requiring template.source
// itself to be named e.g. "sizing.json" (a template's own file rarely
// looks like the format it renders, so requiring that would be a strange
// constraint to impose just to get decoding right).
func templateOutputExtension(source string) string {
	ext := filepath.Ext(source)
	if strings.EqualFold(ext, ".tmpl") {
		ext = filepath.Ext(strings.TrimSuffix(source, ext))
	}
	return strings.ToLower(ext)
}
