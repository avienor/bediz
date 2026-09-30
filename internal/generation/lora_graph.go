package generation

type loRAMetadata struct {
	Model  modelReference `json:"model"`
	Weight float64        `json:"weight"`
}

type loRALoaderNode struct {
	nodeAttributes
	LoRA   modelReference `json:"lora"`
	Weight float64        `json:"weight"`
}
