package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type modelInstallOptions struct {
	remoteOptions
	requestPath string
	sourceType  string
	source      string
	tokenStdin  bool
}

func (c *CLI) newModelsInstallCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := modelInstallOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use: "install", Short: "Install a model from an exact URL", Args: cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationModelsInstall},
		Run: func(cmd *cobra.Command, _ []string) {
			if options.requestPath == "-" && options.tokenStdin {
				*exitCode = c.fail(result.OperationModelsInstall, *jsonOutput, result.CodeInvalidRequest, "--request - cannot be combined with --token-stdin", nil)
				return
			}
			options.captureRemoteFlags(cmd)
			request := models.InstallRequest{SchemaVersion: 1, Source: models.InstallSource{Type: options.sourceType, Reference: options.source}}
			execution := remoteExecution[models.InstallRequest, models.InstallResult]{
				operation: result.OperationModelsInstall, connection: options.remoteOptions, request: request,
				requestPath:       options.requestPath,
				operationFlagsSet: cmd.Flags().Changed("source-type") || cmd.Flags().Changed("source"),
				invoke: func(ctx context.Context, client *httpclient.Client, request models.InstallRequest) (models.InstallResult, error) {
					if options.tokenStdin {
						data, err := io.ReadAll(c.stdin)
						if err != nil {
							return models.InstallResult{}, operation.InvalidRequest("could not read source token from standard input")
						}
						request.SourceToken = strings.TrimSpace(string(data))
						if request.SourceToken == "" {
							return models.InstallResult{}, operation.InvalidRequest("source token must not be empty")
						}
					}
					return models.Install(ctx, client, request)
				}, render: renderInstallResult,
			}
			*exitCode = execution.run(cmd.Context(), c, *jsonOutput)
		},
	}
	addRemoteFlags(command, &options.remoteOptions, requestTimeoutUsage)
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().StringVar(&options.sourceType, "source-type", "", "model source type (url)")
	command.Flags().StringVar(&options.source, "source", "", "exact HTTP(S) artifact URL")
	command.Flags().BoolVar(&options.tokenStdin, "token-stdin", false, "read a temporary source access token from standard input")
	return command
}

func renderInstallResult(value models.InstallResult, writer io.Writer) error {
	for _, job := range value.Jobs {
		if _, err := fmt.Fprintf(writer, "Current install job %d: %s\n", job.JobID, job.Status); err != nil {
			return err
		}
	}
	return nil
}

type modelStatusOptions struct {
	remoteOptions
	requestPath string
	jobID       int
}

func (c *CLI) newModelsStatusCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := modelStatusOptions{remoteOptions: defaultRemoteOptions()}
	command := &cobra.Command{
		Use: "status", Short: "Inspect a current InvokeAI model installation job", Args: cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationModelsStatus},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			request := models.StatusRequest{SchemaVersion: 1}
			if cmd.Flags().Changed("job-id") {
				request.JobID = new(options.jobID)
			}
			execution := remoteExecution[models.StatusRequest, models.StatusResult]{
				operation: result.OperationModelsStatus, connection: options.remoteOptions, request: request,
				requestPath: options.requestPath, operationFlagsSet: cmd.Flags().Changed("job-id"),
				invoke: models.Status, render: renderStatusResult,
			}
			*exitCode = execution.run(cmd.Context(), c, *jsonOutput)
		},
	}
	addRemoteFlags(command, &options.remoteOptions, requestTimeoutUsage)
	command.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.Flags().IntVar(&options.jobID, "job-id", 0, "current InvokeAI installation job ID")
	return command
}

func renderStatusResult(value models.StatusResult, writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "Current install job %d: %s\n", value.JobID, value.Status)
	if err != nil {
		return err
	}
	if value.ModelKey != "" {
		_, err = fmt.Fprintf(writer, "Installed model key: %s\n", value.ModelKey)
	}
	return err
}
