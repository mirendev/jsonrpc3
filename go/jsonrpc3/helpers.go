package jsonrpc3

// MethodMap is a simple Object implementation that dispatches methods to functions.
// This is useful for creating simple handlers and for testing.
type MethodMap struct {
	methods map[string]func(params Params) (any, error)
}

// NewMethodMap creates a new MethodMap.
func NewMethodMap() *MethodMap {
	return &MethodMap{
		methods: make(map[string]func(params Params) (any, error)),
	}
}

// Register registers a method handler function.
func (m *MethodMap) Register(method string, fn func(params Params) (any, error)) {
	m.methods[method] = fn
}

// CallMethod implements the Object interface.
func (m *MethodMap) CallMethod(method string, params Params) (any, error) {
	fn, exists := m.methods[method]
	if !exists {
		return nil, NewMethodNotFoundError(method)
	}
	return fn(params)
}
