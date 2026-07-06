package integrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"text/template"

	"github.com/rockholla/gitspork/v2/internal/config"
	inputpkg "github.com/rockholla/gitspork/v2/internal/input"
	"github.com/rockholla/gitspork/v2/internal/sdktypes"
)

// IntegratorTemplated will process a list of instructions on how to render Go templates in the upstream to downstream rendered files
type IntegratorTemplated struct{}

var _ TemplatedIntegrator = (*IntegratorTemplated)(nil)

// IntegratorTemplateData is the common data interface passed into templates
type IntegratorTemplatedData struct {
	Inputs map[string]string `json:"inputs"`
}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorTemplated) Integrate(templatedInstructions []config.GitSporkConfigTemplated, upstreamPath string, downstreamPath string, forceRePrompt bool, logger sdktypes.Logger) error {
	if err := migrateLegacyTemplatedCache(downstreamPath); err != nil {
		return fmt.Errorf("error migrating legacy templated cache: %v", err)
	}
	existingCache, err := loadTemplatedInputs(downstreamPath)
	if err != nil {
		return fmt.Errorf("error loading templated inputs cache: %v", err)
	}
	// Nothing to do at all — no templated instructions and no lingering cache. Skip
	// creating an empty cache file and .gitattributes on downstreams that don't use
	// templated integration.
	if len(templatedInstructions) == 0 && len(existingCache) == 0 {
		return nil
	}

	// captured input values will support the input 'previous_input' type via this structure:
	/*
		capturedInputValues = {
			<template name> = {
				<input name> = <input value>
				<input name> = <input value>
			}
		}
	*/
	capturedInputValues := map[string]map[string]string{}
	// nextCache is built up as we process each instruction and written at the end.
	// Destinations no longer present in templatedInstructions are pruned by construction.
	nextCache := map[string]map[string]string{}

	for _, templatedInstruction := range templatedInstructions {
		logger.Log("📄 executing templated instruction for rendering upstream template %s to downstream location %s", templatedInstruction.Template, templatedInstruction.Destination)

		capturedInputValues[templatedInstruction.Template] = map[string]string{}
		templateData := IntegratorTemplatedData{
			Inputs: map[string]string{},
		}
		if cached, ok := existingCache[templatedInstruction.Destination]; ok {
			// seed template inputs from consolidated cache so users aren't re-prompted
			maps.Copy(templateData.Inputs, cached)
			maps.Copy(capturedInputValues[templatedInstruction.Template], templateData.Inputs)
		}
		// we'll begin by gathering inputs to start
		for _, input := range templatedInstruction.Inputs {
			if input.JSONDataPath != "" {
				jsonDataPath := filepath.Join(downstreamPath, input.JSONDataPath)
				jsonData, err := os.ReadFile(jsonDataPath)
				if err != nil {
					return fmt.Errorf("error reading json_data_path at %s: %v", jsonDataPath, err)
				}
				if err := json.Unmarshal(jsonData, &templateData.Inputs); err != nil {
					return fmt.Errorf("error parsing json_data_path file %s into inputs: %v", jsonDataPath, err)
				}
				// Only propagate inputs to capturedInputValues after a successful
				// unmarshal — otherwise a JSON parse error would leak partially-
				// populated data into the previous_input chain for subsequent
				// templated instructions in this run.
				maps.Copy(capturedInputValues[templatedInstruction.Template], templateData.Inputs)
			} else if input.Prompt != "" {
				if templateData.Inputs[input.Name] == "" || forceRePrompt {
					requestInputOpts := &inputpkg.RequestInputOptions{
						Type:   inputpkg.SingleValue,
						Prompt: input.Prompt,
					}
					requestInputResult, err := inputpkg.RequestInput(requestInputOpts)
					if err != nil {
						return fmt.Errorf("error setting up prompt input: %v", err)
					}
					templateData.Inputs[input.Name] = requestInputResult.StringValue
					capturedInputValues[templatedInstruction.Template][input.Name] = requestInputResult.StringValue
				}
			} else if input.PreviousInput != nil {
				var previousInputErr error
				if _, ok := capturedInputValues[input.PreviousInput.Template]; ok {
					if value, ok := capturedInputValues[input.PreviousInput.Template][input.PreviousInput.Name]; ok {
						templateData.Inputs[input.Name] = value
						capturedInputValues[templatedInstruction.Template][input.Name] = value
					} else {
						previousInputErr = fmt.Errorf("previous input name %s not found in template %s", input.PreviousInput.Name, input.PreviousInput.Template)
					}
				} else {
					previousInputErr = fmt.Errorf("previous template not found: %s", input.PreviousInput.Template)
				}
				if previousInputErr != nil {
					return fmt.Errorf("error in previous_input configuration under template %s: %v", templatedInstruction.Template, previousInputErr)
				}
			} else {
				return fmt.Errorf("templated definition %s requires at least one of 'prompt', 'json_data_path', or 'previous_input' to be defined", input.Name)
			}
		}

		// now that we have our template data populated we can actually render the template from upstream to the downstream destination
		templateFileBytes, err := os.ReadFile(filepath.Join(upstreamPath, templatedInstruction.Template))
		if err != nil {
			return fmt.Errorf("error reading upstream template %s: %v", templatedInstruction.Template, err)
		}
		t, err := template.New("").Parse(string(templateFileBytes))
		if err != nil {
			return fmt.Errorf("error parsing related template in upstream %s: %v", templatedInstruction.Template, err)
		}
		fullDestinationPath := filepath.Join(downstreamPath, templatedInstruction.Destination)
		fullDestinationDir := filepath.Dir(fullDestinationPath)

		if err := os.MkdirAll(fullDestinationDir, 0755); err != nil {
			return fmt.Errorf("error ensuring %s exists: %v", fullDestinationDir, err)
		}
		performPostMergeStructured := ""
		if templatedInstruction.Merged != nil && templatedInstruction.Merged.Structured != "" {
			if _, err := os.Stat(fullDestinationPath); err == nil {
				// if we have merge instruction present, and there's a file at the destination path already
				performPostMergeStructured = templatedInstruction.Merged.Structured
				if performPostMergeStructured != config.TemplatedMergeStructuredPreferUpstream && performPostMergeStructured != config.TemplatedMergeStructuredPreferDownstream {
					return fmt.Errorf("invalid templated merged.structured value %s, expects one of: %s, %s", performPostMergeStructured,
						config.TemplatedMergeStructuredPreferUpstream, config.TemplatedMergeStructuredPreferDownstream)
				}
			}
		}
		var renderedBytes bytes.Buffer
		err = t.Execute(&renderedBytes, templateData)
		if err != nil {
			return fmt.Errorf("error rendering template data: %v", err)
		}
		if performPostMergeStructured != "" {
			tmpDir, err := os.MkdirTemp("", config.GitSpork)
			if err != nil {
				return fmt.Errorf("error creating temp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)
			tmpFilePath := filepath.Join(tmpDir, filepath.Base(fullDestinationPath))
			if err := os.WriteFile(tmpFilePath, renderedBytes.Bytes(), 0644); err != nil {
				return fmt.Errorf("error writing rendered template to temporary location: %v", err)
			}
			newData, existingData, structuredDataType, err := getStructuredData(tmpFilePath, fullDestinationPath)
			if err != nil {
				return fmt.Errorf("error loading structured data from existing/new template render process in %s: %v", templatedInstruction.Template, err)
			}
			var merged *node
			if performPostMergeStructured == config.TemplatedMergeStructuredPreferDownstream {
				merged = mergeNodes(newData, existingData, true)
			} else {
				merged = mergeNodes(existingData, newData, true)
			}
			if err := writeStructuredData(merged, structuredDataType, fullDestinationPath); err != nil {
				return fmt.Errorf("error writing merged structured data in templated instruction from %s: %v", templatedInstruction.Template, err)
			}
		} else {
			if err := os.WriteFile(fullDestinationPath, renderedBytes.Bytes(), 0644); err != nil {
				return fmt.Errorf("error writing rendered templated file from instruction %s: %v", templatedInstruction.Destination, err)
			}
		}

		nextCache[templatedInstruction.Destination] = templateData.Inputs
	}

	if err := saveTemplatedInputs(downstreamPath, nextCache); err != nil {
		return fmt.Errorf("error writing templated inputs cache: %v", err)
	}
	if err := ensureGitsporkAttributes(downstreamPath); err != nil {
		return fmt.Errorf("error ensuring .gitattributes entry for templated cache: %v", err)
	}
	return nil
}
