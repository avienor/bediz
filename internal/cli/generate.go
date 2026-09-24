package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/synchronization"
	"github.com/spf13/cobra"
)

type generateOptions struct {
	remoteOptions
	requestPath    string
	noWait         bool
	waitTimeout    time.Duration
	model          string
	profile        string
	prompt         string
	negativePrompt string
	width          int
	height         int
	steps          int
	scheduler      string
	guidance       float64
	seed           uint32
	outputCount    int
	boardID        string
	vae            string
	qwen3Encoder   string
	t5Encoder      string
	clipEmbed      string
}

func (c *CLI) newGenerateCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := generateOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use:         "generate",
		Short:       "Generate images with InvokeAI",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationGenerate},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			*exitCode = c.executeGenerate(cmd.Context(), *jsonOutput, cmd, options)
		},
	}
	addConnectionFlags(command, &options.remoteOptions)
	command.Flags().DurationVar(&options.waitTimeout, "timeout", 0, "total local wait timeout; zero waits until the queue item reaches a terminal state")
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().BoolVar(&options.noWait, "no-wait", false, "return after InvokeAI accepts the request")
	command.Flags().StringVar(&options.model, "model", "", "supported main model key or unique name")
	command.Flags().StringVar(&options.profile, "profile", "", "local Generation Profile name")
	command.Flags().StringVar(&options.prompt, "prompt", "", "positive prompt")
	command.Flags().StringVar(&options.negativePrompt, "negative-prompt", "", "negative prompt")
	command.Flags().IntVar(&options.width, "width", 0, "output width")
	command.Flags().IntVar(&options.height, "height", 0, "output height")
	command.Flags().IntVar(&options.steps, "steps", 0, "denoising steps")
	command.Flags().StringVar(&options.scheduler, "scheduler", "", "model family scheduler")
	command.Flags().Float64Var(&options.guidance, "guidance", 0, "model family guidance scale")
	command.Flags().Uint32Var(&options.seed, "seed", 0, "generation seed")
	command.Flags().IntVar(&options.outputCount, "output-count", 0, "number of outputs")
	command.Flags().StringVar(&options.boardID, "board", "", "exact output board id")
	command.Flags().StringVar(&options.vae, "vae", "", "applicable VAE key or unique name")
	command.Flags().StringVar(&options.qwen3Encoder, "qwen3-encoder", "", "Qwen3 encoder key or unique name")
	command.Flags().StringVar(&options.t5Encoder, "t5-encoder", "", "T5 encoder key or unique name")
	command.Flags().StringVar(&options.clipEmbed, "clip-embed", "", "CLIP Embed key or unique name")
	return command
}

func (c *CLI) executeGenerate(ctx context.Context, jsonOutput bool, command *cobra.Command, options generateOptions) int {
	if options.waitTimeout < 0 {
		return c.fail(result.OperationGenerate, jsonOutput, result.CodeInvalidRequest, "timeout cannot be negative", nil)
	}
	if options.noWait && options.waitTimeout > 0 {
		return c.fail(result.OperationGenerate, jsonOutput, result.CodeInvalidRequest, "--timeout cannot be combined with --no-wait", nil)
	}
	operationFlags := []string{
		"model", "profile", "prompt", "negative-prompt", "width", "height", "steps", "scheduler", "guidance",
		"seed", "output-count", "board", "vae", "qwen3-encoder", "t5-encoder", "clip-embed",
	}
	fieldsSet := false
	for _, name := range operationFlags {
		fieldsSet = fieldsSet || command.Flags().Changed(name)
	}
	var request generation.Request
	if fieldsSet {
		request.SchemaVersion = 1
		request.Model = options.model
		request.Profile = options.profile
		request.PositivePrompt = options.prompt
		request.NegativePrompt = options.negativePrompt
		request.BoardID = options.boardID
		if command.Flags().Changed("width") {
			request.Width = new(options.width)
		}
		if command.Flags().Changed("height") {
			request.Height = new(options.height)
		}
		if command.Flags().Changed("steps") {
			request.Steps = new(options.steps)
		}
		if command.Flags().Changed("scheduler") {
			request.Scheduler = new(options.scheduler)
		}
		if command.Flags().Changed("guidance") {
			request.Guidance = new(options.guidance)
		}
		if command.Flags().Changed("seed") {
			request.Seed = new(options.seed)
		}
		if command.Flags().Changed("output-count") {
			request.OutputCount = new(options.outputCount)
		}
		if command.Flags().Changed("vae") || command.Flags().Changed("qwen3-encoder") || command.Flags().Changed("t5-encoder") || command.Flags().Changed("clip-embed") {
			request.Components = &generation.Components{}
			if command.Flags().Changed("vae") {
				request.Components.VAE = new(options.vae)
			}
			if command.Flags().Changed("qwen3-encoder") {
				request.Components.Qwen3Encoder = new(options.qwen3Encoder)
			}
			if command.Flags().Changed("t5-encoder") {
				request.Components.T5Encoder = new(options.t5Encoder)
			}
			if command.Flags().Changed("clip-embed") {
				request.Components.CLIPEmbed = new(options.clipEmbed)
			}
		}
	}
	execution := remoteExecution[generation.Request, generation.ExecutionReceipt]{
		operation:         result.OperationGenerate,
		connection:        options.remoteOptions,
		request:           request,
		requestPath:       options.requestPath,
		operationFlagsSet: fieldsSet,
		invoke: func(ctx context.Context, client *httpclient.Client, request generation.Request) (generation.ExecutionReceipt, error) {
			accepted, err := generation.Submit(ctx, client, request)
			if err != nil {
				return accepted, err
			}
			accepted = synchronization.Synchronize(ctx, client, accepted)
			if options.noWait {
				return accepted, nil
			}
			return generation.Wait(ctx, client, accepted, generation.WaitOptions{Timeout: options.waitTimeout})
		},
		render:   renderExecutionReceipt,
		warnings: func(receipt generation.ExecutionReceipt) []result.Warning { return receipt.Warnings },
	}
	return execution.run(ctx, c, jsonOutput)
}

// renderExecutionReceipt writes the accepted queue position of a --no-wait
// result or the completed image of a generation that waited for its item.
func renderExecutionReceipt(receipt generation.ExecutionReceipt, w io.Writer) error {
	if len(receipt.Outputs) == 0 {
		_, err := fmt.Fprintf(w, "Accepted batch %s in queue %s (item %d)\n", receipt.Queue.BatchID, receipt.Queue.QueueID, receipt.Queue.ItemIDs[0])
		return err
	}
	for _, output := range receipt.Outputs {
		if _, err := fmt.Fprintf(w, "Generated %s for seed %d from queue item %d\n", output.Image.ImageName, output.Seed, output.ItemID); err != nil {
			return err
		}
	}
	return nil
}
