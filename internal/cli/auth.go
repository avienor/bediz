package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/huggingface"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

func (c *CLI) newAuthCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage InvokeAI authentication", Annotations: map[string]string{operationAnnotation: result.OperationAuth}, Run: func(_ *cobra.Command, _ []string) {
		*exitCode = c.fail(result.OperationAuth, *jsonOutput, result.CodeInvalidRequest, "auth requires huggingface", nil)
	}}
	huggingFace := &cobra.Command{Use: "huggingface", Short: "Manage InvokeAI Hugging Face login", Annotations: map[string]string{operationAnnotation: result.OperationAuthHuggingFace}, Run: func(_ *cobra.Command, _ []string) {
		*exitCode = c.fail(result.OperationAuthHuggingFace, *jsonOutput, result.CodeInvalidRequest, "auth huggingface requires status, login, or logout", nil)
	}}
	addAuthLeaf := func(name, operationName string, invoke func(context.Context, *httpclient.Client) (huggingface.Result, error)) *cobra.Command {
		options := defaultRemoteOptions()
		leaf := &cobra.Command{Use: name, Args: cobra.NoArgs, Annotations: map[string]string{operationAnnotation: operationName}, Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			execution := remoteExecution[struct{}, huggingface.Result]{operation: operationName, connection: options, invoke: func(ctx context.Context, client *httpclient.Client, _ struct{}) (huggingface.Result, error) {
				return invoke(ctx, client)
			}, render: func(value huggingface.Result, writer io.Writer) error {
				_, err := fmt.Fprintf(writer, "Hugging Face token: %s\n", value.Status)
				return err
			}}
			*exitCode = execution.run(cmd.Context(), c, *jsonOutput)
		}}
		addRemoteFlags(leaf, &options, requestTimeoutUsage)
		return leaf
	}
	huggingFace.AddCommand(addAuthLeaf("status", result.OperationAuthHFStatus, huggingface.Status))
	var tokenStdin bool
	login := addAuthLeaf("login", result.OperationAuthHFLogin, func(ctx context.Context, client *httpclient.Client) (huggingface.Result, error) {
		if !tokenStdin {
			return huggingface.Result{}, operation.InvalidRequest("login requires --token-stdin")
		}
		data, err := io.ReadAll(c.stdin)
		if err != nil {
			return huggingface.Result{}, operation.InvalidRequest("could not read Hugging Face token from standard input")
		}
		return huggingface.Login(ctx, client, strings.TrimSpace(string(data)))
	})
	login.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read a Hugging Face token from standard input")
	huggingFace.AddCommand(login)
	huggingFace.AddCommand(addAuthLeaf("logout", result.OperationAuthHFLogout, huggingface.Logout))
	command.AddCommand(huggingFace)
	return command
}
