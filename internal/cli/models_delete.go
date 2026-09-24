package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type modelDeleteOptions struct {
	remoteOptions
	requestPath string
	yes         bool
}

func (c *CLI) newModelsDeleteCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := modelDeleteOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use:         "delete MODEL_KEY --yes",
		Short:       "Delete one installed model by its exact model key",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationModelsDelete},
		Run: func(cmd *cobra.Command, args []string) {
			options.captureRemoteFlags(cmd)
			if options.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail(result.OperationModelsDelete, *jsonOutput, result.CodeInvalidRequest, "model key cannot be combined with --request", nil)
				return
			}
			if options.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationModelsDelete, *jsonOutput, result.CodeInvalidRequest, "model key or --request is required", nil)
				return
			}
			modelKey := ""
			if len(args) == 1 {
				modelKey = args[0]
			}
			*exitCode = c.executeModelsDelete(cmd.Context(), *jsonOutput, options, modelKey)
		},
	}
	addRemoteFlags(command, &options.remoteOptions, requestTimeoutUsage)
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().BoolVar(&options.yes, "yes", false, "approve deleting the model and any files InvokeAI manages for it")
	return command
}

func (c *CLI) executeModelsDelete(ctx context.Context, jsonOutput bool, options modelDeleteOptions, modelKey string) int {
	execution := remoteExecution[models.DeleteRequest, models.DeleteResult]{
		operation:   result.OperationModelsDelete,
		connection:  options.remoteOptions,
		request:     models.DeleteRequest{SchemaVersion: 1, ModelKey: modelKey},
		requestPath: options.requestPath,
		invoke: func(ctx context.Context, client *httpclient.Client, request models.DeleteRequest) (models.DeleteResult, error) {
			request.Approved = options.yes
			return models.Delete(ctx, client, request)
		},
		render: func(deleted models.DeleteResult, w io.Writer) error {
			_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", deleted.Key, deleted.Name, deleted.Base, deleted.Type, deleted.Format)
			return err
		},
	}
	return execution.run(ctx, c, jsonOutput)
}
