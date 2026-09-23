package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/upscale"
	"github.com/spf13/cobra"
)

type upscaleOptions struct {
	remoteOptions
	requestPath    string
	noWait         bool
	waitTimeout    time.Duration
	image          string
	imagePath      string
	model          string
	prompt         string
	negativePrompt string
	scale          int
	creativity     int
	structure      int
	steps          int
	scheduler      string
	guidance       float64
	seed           uint32
	tileSize       int
	tileOverlap    int
	boardID        string
	upscaleModel   string
	tileControlNet string
	vae            string
}

func (c *CLI) newUpscaleCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := upscaleOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use:         "upscale",
		Short:       "Generatively upscale an InvokeAI image",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationUpscale},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			*exitCode = c.executeUpscale(cmd.Context(), *jsonOutput, cmd, options)
		},
	}
	addConnectionFlags(command, &options.remoteOptions)
	command.Flags().DurationVar(&options.waitTimeout, "timeout", 0, "total local wait timeout; zero waits until the queue item reaches a terminal state")
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().BoolVar(&options.noWait, "no-wait", false, "return after InvokeAI accepts the request")
	command.Flags().StringVar(&options.image, "image", "", "existing InvokeAI image name")
	command.Flags().StringVar(&options.imagePath, "image-path", "", "local source image path (not supported yet)")
	command.Flags().StringVar(&options.model, "model", "", "SD1.5 or SDXL main model key or unique name")
	command.Flags().StringVar(&options.prompt, "prompt", "", "positive prompt")
	command.Flags().StringVar(&options.negativePrompt, "negative-prompt", "", "negative prompt")
	command.Flags().IntVar(&options.scale, "scale", 0, "upscale factor: 2, 4, or 8")
	command.Flags().IntVar(&options.creativity, "creativity", 0, "creativity from -10 to 10")
	command.Flags().IntVar(&options.structure, "structure", 0, "structure from -10 to 10")
	command.Flags().IntVar(&options.steps, "steps", 0, "denoising steps")
	command.Flags().StringVar(&options.scheduler, "scheduler", "", "scheduler")
	command.Flags().Float64Var(&options.guidance, "guidance", 0, "CFG scale")
	command.Flags().Uint32Var(&options.seed, "seed", 0, "upscale seed")
	command.Flags().IntVar(&options.tileSize, "tile-size", 0, "tile size")
	command.Flags().IntVar(&options.tileOverlap, "tile-overlap", 0, "tile overlap")
	command.Flags().StringVar(&options.boardID, "board", "", "exact output board id")
	command.Flags().StringVar(&options.upscaleModel, "upscale-model", "", "Spandrel upscale model key or unique name")
	command.Flags().StringVar(&options.tileControlNet, "tile-controlnet", "", "Tile ControlNet key or unique name")
	command.Flags().StringVar(&options.vae, "vae", "", "VAE key or unique name matching the main model base")
	return command
}

func (c *CLI) executeUpscale(ctx context.Context, jsonOutput bool, command *cobra.Command, options upscaleOptions) int {
	if options.waitTimeout < 0 {
		return c.fail(result.OperationUpscale, jsonOutput, result.CodeInvalidRequest, "timeout cannot be negative", nil)
	}
	if options.noWait && options.waitTimeout > 0 {
		return c.fail(result.OperationUpscale, jsonOutput, result.CodeInvalidRequest, "--timeout cannot be combined with --no-wait", nil)
	}
	if command.Flags().Changed("image") && command.Flags().Changed("image-path") {
		return c.fail(result.OperationUpscale, jsonOutput, result.CodeInvalidRequest, "--image and --image-path cannot be combined", nil)
	}
	operationFlags := []string{
		"image", "image-path", "model", "prompt", "negative-prompt", "scale", "creativity", "structure", "steps",
		"scheduler", "guidance", "seed", "tile-size", "tile-overlap", "board", "upscale-model", "tile-controlnet", "vae",
	}
	fieldsSet := false
	for _, name := range operationFlags {
		fieldsSet = fieldsSet || command.Flags().Changed(name)
	}
	request := upscale.Request{}
	if fieldsSet {
		request.SchemaVersion = 1
		request.Source = upscale.Source{Type: "image", Reference: options.image}
		if command.Flags().Changed("image-path") {
			request.Source = upscale.Source{Type: "path", Reference: options.imagePath}
		}
		request.Model = options.model
		request.PositivePrompt = options.prompt
		request.NegativePrompt = options.negativePrompt
		request.BoardID = options.boardID
		if command.Flags().Changed("scale") {
			request.Scale = new(options.scale)
		}
		if command.Flags().Changed("creativity") {
			request.Creativity = new(options.creativity)
		}
		if command.Flags().Changed("structure") {
			request.Structure = new(options.structure)
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
		if command.Flags().Changed("tile-size") {
			request.TileSize = new(options.tileSize)
		}
		if command.Flags().Changed("tile-overlap") {
			request.TileOverlap = new(options.tileOverlap)
		}
		if command.Flags().Changed("upscale-model") || command.Flags().Changed("tile-controlnet") || command.Flags().Changed("vae") {
			request.Components = &upscale.Components{}
			if command.Flags().Changed("upscale-model") {
				request.Components.UpscaleModel = new(options.upscaleModel)
			}
			if command.Flags().Changed("tile-controlnet") {
				request.Components.TileControlNet = new(options.tileControlNet)
			}
			if command.Flags().Changed("vae") {
				request.Components.VAE = new(options.vae)
			}
		}
	}
	execution := remoteExecution[upscale.Request, upscale.ExecutionReceipt]{
		operation:         result.OperationUpscale,
		connection:        options.remoteOptions,
		request:           request,
		requestPath:       options.requestPath,
		operationFlagsSet: fieldsSet,
		invoke: func(ctx context.Context, client *httpclient.Client, request upscale.Request) (upscale.ExecutionReceipt, error) {
			accepted, err := upscale.Submit(ctx, client, request)
			if err != nil {
				return accepted, err
			}
			if options.noWait {
				return accepted, nil
			}
			return upscale.Wait(ctx, client, accepted, upscale.WaitOptions{Timeout: options.waitTimeout})
		},
		render:   renderUpscaleReceipt,
		warnings: func(receipt upscale.ExecutionReceipt) []result.Warning { return receipt.Warnings },
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderUpscaleReceipt(receipt upscale.ExecutionReceipt, w io.Writer) error {
	if len(receipt.Outputs) == 0 {
		_, err := fmt.Fprintf(w, "Accepted upscale batch %s in queue %s (item %d)\n", receipt.Queue.BatchID, receipt.Queue.QueueID, receipt.Queue.ItemIDs[0])
		return err
	}
	output := receipt.Outputs[0]
	_, err := fmt.Fprintf(w, "Upscaled %s for seed %d from queue item %d\n", output.Image.ImageName, output.Seed, output.ItemID)
	return err
}
