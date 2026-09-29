package graphops

import (
	"context"

	"github.com/avienor/bediz/internal/capability"
	"github.com/avienor/bediz/internal/compatibility"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/operation"
)

// CheckInvocations verifies the operation's invocation vocabulary in live OpenAPI.
func CheckInvocations(ctx context.Context, client *httpclient.Client, requirements []capability.InvocationRequirement) error {
	return CheckRequirements(ctx, client, nil, requirements)
}

// CheckRequirements verifies graph vocabulary and endpoints before a source upload.
func CheckRequirements(ctx context.Context, client *httpclient.Client, endpoints []capability.EndpointRequirement, requirements []capability.InvocationRequirement) error {
	return CheckEntry(ctx, client, capability.Entry{Endpoints: endpoints, Invocations: requirements})
}

// CheckEntry evaluates the operation's existing OpenAPI preflight requirements.
// The supported version and chosen models have already been checked upstream.
func CheckEntry(ctx context.Context, client *httpclient.Client, entry capability.Entry) error {
	var document compatibility.Document
	if err := client.GetJSON(ctx, "/openapi.json", &document); err != nil {
		return err
	}
	entry.Models = nil
	entry.Special = nil
	failures := compatibility.Evaluate(entry, compatibility.Snapshot{SupportedVersion: true, OpenAPIAvailable: true, Document: document})
	if len(failures) > 0 {
		return operation.UnsupportedCapability(failures[0].Message)
	}
	return nil
}
