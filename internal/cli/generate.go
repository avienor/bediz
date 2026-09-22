package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/avienor/bediz/internal/generation"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type generateOptions struct {
	remoteOptions
	requestPath    string
	noWait         bool
	waitTimeout    time.Duration
	model          string
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
	command.Flags().StringVar(&options.model, "model", "", "Anima main model key or unique name")
	command.Flags().StringVar(&options.prompt, "prompt", "", "positive prompt")
	command.Flags().StringVar(&options.negativePrompt, "negative-prompt", "", "negative prompt")
	command.Flags().IntVar(&options.width, "width", 0, "output width")
	command.Flags().IntVar(&options.height, "height", 0, "output height")
	command.Flags().IntVar(&options.steps, "steps", 0, "denoising steps")
	command.Flags().StringVar(&options.scheduler, "scheduler", "", "Anima scheduler")
	command.Flags().Float64Var(&options.guidance, "guidance", 0, "Anima guidance scale")
	command.Flags().Uint32Var(&options.seed, "seed", 0, "generation seed")
	command.Flags().IntVar(&options.outputCount, "output-count", 0, "number of outputs")
	command.Flags().StringVar(&options.boardID, "board", "", "exact output board id")
	command.Flags().StringVar(&options.vae, "vae", "", "Anima VAE key or unique name")
	command.Flags().StringVar(&options.qwen3Encoder, "qwen3-encoder", "", "Qwen3 encoder key or unique name")
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
		"model", "prompt", "negative-prompt", "width", "height", "steps", "scheduler", "guidance",
		"seed", "output-count", "board", "vae", "qwen3-encoder",
	}
	fieldsSet := false
	for _, name := range operationFlags {
		fieldsSet = fieldsSet || command.Flags().Changed(name)
	}
	request := generation.Request{SchemaVersion: 1}
	if fieldsSet {
		request.Model = options.model
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
		if command.Flags().Changed("vae") || command.Flags().Changed("qwen3-encoder") {
			request.Components = &generation.Components{VAE: options.vae, Qwen3Encoder: options.qwen3Encoder}
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
			if err != nil || options.noWait {
				return accepted, err
			}
			return generation.Wait(ctx, client, accepted, generation.WaitOptions{Timeout: options.waitTimeout})
		},
		render: renderExecutionReceipt,
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
