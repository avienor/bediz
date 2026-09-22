package capability

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/avienor/bediz/internal/result"
)

const SupportedInvokeAIRange = ">= 6.14.1, < 6.15.0"

type EndpointRequirement struct {
	Method string
	Path   string
}

type InvocationRequirement struct {
	Schema     string
	Type       string
	Properties []string
}

type ModelRequirement struct {
	Name         string
	Types        []string
	Bases        []string
	MinimumCount int
}

type VersionPolicy string

const (
	VersionPolicySupportedRange     VersionPolicy = "supported_range"
	VersionPolicyCompatibleEndpoint VersionPolicy = "compatible_endpoint"
)

type Entry struct {
	Operation     string
	Family        string
	UISync        string
	VersionPolicy VersionPolicy
	Endpoints     []EndpointRequirement
	Invocations   []InvocationRequirement
	Models        []ModelRequirement
}

var Matrix = []Entry{
	{
		Operation:     result.OperationModelsList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v2/models/"},
		},
	},
	{
		Operation:     result.OperationImagesList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/images/"},
		},
	},
	{
		Operation:     result.OperationImagesGet,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
	{
		Operation:     result.OperationImagesUpload,
		VersionPolicy: VersionPolicySupportedRange,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/app/version"},
			{Method: "POST", Path: "/api/v1/images/upload"},
		},
	},
	{
		Operation:     result.OperationQueueList,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/item_ids"},
			{Method: "POST", Path: "/api/v1/queue/{queue_id}/item_summaries_by_ids"},
		},
	},
	{
		Operation:     result.OperationQueueGet,
		VersionPolicy: VersionPolicyCompatibleEndpoint,
		Endpoints: []EndpointRequirement{
			{Method: "GET", Path: "/api/v1/queue/{queue_id}/i/{item_id}"},
			{Method: "GET", Path: "/api/v1/images/i/{image_name}"},
		},
	},
}

var versionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?P<prerelease>[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func SupportsInvokeAI(version string) (bool, error) {
	major, minor, patch, err := parseVersion(version)
	if err != nil {
		return false, err
	}
	matches := versionPattern.FindStringSubmatch(version)
	if matches[versionPattern.SubexpIndex("prerelease")] != "" {
		return false, nil
	}
	return major == 6 && minor == 14 && patch >= 1, nil
}

func parseVersion(value string) (int, int, int, error) {
	matches := versionPattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
	}
	for identifier := range strings.SplitSeq(matches[versionPattern.SubexpIndex("prerelease")], ".") {
		if len(identifier) > 1 && identifier[0] == '0' && isNumericIdentifier(identifier) {
			return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
		}
	}
	values := make([]int, 3)
	for i := range values {
		parsed, err := strconv.Atoi(matches[i+1])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid InvokeAI version %q", value)
		}
		values[i] = parsed
	}
	return values[0], values[1], values[2], nil
}

func isNumericIdentifier(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
