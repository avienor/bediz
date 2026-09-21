package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/avienor/bediz/internal/config"
	"github.com/avienor/bediz/internal/doctor"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/operation"
	queueops "github.com/avienor/bediz/internal/queue"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
	"github.com/spf13/cobra"
)

type CLI struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

const operationAnnotation = "bediz.operation"

func New(stdout, stderr io.Writer) *CLI {
	return NewWithIO(os.Stdin, stdout, stderr)
}

func NewWithIO(stdin io.Reader, stdout, stderr io.Writer) *CLI {
	return &CLI{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

func (c *CLI) Run(ctx context.Context, args []string) int {
	exitCode := result.ExitSuccess
	root := c.newRootCommand(&exitCode)
	root.SetArgs(args)
	executed, err := root.ExecuteContextC(ctx)
	if err != nil {
		return c.fail(commandOperation(executed), containsJSONFlag(args), result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
	}
	return exitCode
}

func (c *CLI) newRootCommand(exitCode *int) *cobra.Command {
	var jsonOutput bool
	root := &cobra.Command{
		Use:           "bediz",
		Short:         "Deterministic controller for InvokeAI",
		SilenceErrors: true,
		SilenceUsage:  true,
		Run: func(command *cobra.Command, _ []string) {
			if jsonOutput {
				*exitCode = c.fail("cli", true, result.ExitInvalidRequest, "invalid_request", "a command is required", nil)
				return
			}
			command.SetOut(c.stderr)
			_ = command.Help()
			command.SetOut(c.stdout)
			*exitCode = result.ExitInvalidRequest
		},
	}
	root.SetOut(c.stdout)
	root.SetErr(c.stderr)
	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "write one JSON result envelope")
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(command *cobra.Command, args []string) {
		if jsonOutput {
			*exitCode = c.fail(commandOperation(command), true, result.ExitInvalidRequest, "invalid_request", "help is not available in JSON mode", nil)
			return
		}
		defaultHelp(command, args)
	})

	root.AddCommand(c.newDoctorCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newConfigCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newModelsCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newImagesCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newQueueCommand(exitCode, &jsonOutput))
	root.AddCommand(&cobra.Command{
		Use:         "version",
		Short:       "Print the Bediz version",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "version"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.executeVersion(jsonOutput)
		},
	})
	return root
}

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

func addRemoteFlags(command *cobra.Command, options *remoteOptions) {
	command.Flags().StringVar(&options.url, "url", "", "InvokeAI base URL")
	command.Flags().StringVar(&options.token, "token", "", "InvokeAI bearer token")
	command.Flags().DurationVar(&options.timeout, "timeout", httpclient.DefaultTimeout, "request timeout")
}

func (o *remoteOptions) captureRemoteFlags(command *cobra.Command) {
	o.urlSet = command.Flags().Changed("url")
	o.tokenSet = command.Flags().Changed("token")
}

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
		Annotations: map[string]string{operationAnnotation: "queue"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail("queue", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "queue requires a subcommand", nil)
		},
	}
	options := queueListOptions{remoteOptions: defaultRemoteOptions(), queueID: "default", limit: 20}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List queue item summaries",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "queue.list"},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("queue-id") || cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit")
			*exitCode = c.executeQueueList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions)
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
		Annotations: map[string]string{operationAnnotation: "queue.get"},
		Run: func(cmd *cobra.Command, args []string) {
			getOptions.captureRemoteFlags(cmd)
			if getOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail("queue.get", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "item id or --request is required", nil)
				return
			}
			if getOptions.requestPath != "" && (len(args) > 0 || cmd.Flags().Changed("queue-id")) {
				*exitCode = c.fail("queue.get", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "operation arguments and flags cannot be combined with --request", nil)
				return
			}
			itemID := 0
			var err error
			if len(args) == 1 {
				itemID, err = strconv.Atoi(args[0])
			}
			if len(args) == 1 && (err != nil || itemID < 1) {
				*exitCode = c.fail("queue.get", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "item id must be a positive integer", nil)
				return
			}
			*exitCode = c.executeQueueGet(cmd.Context(), *jsonOutput, getOptions, itemID)
		},
	}
	addRemoteFlags(getCommand, &getOptions.remoteOptions)
	getCommand.Flags().StringVar(&getOptions.queueID, "queue-id", "default", "exact InvokeAI queue id")
	getCommand.Flags().StringVar(&getOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(getCommand)
	return command
}

