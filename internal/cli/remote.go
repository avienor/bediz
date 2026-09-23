package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/avienor/bediz/internal/config"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
	queueops "github.com/avienor/bediz/internal/queue"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
	"github.com/spf13/cobra"
)

// requestTimeoutUsage describes a deadline that bounds one InvokeAI request.
const requestTimeoutUsage = "request timeout"

// remoteOptions carries the connection settings every implemented remote
// command accepts. Flags, environment, and stored configuration resolve to one
// client through newRemoteClient.
type remoteOptions struct {
	url      string
	urlSet   bool
	token    string
	tokenSet bool
	timeout  time.Duration
}

func defaultRemoteOptions() remoteOptions {
	return remoteOptions{timeout: httpclient.DefaultTimeout}
}

// addConnectionFlags registers the InvokeAI connection flags shared by every
// implemented remote command.
func addConnectionFlags(command *cobra.Command, options *remoteOptions) {
	command.Flags().StringVar(&options.url, "url", "", "InvokeAI base URL")
	command.Flags().StringVar(&options.token, "token", "", "InvokeAI bearer token")
}

// addRemoteFlags registers the connection flags and the deadline that bounds one
// request or diagnostic run. Commands whose deadline bounds local waiting
// instead register --timeout themselves.
func addRemoteFlags(command *cobra.Command, options *remoteOptions, timeoutUsage string) {
	addConnectionFlags(command, options)
	command.Flags().DurationVar(&options.timeout, "timeout", httpclient.DefaultTimeout, timeoutUsage)
}

func (o *remoteOptions) captureRemoteFlags(command *cobra.Command) {
	o.urlSet = command.Flags().Changed("url")
	o.tokenSet = command.Flags().Changed("token")
}

// newRemoteClient resolves the active connection and builds the HTTP client
// every remote command uses. Details carry the configuration path when loading
// stored configuration fails so the failure envelope names the offending file.
func newRemoteClient(options remoteOptions) (*httpclient.Client, map[string]any, error) {
	path, err := config.Path()
	if err != nil {
		return nil, nil, err
	}
	fileConfig, err := config.Load(path)
	if err != nil {
		return nil, map[string]any{"path": path}, err
	}
	resolved, err := config.Resolve(
		config.Overrides{URL: options.url, URLSet: options.urlSet, Token: options.token, TokenSet: options.tokenSet},
		config.EnvironmentFrom(os.LookupEnv),
		fileConfig,
	)
	if err != nil {
		return nil, nil, err
	}
	client, err := httpclient.New(resolved.URL, resolved.Token, httpclient.Options{
		Timeout:   options.timeout,
		UserAgent: "bediz/" + version.Current().Version,
	})
	return client, nil, err
}

// remoteExecution is one implemented remote operation: its public name, the
// compiled request, the request document that may replace it, the connection
// settings, the domain call that performs it, and the renderer that writes its
// human-readable result. Every remote command reaches InvokeAI through run, so
// connection resolution, timeout validation, request document loading, failure
// classification, and result output stay identical across operations.
type remoteExecution[Request, Result any] struct {
	operation         string
	connection        remoteOptions
	request           Request
	requestPath       string
	operationFlagsSet bool
	invoke            func(context.Context, *httpclient.Client, Request) (Result, error)
	render            func(Result, io.Writer) error
	warnings          func(Result) []result.Warning
}

