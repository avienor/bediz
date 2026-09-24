package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type modelScanOptions struct {
	remoteOptions
	requestPath string
	path        string
}

func (c *CLI) newModelsScanCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := modelScanOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use:         "scan",
		Short:       "Find model files in a folder on the InvokeAI server",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationModelsScan},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			*exitCode = c.executeModelsScan(cmd.Context(), *jsonOutput, options, cmd.Flags().Changed("path"))
		},
	}
	addRemoteFlags(command, &options.remoteOptions, requestTimeoutUsage)
	command.Flags().StringVar(&options.path, "path", "", "absolute folder path on the InvokeAI server")
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	return command
}

func (c *CLI) executeModelsScan(ctx context.Context, jsonOutput bool, options modelScanOptions, pathSet bool) int {
	execution := remoteExecution[models.ScanRequest, models.ScanResult]{
		operation:         result.OperationModelsScan,
		connection:        options.remoteOptions,
		request:           models.ScanRequest{SchemaVersion: 1, Path: options.path},
		requestPath:       options.requestPath,
		operationFlagsSet: pathSet,
		invoke:            models.Scan,
		render: func(value models.ScanResult, writer io.Writer) error {
			for _, model := range value.Models {
				if _, err := fmt.Fprintf(writer, "%s\t%t\n", model.Path, model.Installed); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return execution.run(ctx, c, jsonOutput)
}
