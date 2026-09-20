package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/avienor/bediz/internal/config"
	"github.com/avienor/bediz/internal/doctor"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/version"
	"github.com/spf13/cobra"
)

type CLI struct {
	stdout io.Writer
	stderr io.Writer
}

const operationAnnotation = "bediz.operation"

func New(stdout, stderr io.Writer) *CLI {
	return &CLI{
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

	root.AddCommand(c.newDoctorCommand(exitCode, &jsonOutput))
	root.AddCommand(c.newConfigCommand(exitCode, &jsonOutput))
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
	fmt.Fprintf(c.stdout, "Configuration: %s\n", view.Path)
	if view.Stored.URL == "" {
		fmt.Fprintln(c.stdout, "Stored URL: not set")
	} else {
		fmt.Fprintf(c.stdout, "Stored URL: %s\n", view.Stored.URL)
	}
	fmt.Fprintf(c.stdout, "Stored token: %s\n", configured(view.Stored.TokenConfigured))
	fmt.Fprintf(c.stdout, "Effective URL: %s (%s)\n", view.Effective.URL, view.Effective.URLSource)
	fmt.Fprintf(c.stdout, "Effective token: %s", configured(view.Effective.TokenConfigured))
	if view.Effective.TokenSource != "" {
		fmt.Fprintf(c.stdout, " (%s)", view.Effective.TokenSource)
	}
	fmt.Fprintln(c.stdout)
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
