package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/redhat-et/copilot-ops/pkg/filemap"
	"github.com/redhat-et/copilot-ops/pkg/openai"
	"github.com/spf13/cobra"
)

// NewGenerateCmd creates the `copilot-ops patch` CLI command
func NewGenerateCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use: COMMAND_GENERATE,

		Short: "Proposes a new files to the repo",

		Long: "Generate takes a request in natural language, packs the related files from the repo, calls AI engine to suggest generating new code based on the request, and optionally applies the suggested changes to the repo.",

		Example: `  copilot-ops generate --file examples/app1/mysql-pvc.yaml --request 'Generate a pod that mounts the PVC. Set the pod resources requests and limits to 4 cpus and 5 Gig of memory.'`,

		RunE: RunGenerate,
	}

	AddRequestFlags(cmd)

	// generate-specific flags
	cmd.Flags().StringArrayP(
		FLAG_FILES, "f", []string{},
		"File paths (glob) to be considered for the patch (can be specified multiple times)",
	)

	cmd.Flags().StringArrayP(
		FLAG_FILESETS, "s", []string{},
		"Fileset names (defined in "+CONFIG_FILE+") to be considered for the patch (can be specified multiple times)",
	)

	cmd.Flags().Int32P(
		FLAG_NTOKENS, "n", 512,
		"Max number of tokens to generate",
	)

	return cmd
}

// RunGenerate is the implementation of the `copilot-ops patch` command
func RunGenerate(cmd *cobra.Command, args []string) error {

	r, err := PrepareRequest(cmd, openai.OpenAICodeDavinciV2)
	if err != nil {
		return err
	}

	input := PrepareGenerateInput(r.UserRequest, r.FilemapText)
	log.Printf("requesting generate from OpenAI: %s", input)

	// generate a response from OpenAI
	output, err := r.OpenAI.GenerateCode(input)
	if err != nil {
		return err
	}

	// print w/ escaped newlines
	if !r.IsWrite {
		log.Printf("received output from OpenAI: %s", strings.ReplaceAll(output, "\\n", "\n"))
	}

	// err = r.Filemap.DecodeFromOutput(output)
	r.Filemap = filemap.NewFilemap()
	log.Printf("decoding output")
	err = r.Filemap.DecodeFromOutput(output)
	if err == nil {
		return PrintOrWriteOut(r)
	}

	// HACK: try other way to decode the output to a fileset
	log.Printf("decoding failed, got error: %s", err)
	log.Printf("trying fallback")

	// fallback - generate a new filename and put the content inside
	newFileName := "generated-by-copilot-ops"
	r.Filemap.Files = map[string]filemap.File{
		newFileName: {Path: newFileName, Content: output, Tag: newFileName},
	}

	return PrintOrWriteOut(r)
}

// PrepareGenerateInput Accepts the userInput and all of the files encoded as a string,
// and formats them as a prompt to be sent off to OpenAI.
func PrepareGenerateInput(userInput string, encodedFiles string) string {
	// HACK: prompt wording needs to be adjusted to improve accuracy
	var prompt string = ""
	var numInstructions int8 = 1

	// preamble
	prompt += fmt.Sprintf(`## This document contains:
## %d. Instructions describing how to generate a new Kubernetes YAML`, numInstructions)
	numInstructions++

	// include the encodedFiles if they are non-empty when stripped
	if strings.TrimSpace(encodedFiles) != "" {
		prompt += fmt.Sprintf(`
## %d. The existing YAMLs, each separated by a '%s'`, numInstructions, filemap.FileDelimeter)
		numInstructions++
	}

	// instruction for the generated code
	prompt += fmt.Sprintf(`
## %d. The new YAML, terminated by an '%s'`, numInstructions, openai.CompletionEndOfSequence)
	prompt += "\n"

	// reset counter
	numInstructions = 1

	// add the user input
	prompt += fmt.Sprintf(`
## %d. Instructions for the new Kubernetes object:
%s
`, numInstructions, userInput)
	numInstructions++

	// add the encoded files if they exist
	if strings.TrimSpace(encodedFiles) != "" {
		prompt += fmt.Sprintf(`
## %d. Existing YAMLs:
%s
`, numInstructions, encodedFiles)
		numInstructions++
	}

	// add the completion sequence
	prompt += fmt.Sprintf(`
## %d. The new YAML:
`, numInstructions)
	return prompt
}
