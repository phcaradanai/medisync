package vending

// NewClientFromConfig returns the real or fake vending client.
func NewClientFromConfig(baseURL, apiKey string, fake bool) Client {
	if fake {
		return NewFakeClient()
	}
	return NewClient(baseURL, apiKey)
}
