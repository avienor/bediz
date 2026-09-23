package graphops

import (
	"encoding/json"
	"slices"
)

// ModelReference is the complete identifier shape accepted by invocation inputs.
type ModelReference struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
	Name string `json:"name"`
	Base string `json:"base"`
	Type string `json:"type"`
}

func Reference(model ModelIdentifier) ModelReference {
	return ModelReference{Key: model.Key, Hash: model.Hash, Name: model.Name, Base: model.Base, Type: model.Type}
}

var sdxlSchedulers = []string{
	"ddim", "ddpm", "deis", "deis_k", "lms", "lms_k", "pndm", "heun", "heun_k", "euler", "euler_k", "euler_a",
	"kdpm_2", "kdpm_2_k", "kdpm_2_a", "kdpm_2_a_k", "dpmpp_2s", "dpmpp_2s_k", "dpmpp_2m", "dpmpp_2m_k",
	"dpmpp_2m_sde", "dpmpp_2m_sde_k", "dpmpp_3m", "dpmpp_3m_k", "dpmpp_sde", "dpmpp_sde_k", "er_sde",
	"unipc", "unipc_k", "lcm", "tcd",
}

// IsSDXLScheduler reports whether name is in the SDXL scheduler list shared by
// SDXL generation and SD1.5 and SDXL generative upscale.
func IsSDXLScheduler(name string) bool {
	return slices.Contains(sdxlSchedulers, name)
}

type EdgeConnection struct {
	NodeID string `json:"node_id"`
	Field  string `json:"field"`
}

type Edge struct {
	Source      EdgeConnection `json:"source"`
	Destination EdgeConnection `json:"destination"`
}

func Connect(sourceNode, sourceField, destinationNode, destinationField string) Edge {
	return Edge{
		Source:      EdgeConnection{NodeID: sourceNode, Field: sourceField},
		Destination: EdgeConnection{NodeID: destinationNode, Field: destinationField},
	}
}

type Graph struct {
	ID    string         `json:"id"`
	Nodes map[string]any `json:"nodes"`
	Edges []Edge         `json:"edges"`
}

type NodeAttributes struct {
	ID             string `json:"id"`
	IsIntermediate bool   `json:"is_intermediate"`
	UseCache       bool   `json:"use_cache"`
	Type           string `json:"type"`
}

type StringNode struct {
	NodeAttributes
	Value string `json:"value"`
}

type IntegerNode struct {
	NodeAttributes
	Value uint32 `json:"value"`
}

type BoardField struct {
	BoardID string `json:"board_id"`
}

type Batch struct {
	Origin      string         `json:"origin"`
	Destination string         `json:"destination"`
	Graph       Graph          `json:"graph"`
	Data        [][]BatchDatum `json:"data"`
	Runs        int            `json:"runs"`
}

type BatchDatum struct {
	NodePath  string   `json:"node_path"`
	FieldName string   `json:"field_name"`
	Items     []uint32 `json:"items"`
	// StringItems represents stock batch fields such as upscale prompts. It is
	// encoded as the same "items" field, without changing numeric seed batches.
	StringItems []string `json:"-"`
}

func (datum BatchDatum) MarshalJSON() ([]byte, error) {
	if datum.StringItems != nil {
		return json.Marshal(struct {
			NodePath  string   `json:"node_path"`
			FieldName string   `json:"field_name"`
			Items     []string `json:"items"`
		}{NodePath: datum.NodePath, FieldName: datum.FieldName, Items: datum.StringItems})
	}
	type numericBatchDatum BatchDatum
	return json.Marshal(numericBatchDatum(datum))
}

// SeedField identifies the batch field whose value is checked on each queue item.
type SeedField struct {
	NodePath  string
	FieldName string
}

type EnqueueRequest struct {
	Batch Batch `json:"batch"`
}

// SeedBatchData matches InvokeAI's newest-first enqueue item ordering.
func SeedBatchData(nodePath, fieldName string, seeds []uint32) [][]BatchDatum {
	items := slices.Clone(seeds)
	slices.Reverse(items)
	return [][]BatchDatum{{{NodePath: nodePath, FieldName: fieldName, Items: items}}}
}
