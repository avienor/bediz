package capability

import (
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/result"
)

func TestMatrixAdvertisesOnlyImplementedOperations(t *testing.T) {
	want := []string{
		result.OperationModelsList,
		result.OperationImagesList,
		result.OperationImagesGet,
		result.OperationImagesUpload,
		result.OperationQueueList,
		result.OperationQueueGet,
	}
	got := make([]string, len(Matrix))
	for i, entry := range Matrix {
		got[i] = entry.Operation
	}

	if !slices.Equal(got, want) {
		t.Fatalf("advertised operations = %q, want implemented operations %q", got, want)
	}
}

func TestSupportsInvokeAI(t *testing.T) {
	tests := []struct {
		version string
		want    bool
		wantErr bool
	}{
		// Supported range boundaries and stable build metadata.
		{version: "6.14.1", want: true},
		{version: "6.14.9", want: true},
		{version: "6.14.1+build", want: true},
		{version: "6.14.1+build.1.2", want: true},

		// Unsupported minor and major versions, including the exclusive upper boundary.
		{version: "6.13.9", want: false},
		{version: "6.14.0", want: false},
		{version: "6.15.0", want: false},
		{version: "7.0.0", want: false},

		// Ordinary prereleases are well-formed but unsupported.
		{version: "6.14.1-rc1", want: false},
		{version: "6.14.1-rc.1", want: false},
		{version: "6.14.1-0", want: false},
		{version: "6.14.1-0a", want: false},
		{version: "6.14.1-alpha-2", want: false},

		// Empty and malformed versions.
		{version: "", wantErr: true},
		{version: "v6.14.1", wantErr: true},
		{version: "6.14", wantErr: true},
		{version: "6.14.1.2", wantErr: true},
		{version: "6.014.1", wantErr: true},
		{version: "6.14.01", wantErr: true},
		{version: "6.14.1-", wantErr: true},
		{version: "6.14.1-alpha.", wantErr: true},
		{version: "6.14.1-alpha..1", wantErr: true},
		{version: "6.14.1-01", wantErr: true},
		{version: "6.14.1-rc.01", wantErr: true},
		{version: "6.14.1+", wantErr: true},
		{version: "6.14.1+.", wantErr: true},
		{version: "latest", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			got, err := SupportsInvokeAI(test.version)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("supported = %t, want %t", got, test.want)
			}
		})
	}
}
