package capability

import "testing"

func TestSupportsInvokeAI(t *testing.T) {
	tests := []struct {
		version string
		want    bool
		wantErr bool
	}{
		{version: "6.14.1", want: true},
		{version: "6.14.9", want: true},
		{version: "6.14.1+build", want: true},
		{version: "6.14.0", want: false},
		{version: "6.15.0", want: false},
		{version: "7.0.0", want: false},
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
