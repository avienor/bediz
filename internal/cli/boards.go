package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/avienor/bediz/internal/boards"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

type boardListOptions struct {
	remoteOptions
	offset          int
	limit           int
	includeArchived bool
	requestPath     string
	fieldsSet       bool
}

type boardGetOptions struct {
	remoteOptions
	requestPath string
}

type boardCreateOptions struct {
	remoteOptions
	requestPath string
}

func (c *CLI) newBoardsCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "boards",
		Short:       "Inspect and create InvokeAI boards",
		Annotations: map[string]string{operationAnnotation: result.OperationBoards},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationBoards, *jsonOutput, result.CodeInvalidRequest, "boards requires a subcommand", nil)
		},
	}
	options := boardListOptions{remoteOptions: defaultRemoteOptions(), limit: 20}
	listCommand := &cobra.Command{
		Use:         "list",
		Short:       "List visible boards, newest first",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationBoardsList},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			options.fieldsSet = cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit") || cmd.Flags().Changed("include-archived")
			*exitCode = c.executeBoardsList(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(listCommand, &options.remoteOptions, requestTimeoutUsage)
	listCommand.Flags().IntVar(&options.offset, "offset", 0, "number of boards to skip")
	listCommand.Flags().IntVar(&options.limit, "limit", 20, "maximum number of boards to return")
	listCommand.Flags().BoolVar(&options.includeArchived, "include-archived", false, "include archived boards")
	listCommand.Flags().StringVar(&options.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(listCommand)

	getOptions := boardGetOptions{remoteOptions: defaultRemoteOptions()}
	getCommand := &cobra.Command{
		Use:         "get SELECTOR",
		Short:       "Get a board by exact identifier or unique name",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationBoardsGet},
		Run: func(cmd *cobra.Command, args []string) {
			getOptions.captureRemoteFlags(cmd)
			if getOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationBoardsGet, *jsonOutput, result.CodeInvalidRequest, "board selector or --request is required", nil)
				return
			}
			if getOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail(result.OperationBoardsGet, *jsonOutput, result.CodeInvalidRequest, "board selector cannot be combined with --request", nil)
				return
			}
			selector := ""
			if len(args) == 1 {
				selector = args[0]
			}
			*exitCode = c.executeBoardsGet(cmd.Context(), *jsonOutput, getOptions, selector)
		},
	}
	addRemoteFlags(getCommand, &getOptions.remoteOptions, requestTimeoutUsage)
	getCommand.Flags().StringVar(&getOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(getCommand)

	createOptions := boardCreateOptions{remoteOptions: defaultRemoteOptions()}
	createCommand := &cobra.Command{
		Use:         "create NAME",
		Short:       "Create a board with a name no visible board has",
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationBoardsCreate},
		Run: func(cmd *cobra.Command, args []string) {
			createOptions.captureRemoteFlags(cmd)
			if createOptions.requestPath == "" && len(args) == 0 {
				*exitCode = c.fail(result.OperationBoardsCreate, *jsonOutput, result.CodeInvalidRequest, "board name or --request is required", nil)
				return
			}
			if createOptions.requestPath != "" && len(args) > 0 {
				*exitCode = c.fail(result.OperationBoardsCreate, *jsonOutput, result.CodeInvalidRequest, "board name cannot be combined with --request", nil)
				return
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			*exitCode = c.executeBoardsCreate(cmd.Context(), *jsonOutput, createOptions, name)
		},
	}
	addRemoteFlags(createCommand, &createOptions.remoteOptions, requestTimeoutUsage)
	createCommand.Flags().StringVar(&createOptions.requestPath, "request", "", "read a request document from a file or standard input with -")
	command.AddCommand(createCommand)
	return command
}

func (c *CLI) executeBoardsList(ctx context.Context, jsonOutput bool, options boardListOptions) int {
	execution := remoteExecution[boards.ListRequest, boards.ListResult]{
		operation:  result.OperationBoardsList,
		connection: options.remoteOptions,
		request: boards.ListRequest{
			SchemaVersion:   1,
			Offset:          options.offset,
			Limit:           options.limit,
			IncludeArchived: options.includeArchived,
		},
		requestPath:       options.requestPath,
		operationFlagsSet: options.fieldsSet,
		invoke:            boards.List,
		render:            renderBoardSummaries,
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderBoardSummaries(list boards.ListResult, w io.Writer) error {
	for _, board := range list.Items {
		if err := renderBoardSummary(board, w); err != nil {
			return err
		}
	}
	return nil
}

func (c *CLI) executeBoardsGet(ctx context.Context, jsonOutput bool, options boardGetOptions, selector string) int {
	execution := remoteExecution[boards.GetRequest, boards.GetResult]{
		operation:   result.OperationBoardsGet,
		connection:  options.remoteOptions,
		request:     boards.GetRequest{SchemaVersion: 1, Board: selector},
		requestPath: options.requestPath,
		invoke:      boards.Get,
		render:      func(get boards.GetResult, w io.Writer) error { return renderBoardSummary(get.Board, w) },
	}
	return execution.run(ctx, c, jsonOutput)
}

func (c *CLI) executeBoardsCreate(ctx context.Context, jsonOutput bool, options boardCreateOptions, name string) int {
	execution := remoteExecution[boards.CreateRequest, boards.CreateResult]{
		operation:   result.OperationBoardsCreate,
		connection:  options.remoteOptions,
		request:     boards.CreateRequest{SchemaVersion: 1, BoardName: name},
		requestPath: options.requestPath,
		invoke:      boards.Create,
		render:      func(create boards.CreateResult, w io.Writer) error { return renderBoardSummary(create.Board, w) },
	}
	return execution.run(ctx, c, jsonOutput)
}

func renderBoardSummary(board boards.Summary, w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%d\n", board.BoardID, board.BoardName, board.ImageCount)
	return err
}
