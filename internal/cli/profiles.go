package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"

	"github.com/avienor/bediz/internal/profiles"
	"github.com/avienor/bediz/internal/result"
	"github.com/spf13/cobra"
)

func (c *CLI) newProfilesCommand(exitCode *int, jsonOutput *bool) *cobra.Command {
	command := &cobra.Command{
		Use: "profiles", Short: "Manage local Generation Profiles",
		Annotations: map[string]string{operationAnnotation: result.OperationProfiles},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = c.fail(result.OperationProfiles, *jsonOutput, result.CodeInvalidRequest, "profiles requires a subcommand", nil)
		},
	}
	var requestPath string
	var replace, yes bool
	create := &cobra.Command{
		Use: "create", Short: "Save a Generation Profile from a JSON document", Args: cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationProfilesCreate},
		Run: func(_ *cobra.Command, _ []string) {
			if requestPath == "" {
				*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeInvalidRequest, "--request is required", nil)
				return
			}
			if replace && !yes {
				*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeInvalidRequest, "--replace requires --yes", nil)
				return
			}
			var doc profiles.Document
			if err := c.loadRequestDocument(requestPath, &doc); err != nil {
				*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeInvalidRequest, err.Error(), nil)
				return
			}
			if err := profiles.Validate(doc); err != nil {
				*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeInvalidRequest, err.Error(), nil)
				return
			}
			if err := profiles.Create(doc, replace); err != nil {
				if existing, ok := errors.AsType[*profiles.ExistsError](err); ok {
					*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeInvalidRequest, err.Error(), map[string]any{"reason": "profile_exists", "name": existing.Name})
				} else {
					*exitCode = c.fail(result.OperationProfilesCreate, *jsonOutput, result.CodeConfigurationWriteFailed, err.Error(), nil)
				}
				return
			}
			*exitCode = c.profileSuccess(result.OperationProfilesCreate, *jsonOutput, doc)
		},
	}
	create.Flags().StringVar(&requestPath, "request", "", "read a Profile Document from a file or standard input with -")
	create.Flags().BoolVar(&replace, "replace", false, "replace an existing profile")
	create.Flags().BoolVar(&yes, "yes", false, "approve replacing an existing profile")
	command.AddCommand(create)
	get := &cobra.Command{
		Use: "get NAME", Short: "Get a Generation Profile", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationProfilesGet},
		Run: func(_ *cobra.Command, args []string) {
			if !profiles.ValidName(args[0]) {
				*exitCode = c.fail(result.OperationProfilesGet, *jsonOutput, result.CodeInvalidRequest, "invalid profile name", nil)
				return
			}
			doc, err := profiles.Get(args[0])
			if errors.Is(err, os.ErrNotExist) {
				*exitCode = c.fail(result.OperationProfilesGet, *jsonOutput, result.CodeNotFound, fmt.Sprintf("profile %q was not found", args[0]), nil)
			} else if err != nil {
				*exitCode = c.fail(result.OperationProfilesGet, *jsonOutput, result.CodeInvalidConfiguration, err.Error(), map[string]any{"name": args[0]})
			} else {
				*exitCode = c.profileSuccess(result.OperationProfilesGet, *jsonOutput, doc)
			}
		},
	}
	command.AddCommand(get)
	command.AddCommand(&cobra.Command{
		Use: "list", Short: "List local Generation Profiles", Args: cobra.NoArgs,
		Annotations: map[string]string{operationAnnotation: result.OperationProfilesList},
		Run: func(_ *cobra.Command, _ []string) {
			items, err := profiles.List()
			if err != nil {
				*exitCode = c.fail(result.OperationProfilesList, *jsonOutput, result.CodeInvalidConfiguration, err.Error(), nil)
				return
			}
			if *jsonOutput {
				*exitCode = c.writeResult(result.OperationProfilesList, struct {
					Profiles []profiles.Summary `json:"profiles"`
				}{Profiles: items})
				return
			}
			for _, item := range items {
				if _, err := fmt.Fprintf(c.stdout, "%s\t%v\n", item.Name, item.Sections); err != nil {
					*exitCode = c.fail(result.OperationProfilesList, false, result.CodeOutputWriteFailed, err.Error(), nil)
					return
				}
			}
			*exitCode = result.ExitSuccess
		},
	})
	var deleteYes bool
	deleteCommand := &cobra.Command{
		Use: "delete NAME", Short: "Delete a local Generation Profile", Args: cobra.ExactArgs(1),
		Annotations: map[string]string{operationAnnotation: result.OperationProfilesDelete},
		Run: func(_ *cobra.Command, args []string) {
			name := args[0]
			if !profiles.ValidName(name) || !deleteYes {
				*exitCode = c.fail(result.OperationProfilesDelete, *jsonOutput, result.CodeInvalidRequest, "valid profile name and --yes are required", nil)
				return
			}
			if err := profiles.Delete(name); errors.Is(err, os.ErrNotExist) {
				*exitCode = c.fail(result.OperationProfilesDelete, *jsonOutput, result.CodeNotFound, fmt.Sprintf("profile %q was not found", name), nil)
			} else if err != nil {
				*exitCode = c.fail(result.OperationProfilesDelete, *jsonOutput, result.CodeConfigurationWriteFailed, err.Error(), map[string]any{"name": name})
			} else if *jsonOutput {
				*exitCode = c.writeResult(result.OperationProfilesDelete, map[string]string{"name": name})
			} else {
				if _, err := fmt.Fprintln(c.stdout, name); err != nil {
					*exitCode = c.fail(result.OperationProfilesDelete, false, result.CodeOutputWriteFailed, err.Error(), nil)
				} else {
					*exitCode = result.ExitSuccess
				}
			}
		},
	}
	deleteCommand.Flags().BoolVar(&deleteYes, "yes", false, "approve deleting the profile")
	command.AddCommand(deleteCommand)
	return command
}

func (c *CLI) profileSuccess(operation string, jsonOutput bool, doc profiles.Document) int {
	if jsonOutput {
		return c.writeResult(operation, doc)
	}
	if operation == result.OperationProfilesGet {
		encoded, err := json.Marshal(doc)
		if err != nil {
			return c.fail(operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
		}
		if _, err := fmt.Fprintln(c.stdout, string(encoded)); err != nil {
			return c.fail(operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
		}
		return result.ExitSuccess
	}
	if _, err := fmt.Fprintln(c.stdout, doc.Name); err != nil {
		return c.fail(operation, false, result.CodeOutputWriteFailed, err.Error(), nil)
	}
	return result.ExitSuccess
}
