package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/avienor/bediz/internal/recall"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type recallOptions struct {
	remoteOptions
	requestPath    string
	model          string
	prompt         string
	negativePrompt string
	width          int
	height         int
	steps          int
	seed           uint32
}

func (c *CLI) newRecallCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := recallOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use: "recall", Short: "Submit a generation parameter patch to InvokeAI",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationRecall},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			*exitCode = c.executeRecall(cmd.Context(), *jsonOutput, cmd, options)
		},
	}
	addRemoteFlags(command, &options.remoteOptions, requestTimeoutUsage)
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().StringVar(&options.model, "model", "", "Anima main model key or unique name")
	command.Flags().StringVar(&options.prompt, "prompt", "", "positive prompt")
	command.Flags().StringVar(&options.negativePrompt, "negative-prompt", "", "negative prompt")
	command.Flags().IntVar(&options.width, "width", 0, "output width")
	command.Flags().IntVar(&options.height, "height", 0, "output height")
	command.Flags().IntVar(&options.steps, "steps", 0, "denoising steps")
	command.Flags().Uint32Var(&options.seed, "seed", 0, "generation seed")
	return command
}

func (c *CLI) executeRecall(ctx context.Context, jsonOutput bool, command *cobra.Command, options recallOptions) int {
	request := recall.Request{SchemaVersion: 1}
	flagsSet := false
	for _, name := range []string{"model", "prompt", "negative-prompt", "width", "height", "steps", "seed"} {
		flagsSet = flagsSet || command.Flags().Changed(name)
	}
	if flagsSet {
		request.Model = options.model
		if command.Flags().Changed("prompt") {
			request.PositivePrompt = new(options.prompt)
		}
		if command.Flags().Changed("negative-prompt") {
			request.NegativePrompt = new(options.negativePrompt)
		}
		if command.Flags().Changed("width") {
			request.Width = new(options.width)
		}
		if command.Flags().Changed("height") {
			request.Height = new(options.height)
		}
		if command.Flags().Changed("steps") {
			request.Steps = new(options.steps)
		}
		if command.Flags().Changed("seed") {
			request.Seed = new(options.seed)
		}
	}
	execution := remoteExecution[recall.Request, recall.Result]{
		operation: result.OperationRecall, connection: options.remoteOptions,
		request: request, requestPath: options.requestPath, operationFlagsSet: flagsSet,
		invoke: recall.Submit,
		render: func(_ recall.Result, w io.Writer) error {
			_, err := fmt.Fprintln(w, "Recall patch submitted to InvokeAI.")
			return err
		},
	}
	return execution.run(ctx, c, jsonOutput)
}
