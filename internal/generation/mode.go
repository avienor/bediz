package generation

func applyModeDefaults(request Request) Request {
	if request.Source != nil && request.Strength == nil {
		request.Strength = new(0.75)
	}
	return request
}
