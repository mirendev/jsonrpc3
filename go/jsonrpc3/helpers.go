package jsonrpc3

// MethodInfo contains metadata about a registered method for introspection.
type MethodInfo struct {
	Name        string
	Description string
	Params      any // Can be map[string]string for named params or []string for positional params
	Category    string
	handler     func(params Params, caller Caller) (any, error)
}

// MethodMap is a simple Object implementation that dispatches methods to functions.
// This is useful for creating simple handlers and for testing.
// It automatically supports the optional $methods and $type introspection methods.
type MethodMap struct {
	methods map[string]*MethodInfo
	Type    string // Optional type identifier for $type introspection
}

// RegisterOption is a functional option for method registration.
type RegisterOption func(*MethodInfo)

// WithDescription sets the description for a method.
func WithDescription(description string) RegisterOption {
	return func(info *MethodInfo) {
		info.Description = description
	}
}

// WithParams sets the parameter types for a method using named parameters.
// The params map should map parameter names to their type descriptions.
// Example: WithParams(map[string]string{"a": "number", "b": "number"})
func WithParams(params map[string]string) RegisterOption {
	return func(info *MethodInfo) {
		info.Params = params
	}
}

// WithPositionalParams sets the parameter types for a method using positional parameters.
// Example: WithPositionalParams([]string{"number", "number"})
func WithPositionalParams(params []string) RegisterOption {
	return func(info *MethodInfo) {
		info.Params = params
	}
}

// WithCategory sets the category for a method.
// Example: WithCategory("math")
func WithCategory(category string) RegisterOption {
	return func(info *MethodInfo) {
		info.Category = category
	}
}

// NewMethodMap creates a new MethodMap.
func NewMethodMap() *MethodMap {
	return &MethodMap{
		methods: make(map[string]*MethodInfo),
	}
}

// Register registers a method handler function with optional metadata.
// Use WithDescription and WithParams/WithPositionalParams options to provide introspection metadata.
//
// Example:
//
//	m.Register("add", addHandler,
//	    WithDescription("Adds two numbers"),
//	    WithParams(map[string]string{"a": "number", "b": "number"}))
func (m *MethodMap) Register(method string, fn func(params Params, caller Caller) (any, error), opts ...RegisterOption) {
	info := &MethodInfo{
		Name:    method,
		handler: fn,
	}

	for _, opt := range opts {
		opt(info)
	}

	m.methods[method] = info
}

// CallMethod implements the Object interface.
// It automatically handles the $methods and $type introspection methods.
func (m *MethodMap) CallMethod(method string, params Params, caller Caller) (any, error) {
	// Handle introspection methods
	switch method {
	case "$methods":
		return m.getMethods(), nil
	case "$type":
		return m.getType(), nil
	}

	info, exists := m.methods[method]
	if !exists {
		return nil, NewMethodNotFoundError(method)
	}
	return info.handler(params, caller)
}

// getMethods returns detailed information about all methods supported by this object.
func (m *MethodMap) getMethods() []map[string]any {
	methods := make([]map[string]any, 0, len(m.methods)+2)

	// Add user-registered methods
	for _, info := range m.methods {
		methodInfo := map[string]any{
			"name": info.Name,
		}

		if info.Description != "" {
			methodInfo["description"] = info.Description
		}

		if info.Params != nil {
			methodInfo["params"] = info.Params
		}

		if info.Category != "" {
			methodInfo["category"] = info.Category
		}

		methods = append(methods, methodInfo)
	}

	// Add introspection methods
	methods = append(methods,
		map[string]any{"name": "$methods"},
		map[string]any{"name": "$type"},
	)

	return methods
}

// getType returns the type string for this object.
func (m *MethodMap) getType() string {
	if m.Type != "" {
		return m.Type
	}
	return "MethodMap"
}