// run performs the operation end to end. Local problems are reported before
// any request is sent; a domain failure is classified by failRemote; a domain
// result is written as the single final envelope or as human-readable output.
func (e remoteExecution[Request, Result]) run(ctx context.Context, c *CLI, jsonOutput bool) int {
	if e.connection.timeout <= 0 {
		return c.fail(e.operation, jsonOutput, result.CodeInvalidRequest, "timeout must be positive", nil)
	}
	if e.requestPath != "" && e.operationFlagsSet {
		return c.fail(e.operation, jsonOutput, result.CodeInvalidRequest, "operation flags cannot be combined with --request", nil)
	}
	client, details, err := newRemoteClient(e.connection)
	if err != nil {
		return c.fail(e.operation, jsonOutput, result.CodeInvalidConfiguration, err.Error(), details)
	}
	if e.requestPath != "" {
		if err := c.loadRequestDocument(e.requestPath, &e.request); err != nil {
			return c.fail(e.operation, jsonOutput, result.CodeInvalidRequest, err.Error(), nil)
		}
	}
	value, err := e.invoke(ctx, client, e.request)
	if err != nil {
		return c.failRemote(e.operation, jsonOutput, err)
	}
	if jsonOutput {
		if e.warnings != nil {
			return c.writeResultWithWarnings(e.operation, value, e.warnings(value))
		}
		return c.writeResult(e.operation, value)
	}
	if err := e.render(value, c.stdout); err != nil {
		return c.fail(e.operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
	}
	if e.warnings != nil {
		for _, warning := range e.warnings(value) {
			if _, err := fmt.Fprintf(c.stderr, "%s: %s\n", warning.Code, warning.Message); err != nil {
				return c.fail(e.operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
			}
		}
	}
	return result.ExitSuccess
}

// failRemote maps one domain failure to its public structured error code. It is
// the single place command failures become structured error codes; doctor
// classifies its own diagnostic issues before it reports one.
func (c *CLI) failRemote(operationName string, jsonOutput bool, err error) int {
	if invalid, ok := errors.AsType[*operation.InvalidRequestError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeInvalidRequest, invalid.Error(), nil)
	}
	if unsupported, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeUnsupportedCapability, unsupported.Error(), nil)
	}
	if selection, ok := errors.AsType[*operation.SelectionRequiredError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeSelectionRequired, selection.Error(), map[string]any{
			"kind": selection.Kind, "selector": selection.Selector, "candidates": selection.Candidates,
		})
	}
	if missing, ok := errors.AsType[*operation.MissingComponentError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeMissingComponent, missing.Error(), map[string]any{
			"component_type":        missing.ComponentType,
			"required_base":         missing.RequiredBase,
			"required_type":         missing.RequiredType,
			"installation_guidance": missing.InstallationGuidance,
		})
	}
	if timeout, ok := errors.AsType[*operation.WaitTimeoutError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeWaitTimeout, timeout.Error(), queuePositionDetails(timeout.Position))
	}
	if interrupted, ok := errors.AsType[*operation.InterruptedError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeInterrupted, interrupted.Error(), queuePositionDetails(interrupted.Position))
	}
	if failure, ok := errors.AsType[*operation.ItemFailureError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeInvokeAIOperationFailed, failure.Error(), acceptedItemDetails(failure.Position, failure.ItemID, failure.Status))
	}
	if invalid, ok := errors.AsType[*operation.InvalidQueueResultError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeInvalidInvokeAIResponse, invalid.Error(), acceptedItemDetails(invalid.Position, invalid.ItemID, invalid.Status))
	}
	if _, ok := errors.AsType[*httpclient.OutcomeUnknownError](err); ok {
		if operationName == result.OperationModelsInstall {
			return c.fail(operationName, jsonOutput, result.CodeOutcomeUnknown, "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again", nil)
		}
		return c.fail(operationName, jsonOutput, result.CodeOutcomeUnknown, "InvokeAI may have accepted the operation; inspect remote state before retrying", nil)
	}
	if errors.Is(err, context.Canceled) {
		return c.fail(operationName, jsonOutput, result.CodeInterrupted, "operation was interrupted locally", nil)
	}
	if _, ok := errors.AsType[*httpclient.NetworkError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeConnectionFailed, "could not reach InvokeAI", nil)
	}
	if _, ok := errors.AsType[*httpclient.InvalidResponseError](err); ok {
		return c.fail(operationName, jsonOutput, result.CodeInvalidInvokeAIResponse, "InvokeAI returned an invalid response", nil)
	}
	if httpErr, ok := errors.AsType[*httpclient.HTTPError](err); ok {
		switch {
		case httpErr.AuthenticationFailure():
			return c.fail(operationName, jsonOutput, result.CodeAuthenticationFailed, "InvokeAI rejected authentication", map[string]any{"status": httpErr.StatusCode})
		case httpErr.StatusCode == http.StatusNotFound:
			return c.fail(operationName, jsonOutput, result.CodeNotFound, "the requested InvokeAI resource was not found", nil)
		default:
			return c.fail(operationName, jsonOutput, result.CodeInvokeAIOperationFailed, "InvokeAI rejected the operation", map[string]any{"status": httpErr.StatusCode})
		}
	}
	return c.fail(operationName, jsonOutput, result.CodeInvokeAIOperationFailed, err.Error(), nil)
}

