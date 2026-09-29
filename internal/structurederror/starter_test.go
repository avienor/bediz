package structurederror_test

import (
	"errors"
	"testing"

	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/models"
	"github.com/avienor/bediz/internal/result"
	"github.com/avienor/bediz/internal/structurederror"
)

func TestStarterSubmissionFailures(t *testing.T) {
	tests := []struct {
		name string
		err  *models.StarterSubmissionError
		want structurederror.Failure
	}{
		{
			name: "uncertain main job without progress",
			err:  &models.StarterSubmissionError{Role: "main", Cause: &httpclient.OutcomeUnknownError{Err: errors.New("connection lost")}},
			want: structurederror.Failure{Error: result.Error{
				Code: "outcome_unknown", Message: "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again",
				Details: map[string]any{"jobs": []models.InstallJob(nil), "skipped": (*[]models.InstallSkip)(nil), "uncertain_role": "main"},
			}},
		},
		{
			name: "uncertain dependency retains accepted and skipped jobs",
			err: &models.StarterSubmissionError{
				Role: "dependency", DependencyIndex: new(2), Cause: &httpclient.OutcomeUnknownError{StatusCode: 502, Err: errors.New("gateway failure")},
				Progress: models.InstallResult{
					Jobs:    []models.InstallJob{{JobID: 17, Status: "waiting", SourceType: "url", Role: "main"}},
					Skipped: new([]models.InstallSkip{{Role: "dependency", DependencyIndex: new(1), Reason: "already_installed"}}),
				},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "outcome_unknown", Message: "InvokeAI may have accepted the installation; inspect the current model inventory and install job list before submitting again",
				Details: map[string]any{
					"jobs":           []models.InstallJob{{JobID: 17, Status: "waiting", SourceType: "url", Role: "main"}},
					"skipped":        new([]models.InstallSkip{{Role: "dependency", DependencyIndex: new(1), Reason: "already_installed"}}),
					"uncertain_role": "dependency", "uncertain_dependency_index": 2, "status": 502,
				},
			}},
		},
		{
			name: "rejected main job takes precedence over HTTP authentication",
			err:  &models.StarterSubmissionError{Role: "main", Cause: &httpclient.HTTPError{StatusCode: 401, Detail: "private install detail"}},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: "InvokeAI rejected a starter installation job; reinspect the catalog before retrying",
				Details: map[string]any{"jobs": []models.InstallJob(nil), "skipped": (*[]models.InstallSkip)(nil), "rejected_role": "main", "status": 401},
			}},
		},
		{
			name: "rejected dependency without HTTP status",
			err: &models.StarterSubmissionError{
				Role: "dependency", DependencyIndex: new(0), Cause: errors.New("submission rejected"),
				Progress: models.InstallResult{Jobs: []models.InstallJob{}, Skipped: new([]models.InstallSkip{})},
			},
			want: structurederror.Failure{Error: result.Error{
				Code: "invokeai_operation_failed", Message: "InvokeAI rejected a starter installation job; reinspect the catalog before retrying",
				Details: map[string]any{"jobs": []models.InstallJob{}, "skipped": new([]models.InstallSkip{}), "rejected_role": "dependency", "rejected_dependency_index": 0},
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertFailure(t, structurederror.Classify(result.OperationModelsInstall, test.err), test.want)
		})
	}
}
