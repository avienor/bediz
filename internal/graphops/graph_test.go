package graphops_test

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/avienor/bediz/internal/graphops"
)

func TestBatchDatumPreservesNumericSeedEncodingAndSupportsStockPromptItems(t *testing.T) {
	numeric := graphops.BatchDatum{NodePath: "seed", FieldName: "value", Items: []uint32{42}}
	stringValue := graphops.BatchDatum{NodePath: "positive_prompt", FieldName: "value", StringItems: []string{"mountain landscape"}}
	for _, test := range []struct {
		name string
		item graphops.BatchDatum
		want string
	}{
		{"generation seed", numeric, `{"node_path":"seed","field_name":"value","items":[42]}`},
		{"upscale prompt", stringValue, `{"node_path":"positive_prompt","field_name":"value","items":["mountain landscape"]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			for name, marshal := range map[string]func(any) ([]byte, error){"json v1": json.Marshal, "json v2": func(value any) ([]byte, error) { return jsonv2.Marshal(value) }} {
				actual, err := marshal(test.item)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, []byte(test.want)) {
					t.Fatalf("%s: %s, want %s", name, actual, test.want)
				}
			}
		})
	}
}