// queuePositionDetails reports accepted remote work in the structured failure
// details of a wait-path error.
func queuePositionDetails(position operation.QueuePosition) map[string]any {
	return map[string]any{
		"queue_id": position.QueueID,
		"batch_id": position.BatchID,
		"item_ids": position.ItemIDs,
	}
}

// acceptedItemDetails reports accepted remote work and the queue item that
// produced the failure.
func acceptedItemDetails(position operation.QueuePosition, itemID int, status string) map[string]any {
	details := queuePositionDetails(position)
	details["item_id"] = itemID
	details["status"] = status
	return details
}

type queueListOptions struct {
	remoteOptions
	queueID     string
	offset      int
	limit       int
	requestPath string
	fieldsSet   bool
}

type queueGetOptions struct {
	remoteOptions
	queueID     string
	requestPath string
}

func (c *CLI) newQueueCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "queue",
		Short:       "Inspect and manage the InvokeAI queue",
		Annotations: map[string]string{operationAnnotation: result.OperationQueue},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationQueue, *jsonOutput, result.CodeInvalidRequest, "queue requires a subcommand", nil)
		},
	}
	options := queueListOptions{remoteOptions: defaultRemoteOptions(), queueID: "default", limit: 20}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List queue item summaries",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationQueueList},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("queue-id") || cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit")
			*exitCode = c.executeQueueList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions, requestTimeoutUsage)
	listCommand.Flags().StringVar(&options.queueID, "queue-id", "default", "exact InvokeAI queue id")
	listCommand.Flags().IntVar(&options.offset, "offset", 0, "number of matching queue items to skip")
	listCommand.Flags().IntVar(&options.limit, "limit", 20, "maximum number of queue items to return")
	listCommand.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(listCommand)

	getOptions := queueGetOptions{remoteOptions: defaultRemoteOptions(), queueID: "default"}
	getCommand := &cobra.Command{
		Use:         "get ITEM_ID",
		Short:       "Get one queue item",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationQueueGet},
		Run: func(cmd *cobra.Command, args []string) {
			getOptions.captureRemoteFlags(cmd)
			if getOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationQueueGet, *jsonOutput, result.CodeInvalidRequest, "item id or --request is required", nil)
				return
			}
			if getOptions.requestPath != "" && (len(args) > 0 || cmd.Flags().Changed("queue-id")) {
				*exitCode = c.fail(result.OperationQueueGet, *jsonOutput, result.CodeInvalidRequest, "operation arguments and flags cannot be combined with --request", nil)
				return
			}
			itemID := 0
			var err error
			if len(args) == 1 {
				itemID, err = strconv.Atoi(args[0])
			}
			if len(args) == 1 && (err != nil || itemID < 1) {
				*exitCode = c.fail(result.OperationQueueGet, *jsonOutput, result.CodeInvalidRequest, "item id must be a positive integer", nil)
				return
			}
			*exitCode = c.executeQueueGet(cmd.Context(), *jsonOutput, getOptions, itemID)
		},
	}
	addRemoteFlags(getCommand, &getOptions.remoteOptions, requestTimeoutUsage)
	getCommand.Flags().StringVar(&getOptions.queueID, "queue-id", "default", "exact InvokeAI queue id")
	getCommand.Flags().StringVar(&getOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(getCommand)
	return command
}

