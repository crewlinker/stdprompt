package resumeparsing_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	yaml "github.com/goccy/go-yaml"
	jsc "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/require"
)

func TestResumeParsing(t *testing.T) {
	t.Parallel()

	file, err := os.Open("resume.sprompt")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	prompt, err := Parse(file)
	require.NoError(t, err)

	require.NotNil(t, prompt.Metadata)
	require.NotNil(t, prompt.Body)
}

////////////////////////////////////////////////////////////////////////////////
// Public data structures
////////////////////////////////////////////////////////////////////////////////

// PromptMetadata mirrors the YAML front-matter.
type PromptMetadata struct {
	Data   []string    `json:"data"`
	Input  *jsc.Schema `json:"input"`
	Output *jsc.Schema `json:"output"`
}

// PromptData represents one validated sample from a data file.
type PromptData struct {
	Input  any `json:"input"`
	Output any `json:"output"`
}

// Prompt is the fully-parsed result.
type Prompt struct {
	Metadata *PromptMetadata
	Body     *template.Template
	Data     []PromptData
}

////////////////////////////////////////////////////////////////////////////////
// Parser
////////////////////////////////////////////////////////////////////////////////

var delim = []byte("---")

// Parse reads an .sprompt file, loads & validates any data files,
// and returns a fully-populated Prompt.
//
//nolint:gocognit
func Parse(r io.Reader) (*Prompt, error) {
	// ──────────────────────────────────────────────────────────────────────
	// 1. Split front-matter and body (streaming; O(1) memory)
	// ──────────────────────────────────────────────────────────────────────
	const (
		expectStart = iota
		inFront
		inBody
	)
	state := expectStart
	var ybuf, body bytes.Buffer

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Bytes()

		switch state {
		case expectStart:
			if bytes.Equal(line, delim) {
				state = inFront
			} else {
				return nil, fmt.Errorf("first line must be '---', got: %s", line)
			}

		case inFront:
			if bytes.Equal(line, delim) {
				state = inBody
			} else {
				ybuf.Write(line)
				ybuf.WriteByte('\n')
			}

		case inBody:
			body.Write(line)
			body.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// ──────────────────────────────────────────────────────────────────────
	// 2. Decode YAML front-matter
	//    (convert schema blocks → JSON bytes)
	// ──────────────────────────────────────────────────────────────────────
	var raw struct {
		Data   []string       `yaml:"data"`
		Input  map[string]any `yaml:"input"`
		Output map[string]any `yaml:"output"`
	}
	if err := yaml.Unmarshal(ybuf.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("YAML: %w", err)
	}

	toJSON := func(v map[string]any) (json.RawMessage, error) {
		if v == nil {
			return nil, nil
		}
		b, err := json.Marshal(v)
		return json.RawMessage(b), err
	}
	inJSON, err := toJSON(raw.Input)
	if err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	outJSON, err := toJSON(raw.Output)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}

	// ──────────────────────────────────────────────────────────────────────
	// 3. Compile JSON-Schema validators
	// ──────────────────────────────────────────────────────────────────────
	inValidator, err := compileSchema(inJSON)
	if err != nil {
		return nil, fmt.Errorf("input schema: %w", err)
	}
	outValidator, err := compileSchema(outJSON)
	if err != nil {
		return nil, fmt.Errorf("output schema: %w", err)
	}

	meta := &PromptMetadata{
		Data:   raw.Data,
		Input:  inValidator,
		Output: outValidator,
	}

	// ──────────────────────────────────────────────────────────────────────
	// 5. Compile template body
	// ──────────────────────────────────────────────────────────────────────
	tpl, err := template.
		New("body").
		Option("missingkey=error").
		Parse(body.String())
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}

	// ──────────────────────────────────────────────────────────────────────
	// 4. Load & validate data files
	// ──────────────────────────────────────────────────────────────────────
	var samples []PromptData
	for _, pattern := range meta.Data {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", pattern, err)
		}
		for _, path := range matches {
			js, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}

			var raw map[string]any
			if err := json.Unmarshal(js, &raw); err != nil {
				return nil, fmt.Errorf("decode %s: %w", path, err)
			}

			inRaw, okIn := raw["input"]
			outRaw, okOut := raw["output"]
			if !okIn || !okOut {
				return nil, fmt.Errorf("%s: each data file must have \"input\" and \"output\"", path)
			}

			var sample PromptData

			if err := inValidator.Validate(inRaw); err != nil {
				return nil, fmt.Errorf("%s: input invalid: %w", path, err)
			}

			sample.Input = inRaw

			if err := outValidator.Validate(outRaw); err != nil {
				return nil, fmt.Errorf("%s: output invalid: %w", path, err)
			}

			sample.Output = outRaw

			if err := tpl.Execute(io.Discard, sample.Input); err != nil {
				return nil, fmt.Errorf("%s: template execution: %w", path, err)
			}

			samples = append(samples, sample)
		}
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("no data files matched %v", meta.Data)
	}

	return &Prompt{Metadata: meta, Body: tpl, Data: samples}, nil
}

////////////////////////////////////////////////////////////////////////////////
// Helper
////////////////////////////////////////////////////////////////////////////////

// compileSchema turns raw JSON bytes into a jsonschema validator.
// Returns (nil, nil) if b is empty.
func compileSchema(b []byte) (*jsc.Schema, error) {
	if len(b) == 0 {
		return nil, nil
	}
	c := jsc.NewCompiler()
	c.Draft = jsc.Draft2020
	if err := c.AddResource("inline", bytes.NewReader(b)); err != nil {
		return nil, err
	}
	return c.Compile("inline")
}
