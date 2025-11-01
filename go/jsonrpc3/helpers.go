package jsonrpc3

// MethodInfo contains metadata about a registered method for introspection.
type MethodInfo struct {
	Name        string
	Description string
	Params      any // Can be map[string]string for named params or []string for positional params
	handler     func(params Params, caller Caller) (any, error)
}

// MethodMap is a simple Object implementation that dispatches methods to functions.
// This is useful for creating simple handlers and for testing.
// It automatically supports the optional $methods, $type, and $method introspection methods.
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
// It automatically handles the $methods, $type, and $method introspection methods.
func (m *MethodMap) CallMethod(method string, params Params, caller Caller) (any, error) {
	// Handle introspection methods
	switch method {
	case "$methods":
		return m.getMethods(), nil
	case "$type":
		return m.getType(), nil
	case "$method":
		return m.getMethodInfo(params)
	}

	info, exists := m.methods[method]
	if !exists {
		return nil, NewMethodNotFoundError(method)
	}
	return info.handler(params, caller)
}

// getMethods returns a list of all method names supported by this object.
func (m *MethodMap) getMethods() []string {
	methods := make([]string, 0, len(m.methods)+3)

	// Add user-registered methods
	for name := range m.methods {
		methods = append(methods, name)
	}

	// Add introspection methods
	methods = append(methods, "$methods", "$type", "$method")

	return methods
}

// getType returns the type string for this object.
func (m *MethodMap) getType() string {
	if m.Type != "" {
		return m.Type
	}
	return "MethodMap"
}

// getMethodInfo returns detailed information about a specific method.
// Implements the $method introspection method.
func (m *MethodMap) getMethodInfo(params Params) (any, error) {
	var methodName string
	if err := params.Decode(&methodName); err != nil {
		return nil, NewInvalidParamsError("$method expects a string parameter")
	}

	info, exists := m.methods[methodName]
	if !exists {
		return nil, nil // Return null for non-existent methods
	}

	// Build result object with only non-empty fields
	result := map[string]any{
		"name": info.Name,
	}

	if info.Description != "" {
		result["description"] = info.Description
	}

	if info.Params != nil {
		result["params"] = info.Params
	}

	return result, nil
}