func (c *CLI) executeQueueList(ctx context.Context, jsonOutput bool, options queueListOptions) int {
	execution := remoteExecution[queueops.ListRequest, queueops.ListResult]{
		operation:         result.OperationQueueList,
		connection:        options.remoteOptions,
		request:           queueops.ListRequest{SchemaVersion: 1, QueueID: options.queueID, Offset: options.offset, Limit: options.limit},
		requestPath:       options.requestPath,
		operationFlagsSet: options.fieldsSet,
		invoke:            queueops.List,
		render:            renderQueueSummaries,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderQueueSummaries(list queueops.ListResult, w io.Writer) error {
	for _, item := range list.Items {
		if _, err := fmt.Fprintf(w, "%d\t%s\t%s\n", item.ItemID, item.Status, item.BatchID); err != nil {
			return err
		}
	}
	return nil
}

func (c *CLI) executeQueueGet(ctx context.Context, jsonOutput bool, options queueGetOptions, itemID int) int {
	execution := remoteExecution[queueops.GetRequest, queueops.GetResult]{
		operation:   result.OperationQueueGet,
		connection:  options.remoteOptions,
		request:     queueops.GetRequest{SchemaVersion: 1, QueueID: options.queueID, ItemID: itemID},
		requestPath: options.requestPath,
		invoke:      queueops.Get,
		render:      renderQueueItem,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderQueueItem(get queueops.GetResult, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%d\t%s\t%s\n", get.Item.ItemID, get.Item.Status, get.Item.BatchID)
	return err
}

type imageListOptions struct {
	remoteOptions
	offset              int
	limit               int
	boardID             string
	includeIntermediate bool
	requestPath         string
	fieldsSet           bool
}

type imageGetOptions struct {
	remoteOptions
	requestPath string
}

type imageUploadOptions struct {
	remoteOptions
	requestPath string
}

func (c *CLI) newImagesCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "images",
		Short:       "Inspect and manage InvokeAI images",
		Annotations: map[string]string{operationAnnotation: result.OperationImages},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationImages, *jsonOutput, result.CodeInvalidRequest, "images requires a subcommand", nil)
		},
	}
	options := imageListOptions{remoteOptions: defaultRemoteOptions(), limit: 20}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List gallery images",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationImagesList},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit") || cmd.Flags().Changed("board") || cmd.Flags().Changed("include-intermediate")
			*exitCode = c.executeImagesList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions, requestTimeoutUsage)
	listCommand.Flags().IntVar(&options.offset, "offset", 0, "number of matching images to skip")
	listCommand.Flags().IntVar(&options.limit, "limit", 20, "maximum number of images to return")
	listCommand.Flags().StringVar(&options.boardID, "board", "", "include images from an exact board id, or none")
	listCommand.Flags().BoolVar(&options.includeIntermediate, "include-intermediate", false, "include intermediate images")
	listCommand.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(listCommand)

	getOptions := imageGetOptions{remoteOptions: defaultRemoteOptions()}
	getCommand := &cobra.Command{
		Use:         "get IMAGE_NAME",
		Short:       "Get an image by its exact InvokeAI image name",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationImagesGet},
		Run: func(cmd *cobra.Command, args []string) {
			getOptions.captureRemoteFlags(cmd)
			if getOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationImagesGet, *jsonOutput, result.CodeInvalidRequest, "image name or --request is required", nil)
				return
			}
			if getOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail(result.OperationImagesGet, *jsonOutput, result.CodeInvalidRequest, "image name cannot be combined with --request", nil)
				return
			}
			imageName := ""
			if len(args) == 1 {
				imageName = args[0]
			}
			*exitCode = c.executeImagesGet(cmd.Context(), *jsonOutput, getOptions, imageName)
		},
	}
	addRemoteFlags(getCommand, &getOptions.remoteOptions, requestTimeoutUsage)
	getCommand.Flags().StringVar(&getOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(getCommand)

	uploadOptions := imageUploadOptions{remoteOptions: defaultRemoteOptions()}
	uploadCommand := &cobra.Command{
		Use:         "upload PATH",
		Short:       "Upload one local image",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationImagesUpload},
		Run: func(cmd *cobra.Command, args []string) {
			uploadOptions.captureRemoteFlags(cmd)
			if uploadOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationImagesUpload, *jsonOutput, result.CodeInvalidRequest, "upload path or --request is required", nil)
				return
			}
			if uploadOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail(result.OperationImagesUpload, *jsonOutput, result.CodeInvalidRequest, "upload path cannot be combined with --request", nil)
				return
			}
			uploadPath := ""
			if len(args) == 1 {
				uploadPath = args[0]
			}
			*exitCode = c.executeImagesUpload(cmd.Context(), *jsonOutput, uploadOptions, uploadPath)
		},
	}
	addRemoteFlags(uploadCommand, &uploadOptions.remoteOptions, requestTimeoutUsage)
	uploadCommand.Flags().StringVar(&uploadOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(uploadCommand)
	return command
}

