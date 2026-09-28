package generation

type imageField struct {
	ImageName string `json:"image_name"`
}

type imageResizeNode struct {
	nodeAttributes
	Image        imageField `json:"image"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	ResampleMode string     `json:"resample_mode"`
}
