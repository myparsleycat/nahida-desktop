package platform

// StringPtr returns a pointer to value, for bound models whose optional string
// fields are shared with the renderer.
func StringPtr(value string) *string { return &value }