func (c *CLI) executeImagesList(ctx context.Context, jsonOutput bool, options imageListOptions) int {
	execution := remoteExecution[images.ListRequest, images.ListResult]{
		operation:  result.OperationImagesList,
		connection: options.remoteOptions,
		request: images.ListRequest{
			SchemaVersion:       1,
			Offset:              options.offset,
			Limit:               options.limit,
			BoardID:             options.boardID,
			IncludeIntermediate: options.includeIntermediate,
		},
		requestPath:       options.requestPath,
		operationFlagsSet: options.fieldsSet,
		invoke:            images.List,
		render:            renderImageReferences,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderImageReferences(list images.ListResult, w io.Writer) error {
	for _, image := range list.Items {
		if _, err := fmt.Fprintf(w, "%s\t%dx%d\t%s\n", image.ImageName, image.Width, image.Height, image.ImageCategory); err != nil {
			return err
		}
	}
	return nil
}

func (c *CLI) executeImagesGet(ctx context.Context, jsonOutput bool, options imageGetOptions, imageName string) int {
	execution := remoteExecution[images.GetRequest, images.GetResult]{
		operation:   result.OperationImagesGet,
		connection:  options.remoteOptions,
		request:     images.GetRequest{SchemaVersion: 1, ImageName: imageName},
		requestPath: options.requestPath,
		invoke:      images.Get,
		render:      renderImageReference,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderImageReference(get images.GetResult, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%dx%d\t%s\n", get.Image.ImageName, get.Image.Width, get.Image.Height, get.Image.ImageURL)
	return err
}

func (c *CLI) executeImagesUpload(ctx context.Context, jsonOutput bool, options imageUploadOptions, uploadPath string) int {
	execution := remoteExecution[images.UploadRequest, images.GetResult]{
		operation:   result.OperationImagesUpload,
		connection:  options.remoteOptions,
		request:     images.UploadRequest{SchemaVersion: 1, Path: uploadPath},
		requestPath: options.requestPath,
		invoke:      images.Upload,
		render:      renderUploadedImage,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderUploadedImage(upload images.GetResult, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\n", upload.Image.ImageName, upload.Image.ImageURL)
	return err
}

type modelListOptions struct {
	remoteOptions
	requestPath string
	baseModels  []string
	modelType   string
	modelFormat string
	modelName   string
	fieldsSet   bool
}

func (c *CLI) newModelsCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "models",
		Short:       "Inspect and manage InvokeAI models",
		Annotations: map[string]string{operationAnnotation: result.OperationModels},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationModels, *jsonOutput, result.CodeInvalidRequest, "models requires a subcommand", nil)
		},
	}

	options := modelListOptions{remoteOptions: defaultRemoteOptions()}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List installed models",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationModelsList},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("base") || cmd.Flags().Changed("type") || cmd.Flags().Changed("format") || cmd.Flags().Changed("name")
			*exitCode = c.executeModelsList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions, requestTimeoutUsage)
	listCommand.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	listCommand.Flags().StringArrayVar(&options.baseModels, "base", nil, "include an exact base model (repeatable)")
	listCommand.Flags().StringVar(&options.modelType, "type", "", "include one exact model type")
	listCommand.Flags().StringVar(&options.modelFormat, "format", "", "include one exact model format")
	listCommand.Flags().StringVar(&options.modelName, "name", "", "include one exact model name")
	command.AddCommand(listCommand, c.newModelsInstallCommand(exitCode, jsonOutput), c.newModelsStatusCommand(exitCode, jsonOutput))
	return command
}

func (c *CLI) executeModelsList(ctx context.Context, jsonOutput bool, options modelListOptions) int {
	execution := remoteExecution[models.ListRequest, models.ListResult]{
		operation:  result.OperationModelsList,
		connection: options.remoteOptions,
		request: models.ListRequest{
			SchemaVersion: 1,
			BaseModels:    options.baseModels,
			ModelType:     options.modelType,
			ModelFormat:   options.modelFormat,
			ModelName:     options.modelName,
		},
		requestPath:       options.requestPath,
		operationFlagsSet: options.fieldsSet,
		invoke:            models.List,
		render:            renderModelSummaries,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderModelSummaries(list models.ListResult, w io.Writer) error {
	for _, model := range list.Models {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", model.Key, model.Name, model.Base, model.Type); err != nil {
			return err
		}
	}
	return nil
}
