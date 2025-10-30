package jsonrpc3

// MethodMap is a simple Object implementation that dispatches methods to functions.
// This is useful for creating simple handlers and for testing.
// It automatically supports the optional $methods and $type introspection methods.
type MethodMap struct {
	methods map[string]func(params Params) (any, error)
	Type    string // Optional type identifier for $type introspection
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
// It automatically handles the $methods and $type introspection methods.
func (m *MethodMap) CallMethod(method string, params Params) (any, error) {
	// Handle introspection methods
	switch method {
	case "$methods":
		return m.getMethods(), nil
	case "$type":
		return m.getType(), nil
	}

	fn, exists := m.methods[method]
	if !exists {
		return nil, NewMethodNotFoundError(method)
	}
	return fn(params)
}

// getMethods returns a list of all method names supported by this object.
func (m *MethodMap) getMethods() []string {
	methods := make([]string, 0, len(m.methods)+2)

	// Add user-registered methods
	for name := range m.methods {
		methods = append(methods, name)
	}

	// Add introspection methods
	methods = append(methods, "$methods", "$type")

	return methods
}

// getType returns the type string for this object.
func (m *MethodMap) getType() string {
	if m.Type != "" {
		return m.Type
	}
	return "MethodMap"
}
