package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/config"
	"github.com/avienor/bediz/internal/doctor"
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
		return c.fail(commandOperation(executed), containsJSONFlag(args), result.CodeInvalidRequest, err.Error(), nil)
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
				*exitCode = c.fail(result.OperationCLI, true, result.CodeInvalidRequest, "a command is required", nil)
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
			*exitCode = c.fail(commandOperation(command), true, result.CodeInvalidRequest, "help is not available in JSON mode", nil)
			return
		}
		defaultHelp(command, args)
	})

	root.AddCommand(c.newDoctorCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newGenerateCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newConfigCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newModelsCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newImagesCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newQueueCommand(exitCode, &jsonOutput))
	root.AddCommand(&cobra.Command{
		Use:         "version",
		Short:       "Print the Bediz version",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationVersion},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.executeVersion(jsonOutput)
		},
	})
	return root
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
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode request document: %w", err)
	}
	return errors.New("request document must contain exactly one JSON value")
}

func (c *CLI) newDoctorCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	options := defaultRemoteOptions()
	command := &cobra.Command{
		Use:         "doctor",
		Short:       "Check InvokeAI readiness",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationDoctor},
		Run: func(cmd *cobra.Command, _ []string) {
			options.captureRemoteFlags(cmd)
			*exitCode = c.executeDoctor(cmd.Context(), *jsonOutput, options)
		},
	}
	addRemoteFlags(command, &options, "total diagnostic request timeout")
	return command
}

// executeDoctor resolves the connection the same way every remote command does,
// then bounds the whole diagnostic run, not one request, with the timeout.
func (c *CLI) executeDoctor(ctx context.Context, jsonOutput bool, options remoteOptions) int {
	if options.timeout <= 0 {
		return c.fail(result.OperationDoctor, jsonOutput, result.CodeInvalidRequest, "timeout must be positive", nil)
	}
	client, details, err := newRemoteClient(options)
	if err != nil {
		return c.fail(result.OperationDoctor, jsonOutput, result.CodeInvalidConfiguration, err.Error(), details)
	}
	diagnosticContext, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	report := doctor.Run(diagnosticContext, client, version.Current())
	if ctx.Err() != nil {
		return c.fail(result.OperationDoctor, jsonOutput, result.CodeInterrupted, "doctor was interrupted locally", map[string]any{"report": report})
	}
	failure := doctor.Failure(report)
	if jsonOutput {
		if failure != nil {
			return c.fail(result.OperationDoctor, true, failure.Code, failure.Message, map[string]any{"report": report})
		}
		return c.writeResult(result.OperationDoctor, report)
	}
	if err := report.Human(c.stdout); err != nil {
		return c.fail(result.OperationDoctor, false, result.CodeOutputWriteFailed, err.Error(), nil)
	}
	if failure != nil {
		return result.ExitStatus(failure.Code)
	}
	return result.ExitSuccess
}

func (c *CLI) newConfigCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use:         "config",
		Short:       "Manage Bediz configuration",
		Annotations: map[string]string{operationAnnotation: result.OperationConfig},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationConfig, *jsonOutput, result.CodeInvalidRequest, "config requires get or set", nil)
		},
	}
	command.AddCommand(&cobra.Command{
		Use:         "get",
		Short:       "Show stored and effective configuration",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationConfigGet},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.executeConfigGet(*jsonOutput)
		},
	})
	var options configSetOptions
	setCommand := &cobra.Command{
		Use:         "set",
		Short:       "Update stored configuration",
		Args:        cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationConfigSet},
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

func (c *CLI) executeConfigGet(jsonOutput bool) int {
	view, err := c.configurationView()
	if err != nil {
		return c.fail(result.OperationConfigGet, jsonOutput, result.CodeInvalidConfiguration, err.Error(), nil)
	}
	return c.writeConfigView(result.OperationConfigGet, jsonOutput, view)
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
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidRequest, "config set requires a value to set or unset", nil)
	}
	if options.urlSet && options.unsetURL {
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidRequest, "--url and --unset-url cannot be combined", nil)
	}
	if options.tokenSet && options.unsetToken {
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidRequest, "--token and --unset-token cannot be combined", nil)
	}

	path, err := config.Path()
	if err != nil {
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidConfiguration, err.Error(), nil)
	}
	fileConfig, err := config.Load(path)
	if err != nil {
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidConfiguration, err.Error(), map[string]any{"path": path})
	}
	if options.urlSet {
		normalized, err := config.NormalizeURL(options.url)
		if err != nil {
			return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidRequest, err.Error(), nil)
		}
		fileConfig.URL = normalized
	}
	if options.tokenSet {
		if options.token == "" {
			return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidRequest, "token cannot be empty; use --unset-token", nil)
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
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeConfigurationWriteFailed, err.Error(), map[string]any{"path": path})
	}
	view, err := c.configurationView()
	if err != nil {
		return c.fail(result.OperationConfigSet, jsonOutput, result.CodeInvalidConfiguration, err.Error(), nil)
	}
	return c.writeConfigView(result.OperationConfigSet, jsonOutput, view)
}

func (c *CLI) executeVersion(jsonOutput bool) int {
	info := version.Current()
	if jsonOutput {
		return c.writeResult(result.OperationVersion, info)
	}
	if _, err := fmt.Fprintln(c.stdout, info.Version); err != nil {
		return c.fail(result.OperationVersion, false, result.CodeOutputWriteFailed, err.Error(), nil)
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
		return c.writeResult(operation, view)
	}
	storedURL := cmp.Or(view.Stored.URL, "not set")
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
		return c.fail(operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
	}
	return result.ExitSuccess
}

// writeResult writes the single final success envelope. A failure to write that
// envelope has no structured representation, so it is reported on standard
// error and reports the InvokeAI failure category.
func (c *CLI) writeResult(operation string, data any) int {
	if err := result.WriteJSON(c.stdout, result.Success(operation, data, nil)); err != nil {
		fmt.Fprintf(c.stderr, "write JSON result: %v\n", err)
		return result.ExitInvokeAIFailure
	}
	return result.ExitSuccess
}

// fail reports one structured failure: the failure envelope in machine-output
// mode, a diagnostic line on standard error otherwise. The exit status follows
// from the structured error code.
func (c *CLI) fail(operation string, jsonOutput bool, code, message string, details map[string]any) int {
	if jsonOutput {
		err := result.WriteJSON(c.stdout, result.Failure(operation, result.Error{Code: code, Message: message, Details: details}, nil))
		if err != nil {
			fmt.Fprintf(c.stderr, "write JSON error: %v\n", err)
			return result.ExitInvokeAIFailure
		}
		return result.ExitStatus(code)
	}
	fmt.Fprintf(c.stderr, "%s: %s\n", code, message)
	return result.ExitStatus(code)
}

func containsJSONFlag(args []string) bool {
	enabled := false
	for _, arg := range args {
		if arg == "--json" {
			enabled = true
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false
			}
			enabled = parsed
		}
	}
	return enabled
}

func commandOperation(command *cobra.Command) string {
	if command != nil && command.Annotations != nil {
		if operation := command.Annotations[operationAnnotation]; operation != "" {
			return operation
		}
	}
	return result.OperationCLI
}

func configured(value bool) string {
	if value {
		return "configured"
	}
	return "not configured"
}
