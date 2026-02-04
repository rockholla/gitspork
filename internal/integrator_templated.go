package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"dario.cat/mergo"
	inputpkg "github.com/rockholla/gitspork/internal/input"
)

const (
	templatedMergeStructuredPreferUpstream   = "prefer-upstream"
	templatedMergeStructuredPreferDownstream = "prefer-downstream"
)

// IntegratorTemplated will process a list of instructions on how to render Go templates in the upstream to downstream rendered files
type IntegratorTemplated struct{}

// IntegratorTemplateData is the common data interface passed into templates
type IntegratorTemplatedData struct {
	Inputs map[string]string `json:"inputs"`
}

// Integrate will process the gitspork files list to ensure integration b/w upstream -> downstream
func (i *IntegratorTemplated) Integrate(templatedInstructions []GitSporkConfigTemplated, upstreamPath string, downstreamPath string, forceRePrompt bool, logger *Logger) error {

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
	for _, templatedInstruction := range templatedInstructions {
		logger.Log("📄 executing templated instruction for rendering upstream template %s to downstream location %s", templatedInstruction.Template, templatedInstruction.Destination)

		capturedInputValues[templatedInstruction.Template] = map[string]string{}
		cachedTemplateDataFilePath := filepath.Join(downstreamPath, filepath.Join(fmt.Sprintf(".%s", gitSpork), fmt.Sprintf("%s.json", templatedInstruction.Destination)))
		templateData := IntegratorTemplatedData{
			Inputs: map[string]string{},
		}
		if _, err := os.Stat(cachedTemplateDataFilePath); err == nil {
			// cached data path is there, we'll try to load it into inputs to pre-populate from pre-existing awareness of this data
			jsonData, err := os.ReadFile(cachedTemplateDataFilePath)
			if err != nil {
				return fmt.Errorf("error reading cached template data file at %s: %v", strings.TrimLeft(strings.TrimLeft(cachedTemplateDataFilePath, downstreamPath), "/"), err)
			}
			err = json.Unmarshal(jsonData, &templateData)
			if err != nil {
				return fmt.Errorf("error parsing cached template data file at %s into inputs: %v", strings.TrimLeft(strings.TrimLeft(cachedTemplateDataFilePath, downstreamPath), "/"), err)
			}
		}
		// we'll begin by gathering inputs to start
		for _, input := range templatedInstruction.Inputs {
			if input.JSONDataPath != "" {
				jsonDataPath := filepath.Join(downstreamPath, input.JSONDataPath)
				jsonData, err := os.ReadFile(jsonDataPath)
				if err != nil {
					return fmt.Errorf("error reading json_data_path at %s: %v", jsonDataPath, err)
				}
				err = json.Unmarshal(jsonData, &templateData.Inputs)
				maps.Copy(capturedInputValues[templatedInstruction.Template], templateData.Inputs)
				if err != nil {
					return fmt.Errorf("error parsing json_data_path file %s into inputs: %v", jsonDataPath, err)
				}
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
				if performPostMergeStructured != templatedMergeStructuredPreferUpstream && performPostMergeStructured != templatedMergeStructuredPreferDownstream {
					return fmt.Errorf("invalid templated merged.structured value %s, expects one of: %s, %s", performPostMergeStructured,
						templatedMergeStructuredPreferUpstream, templatedMergeStructuredPreferDownstream)
				}
			}
		}
		var renderedBytes bytes.Buffer
		err = t.Execute(&renderedBytes, templateData)
		if err != nil {
			return fmt.Errorf("error rendering template data: %v", err)
		}
		if performPostMergeStructured != "" {
			tmpDir, err := os.MkdirTemp("", gitSpork)
			if err != nil {
				return fmt.Errorf("error creating temp directory: %v", err)
			}
			defer os.RemoveAll(tmpDir)
			tmpFilePath := filepath.Join(tmpDir, filepath.Base(fullDestinationPath))
			if err := os.WriteFile(tmpFilePath, renderedBytes.Bytes(), 0644); err != nil {
				return fmt.Errorf("error writing rendered template to temporary location: %v", err)
			}
			newData, existingData, structuredDataType, err := getStructuredData(tmpFilePath, fullDestinationPath)
			var preferredData *map[string]any
			var secondaryData *map[string]any
			if err != nil {
				return fmt.Errorf("error loading structured data from existing/new template render process in %s: %v", templatedInstruction.Template, err)
			}
			if performPostMergeStructured == templatedMergeStructuredPreferDownstream {
				preferredData = existingData
				secondaryData = newData
			} else {
				preferredData = newData
				secondaryData = existingData
			}
			if err := mergo.Merge(secondaryData, *preferredData, mergo.WithOverride); err != nil {
				return fmt.Errorf("error merging structured data in templated instruction from %s: %v", templatedInstruction.Template, err)
			}
			if err := writeStructuredData(preferredData, structuredDataType, fullDestinationPath); err != nil {
				return fmt.Errorf("error writing merged structured data in templated instruction from %s: %v", templatedInstruction.Template, err)
			}
		} else {
			if err := os.WriteFile(fullDestinationPath, renderedBytes.Bytes(), 0644); err != nil {
				return fmt.Errorf("error writing rendered templated file from instruction %s: %v", templatedInstruction.Destination, err)
			}
		}

		// caching input data in the path for later runs, will respect this data moving forward unless instructed to re-prompt
		templateDataBytes, err := json.Marshal(templateData)
		if err != nil {
			return fmt.Errorf("error marshaling template data: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(cachedTemplateDataFilePath), 0755); err != nil {
			return fmt.Errorf("error creating template data cache directory at %s: %v", strings.TrimLeft(strings.TrimLeft(cachedTemplateDataFilePath, downstreamPath), "/"), err)
		}
		if err := os.WriteFile(cachedTemplateDataFilePath, templateDataBytes, 0644); err != nil {
			return fmt.Errorf("error writing cached templated data to %s: %v", strings.TrimLeft(strings.TrimLeft(cachedTemplateDataFilePath, downstreamPath), "/"), err)
		}
	}

	return nil
}