func (c *CLI) executeQueueList(ctx context.Context, jsonOutput bool, options queueListOptions) int {
	if options.timeout <= 0 {
		return c.fail("queue.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	if options.requestPath != "" && options.fieldsSet {
		return c.fail("queue.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "operation flags cannot be combined with --request", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("queue.list", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := queueops.ListRequest{
		SchemaVersion: 1,
		QueueID:       options.queueID,
		Offset:        options.offset,
		Limit:         options.limit,
	}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("queue.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	list, err := queueops.List(ctx, client, request)
	if err != nil {
		return c.failRemote("queue.list", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("queue.list", list, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	for _, item := range list.Items {
		if _, err := fmt.Fprintf(c.stdout, "%d\t%s\t%s\n", item.ItemID, item.Status, item.BatchID); err != nil {
			return c.fail("queue.list", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
		}
	}
	return result.ExitSuccess
}

func (c *CLI) executeQueueGet(ctx context.Context, jsonOutput bool, options queueGetOptions, itemID int) int {
	if options.timeout <= 0 {
		return c.fail("queue.get", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("queue.get", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := queueops.GetRequest{SchemaVersion: 1, QueueID: options.queueID, ItemID: itemID}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("queue.get", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	get, err := queueops.Get(ctx, client, request)
	if err != nil {
		return c.failRemote("queue.get", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("queue.get", get, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	if _, err := fmt.Fprintf(c.stdout, "%d\t%s\t%s\n", get.Item.ItemID, get.Item.Status, get.Item.BatchID); err != nil {
		return c.fail("queue.get", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
	}
	return result.ExitSuccess
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
		Annotations: map[string]string{operationAnnotation: "images"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail("images", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "images requires a subcommand", nil)
		},
	}
	options := imageListOptions{remoteOptions: defaultRemoteOptions(), limit: 20}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List gallery images",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "images.list"},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit") || cmd.Flags().Changed("board") || cmd.Flags().Changed("include-intermediate")
			*exitCode = c.executeImagesList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions)
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
		Annotations: map[string]string{operationAnnotation: "images.get"},
		Run: func(cmd *cobra.Command, args []string) {
			getOptions.captureRemoteFlags(cmd)
			if getOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail("images.get", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "image name or --request is required", nil)
				return
			}
			if getOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail("images.get", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "image name cannot be combined with --request", nil)
				return
			}
			imageName := ""
			if len(args) == 1 {
				imageName = args[0]
			}
			*exitCode = c.executeImagesGet(cmd.Context(), *jsonOutput, getOptions, imageName)
		},
	}
	addRemoteFlags(getCommand, &getOptions.remoteOptions)
	getCommand.Flags().StringVar(&getOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(getCommand)

	uploadOptions := imageUploadOptions{remoteOptions: defaultRemoteOptions()}
	uploadCommand := &cobra.Command{
		Use:         "upload PATH",
		Short:       "Upload one local image",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: "images.upload"},
		Run: func(cmd *cobra.Command, args []string) {
			uploadOptions.captureRemoteFlags(cmd)
			if uploadOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail("images.upload", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "upload path or --request is required", nil)
				return
			}
			if uploadOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail("images.upload", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "upload path cannot be combined with --request", nil)
				return
			}
			uploadPath := ""
			if len(args) == 1 {
				uploadPath = args[0]
			}
			*exitCode = c.executeImagesUpload(cmd.Context(), *jsonOutput, uploadOptions, uploadPath)
		},
	}
	addRemoteFlags(uploadCommand, &uploadOptions.remoteOptions)
	uploadCommand.Flags().StringVar(&uploadOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(uploadCommand)
	return command
}

func (c *CLI) executeImagesList(ctx context.Context, jsonOutput bool, options imageListOptions) int {
	if options.timeout <= 0 {
		return c.fail("images.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	if options.requestPath != "" && options.fieldsSet {
		return c.fail("images.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "operation flags cannot be combined with --request", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("images.list", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := images.ListRequest{
		SchemaVersion:       1,
		Offset:              options.offset,
		Limit:               options.limit,
		BoardID:             options.boardID,
		IncludeIntermediate: options.includeIntermediate,
	}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("images.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	list, err := images.List(ctx, client, request)
	if err != nil {
		return c.failRemote("images.list", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("images.list", list, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	for _, image := range list.Items {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%dx%d\t%s\n", image.ImageName, image.Width, image.Height, image.ImageCategory); err != nil {
			return c.fail("images.list", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
		}
	}
	return result.ExitSuccess
}

func (c *CLI) executeImagesGet(ctx context.Context, jsonOutput bool, options imageGetOptions, imageName string) int {
	if options.timeout <= 0 {
		return c.fail("images.get", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("images.get", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := images.GetRequest{SchemaVersion: 1, ImageName: imageName}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("images.get", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	get, err := images.Get(ctx, client, request)
	if err != nil {
		return c.failRemote("images.get", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("images.get", get, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	if _, err := fmt.Fprintf(c.stdout, "%s\t%dx%d\t%s\n", get.Image.ImageName, get.Image.Width, get.Image.Height, get.Image.ImageURL); err != nil {
		return c.fail("images.get", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
	}
	return result.ExitSuccess
}

func (c *CLI) executeImagesUpload(ctx context.Context, jsonOutput bool, options imageUploadOptions, uploadPath string) int {
	if options.timeout <= 0 {
		return c.fail("images.upload", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("images.upload", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := images.UploadRequest{SchemaVersion: 1, Path: uploadPath}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("images.upload", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	upload, err := images.Upload(ctx, client, request)
	if err != nil {
		return c.failRemote("images.upload", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("images.upload", upload, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	if _, err := fmt.Fprintf(c.stdout, "%s\t%s\n", upload.Image.ImageName, upload.Image.ImageURL); err != nil {
		return c.fail("images.upload", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
	}
	return result.ExitSuccess
}

func (c *CLI) failRemote(operationName string, jsonOutput bool, err error) int {
	var invalid *operation.InvalidRequestError
	if errors.As(err, &invalid) {
		return c.fail(operationName, jsonOutput, result.ExitInvalidRequest, "invalid_request", invalid.Error(), nil)
	}
	var unsupported *operation.UnsupportedCapabilityError
	if errors.As(err, &unsupported) {
		return c.fail(operationName, jsonOutput, result.ExitUnsupportedCapability, "unsupported_capability", unsupported.Error(), nil)
	}
	var unknown *httpclient.OutcomeUnknownError
	if errors.As(err, &unknown) {
		return c.fail(operationName, jsonOutput, result.ExitInvokeAIFailure, "outcome_unknown", "InvokeAI may have accepted the operation; inspect remote state before retrying", nil)
	}
	if errors.Is(err, context.Canceled) {
		return c.fail(operationName, jsonOutput, result.ExitInterrupted, "interrupted", "operation was interrupted locally", nil)
	}
	var network *httpclient.NetworkError
	if errors.As(err, &network) {
		return c.fail(operationName, jsonOutput, result.ExitConnection, "connection_failed", "could not reach InvokeAI", nil)
	}
	var invalidResponse *httpclient.InvalidResponseError
	if errors.As(err, &invalidResponse) {
		return c.fail(operationName, jsonOutput, result.ExitInvokeAIFailure, "invalid_invokeai_response", "InvokeAI returned an invalid response", nil)
	}
	var httpErr *httpclient.HTTPError
	if errors.As(err, &httpErr) && httpErr.AuthenticationFailure() {
		return c.fail(operationName, jsonOutput, result.ExitConnection, "authentication_failed", "InvokeAI rejected authentication", map[string]any{"status": httpErr.StatusCode})
	}
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		return c.fail(operationName, jsonOutput, result.ExitInvokeAIFailure, "not_found", "the requested InvokeAI resource was not found", nil)
	}
	if errors.As(err, &httpErr) {
		return c.fail(operationName, jsonOutput, result.ExitInvokeAIFailure, "invokeai_operation_failed", "InvokeAI rejected the operation", map[string]any{"status": httpErr.StatusCode})
	}
	return c.fail(operationName, jsonOutput, result.ExitInvokeAIFailure, "invokeai_operation_failed", err.Error(), nil)
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
		Annotations: map[string]string{operationAnnotation: "models"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail("models", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "models requires a subcommand", nil)
		},
	}

	options := modelListOptions{remoteOptions: defaultRemoteOptions()}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List installed models",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "models.list"},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("base") || cmd.Flags().Changed("type") || cmd.Flags().Changed("format") || cmd.Flags().Changed("name")
			*exitCode = c.executeModelsList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions)
	listCommand.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	listCommand.Flags().StringArrayVar(&options.baseModels, "base", nil, "include an exact base model (repeatable)")
	listCommand.Flags().StringVar(&options.modelType, "type", "", "include one exact model type")
	listCommand.Flags().StringVar(&options.modelFormat, "format", "", "include one exact model format")
	listCommand.Flags().StringVar(&options.modelName, "name", "", "include one exact model name")
	command.AddCommand(listCommand)
	return command
}

func (c *CLI) executeModelsList(ctx context.Context, jsonOutput bool, options modelListOptions) int {
	if options.timeout <= 0 {
		return c.fail("models.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}
	if options.requestPath != "" && options.fieldsSet {
		return c.fail("models.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", "operation flags cannot be combined with --request", nil)
	}
	client, details, err := newRemoteClient(options.remoteOptions)
	if err != nil {
		return c.fail("models.list", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), details)
	}
	request := models.ListRequest{
		SchemaVersion: 1,
		BaseModels:    options.baseModels,
		ModelType:     options.modelType,
		ModelFormat:   options.modelFormat,
		ModelName:     options.modelName,
	}
	if options.requestPath != "" {
		if err := c.loadRequestDocument(options.requestPath, &request); err != nil {
			return c.fail("models.list", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
	}
	list, err := models.List(ctx, client, request)
	if err != nil {
		return c.failRemote("models.list", jsonOutput, err)
	}
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("models.list", list, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	for _, model := range list.Models {
		if _, err := fmt.Fprintf(c.stdout, "%s\t%s\t%s\t%s\n", model.Key, model.Name, model.Base, model.Type); err != nil {
			return c.fail("models.list", false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
		}
	}
	return result.ExitSuccess
}

func (c *CLI) loadRequestDocument(path string, target any) error {
	var reader io.Reader
	if path == "-" {
		reader = c.stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open request document: %w", err)
		}
		defer file.Close()
		reader = file
	}

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode request document: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("request document must contain exactly one JSON value")
		}
		return fmt.Errorf("decode request document: %w", err)
	}
	return nil
}

type doctorOptions struct {
	url      string
	urlSet   bool
	token    string
	tokenSet bool
	timeout  time.Duration
}

func (c *CLI) newDoctorCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := doctorOptions{timeout: httpclient.DefaultTimeout}
	command := &cobra.Command{
		Use:         "doctor",
		Short:       "Check InvokeAI readiness",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "doctor"},
		Run: func(cmd *cobra.Command, _ []string) {
			options.urlSet = cmd.Flags().Changed("url")
			options.tokenSet = cmd.Flags().Changed("token")
			*exitCode = c.executeDoctor(cmd.Context(), *jsonOutput, options)
		},
	}
	command.Flags().StringVar(&options.url, "url", "", "InvokeAI base URL")
	command.Flags().StringVar(&options.token, "token", "", "InvokeAI bearer token")
	command.Flags().DurationVar(&options.timeout, "timeout", httpclient.DefaultTimeout, "total diagnostic request timeout")
	return command
}

func (c *CLI) newConfigCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "config",
		Short:       "Manage Bediz configuration",
		Annotations: map[string]string{operationAnnotation: "config"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail("config", *jsonOutput, result.ExitInvalidRequest, "invalid_request", "config requires get or set", nil)
		},
	}
	command.AddCommand(&cobra.Command{
		Use:         "get",
		Short:       "Show stored and effective configuration",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "config.get"},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.executeConfigGet(*jsonOutput)
		},
	})
	var options configSetOptions
	setCommand := &cobra.Command{
		Use:         "set",
		Short:       "Update stored configuration",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: "config.set"},
		Run: func(cmd *cobra.Command, _ []string) {
			options.urlSet = cmd.Flags().Changed("url")
			options.tokenSet = cmd.Flags().Changed("token")
			*exitCode = c.executeConfigSet(*jsonOutput, options)
		},
	}
	setCommand.Flags().StringVar(&options.url, "url", "", "persist an InvokeAI base URL")
	setCommand.Flags().StringVar(&options.token, "token", "", "persist an InvokeAI bearer token")
	setCommand.Flags().BoolVar(&options.unsetURL, "unset-url", false, "remove the persisted URL")
	setCommand.Flags().BoolVar(&options.unsetToken, "unset-token", false, "remove the persisted token")
	command.AddCommand(setCommand)
	return command
}

func (c *CLI) executeDoctor(ctx context.Context, jsonOutput bool, options doctorOptions) int {
	if options.timeout <= 0 {
		return c.fail("doctor", jsonOutput, result.ExitInvalidRequest, "invalid_request", "timeout must be positive", nil)
	}

	path, err := config.Path()
	if err != nil {
		return c.fail("doctor", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	fileConfig, err := config.Load(path)
	if err != nil {
		return c.fail("doctor", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), map[string]any{"path": path})
	}
	resolved, err := config.Resolve(
		config.Overrides{URL: options.url, URLSet: options.urlSet, Token: options.token, TokenSet: options.tokenSet},
		config.EnvironmentFrom(os.LookupEnv),
		fileConfig,
	)
	if err != nil {
		return c.fail("doctor", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	client, err := httpclient.New(resolved.URL, resolved.Token, httpclient.Options{
		Timeout:   options.timeout,
		UserAgent: "bediz/" + version.Current().Version,
	})
	if err != nil {
		return c.fail("doctor", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	diagnosticContext, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	report := doctor.Run(diagnosticContext, client, version.Current())
	if ctx.Err() != nil {
		return c.fail("doctor", jsonOutput, result.ExitInterrupted, "interrupted", "doctor was interrupted locally", map[string]any{"report": report})
	}

	exitCode, code, message := doctor.Failure(report)
	if jsonOutput {
		if exitCode != result.ExitSuccess {
			return c.fail("doctor", true, exitCode, code, message, map[string]any{"report": report})
		}
		if err := result.WriteJSON(c.stdout, result.Success("doctor", report, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	report.Human(c.stdout)
	return exitCode
}

func (c *CLI) executeConfigGet(jsonOutput bool) int {
	view, err := c.configurationView()
	if err != nil {
		return c.fail("config.get", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	return c.writeConfigView("config.get", jsonOutput, view)
}

type configSetOptions struct {
	url        string
	urlSet     bool
	token      string
	tokenSet   bool
	unsetURL   bool
	unsetToken bool
}

func (c *CLI) executeConfigSet(jsonOutput bool, options configSetOptions) int {
	if !options.urlSet && !options.tokenSet && !options.unsetURL && !options.unsetToken {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_request", "config set requires a value to set or unset", nil)
	}
	if options.urlSet && options.unsetURL {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_request", "--url and --unset-url cannot be combined", nil)
	}
	if options.tokenSet && options.unsetToken {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_request", "--token and --unset-token cannot be combined", nil)
	}

	path, err := config.Path()
	if err != nil {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	fileConfig, err := config.Load(path)
	if err != nil {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), map[string]any{"path": path})
	}
	if options.urlSet {
		normalized, err := config.NormalizeURL(options.url)
		if err != nil {
			return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_request", err.Error(), nil)
		}
		fileConfig.URL = normalized
	}
	if options.tokenSet {
		if options.token == "" {
			return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_request", "token cannot be empty; use --unset-token", nil)
		}
		fileConfig.Token = options.token
	}
	if options.unsetURL {
		fileConfig.URL = ""
	}
	if options.unsetToken {
		fileConfig.Token = ""
	}
	if err := config.Save(path, fileConfig); err != nil {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "configuration_write_failed", err.Error(), map[string]any{"path": path})
	}
	view, err := c.configurationView()
	if err != nil {
		return c.fail("config.set", jsonOutput, result.ExitInvalidRequest, "invalid_configuration", err.Error(), nil)
	}
	return c.writeConfigView("config.set", jsonOutput, view)
}

func (c *CLI) executeVersion(jsonOutput bool) int {
	info := version.Current()
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success("version", info, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
	} else {
		fmt.Fprintln(c.stdout, info.Version)
	}
	return result.ExitSuccess
}

type storedView struct {
	URL             string `json:"url,omitempty"`
	TokenConfigured bool   `json:"token_configured"`
}

type effectiveView struct {
	URL             string `json:"url"`
	URLSource       string `json:"url_source"`
	TokenConfigured bool   `json:"token_configured"`
	TokenSource     string `json:"token_source,omitempty"`
}

type configView struct {
	Path      string        `json:"path"`
	Stored    storedView    `json:"stored"`
	Effective effectiveView `json:"effective"`
}

func (c *CLI) configurationView() (configView, error) {
	path, err := config.Path()
	if err != nil {
		return configView{}, err
	}
	fileConfig, err := config.Load(path)
	if err != nil {
		return configView{}, err
	}
	resolved, err := config.Resolve(config.Overrides{}, config.EnvironmentFrom(os.LookupEnv), fileConfig)
	if err != nil {
		return configView{}, err
	}
	return configView{
		Path: path,
		Stored: storedView{
			URL:             fileConfig.URL,
			TokenConfigured: fileConfig.Token != "",
		},
		Effective: effectiveView{
			URL:             resolved.URL,
			URLSource:       resolved.URLSource,
			TokenConfigured: resolved.Token != "",
			TokenSource:     resolved.TokenSource,
		},
	}, nil
}

func (c *CLI) writeConfigView(operation string, jsonOutput bool, view configView) int {
	if jsonOutput {
		if err := result.WriteJSON(c.stdout, result.Success(operation, view, nil)); err != nil {
			fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitSuccess
	}
	storedURL := "not set"
	if view.Stored.URL != "" {
		storedURL = view.Stored.URL
	}
	effectiveToken := configured(view.Effective.TokenConfigured)
	if view.Effective.TokenSource != "" {
		effectiveToken += fmt.Sprintf(" (%s)", view.Effective.TokenSource)
	}
	output := strings.Join([]string{
		fmt.Sprintf("Configuration: %s", view.Path),
		fmt.Sprintf("Stored URL: %s", storedURL),
		fmt.Sprintf("Stored token: %s", configured(view.Stored.TokenConfigured)),
		fmt.Sprintf("Effective URL: %s (%s)", view.Effective.URL, view.Effective.URLSource),
		fmt.Sprintf("Effective token: %s", effectiveToken),
	}, "\n")
	if _, err := fmt.Fprintln(c.stdout, output); err != nil {
		return c.fail(operation, false, result.ExitInvokeAIFailure, "output_write_failed", err.Error(), nil)
	}
	return result.ExitSuccess
}

func (c *CLI) fail(operation string, jsonOutput bool, exitCode int, code, message string, details map[string]any) int {
	if jsonOutput {
		err := result.WriteJSON(c.stdout, result.Failure(operation, result.Error{Code: code, Message: message, Details: details}, nil))
		if err != nil {
			fmt.Fprintf(c.stderr, "write JSON error: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return exitCode
	}
	fmt.Fprintf(c.stderr, "%s: %s\n", code, message)
	return exitCode
}

func containsJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}

func commandOperation(command *cobra.Command) string {
	if command != nil && command.Annotations != nil {
		if operation := command.Annotations[operationAnnotation]; operation != "" {
			return operation
		}
	}
	return "cli"
}

func configured(value bool) string {
	if value {
		return "configured"
	}
	return "not configured"
}
