package rpc

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	// ErrMethodNotFound is returned when a method is not registered.
	ErrMethodNotFound = errors.New("rpc: method not found")

	// ErrDuplicateMethod is returned when registering an already-registered method.
	ErrDuplicateMethod = errors.New("rpc: duplicate method")

	// ErrInvalidParams is returned when a method receives the wrong number of params.
	ErrInvalidParams = errors.New("rpc: invalid params")
)

// MethodHandler is a function that handles an RPC method call.
type MethodHandler func(params []interface{}) (interface{}, error)

// Middleware wraps a method call, allowing pre/post processing.
// It receives the method name, params, and the next handler to call.
type Middleware func(method string, params []interface{}, next MethodHandler) (interface{}, error)

// MethodInfo describes a registered RPC method.
type MethodInfo struct {
	Name        string
	Handler     MethodHandler
	Description string
	ParamCount  int // expected param count; -1 for variadic
	Deprecated  bool
	Namespace   string // e.g. "eth", "debug", "admin"
}

// MethodRegistry is a thread-safe registry for RPC methods with middleware support.
type MethodRegistry struct {
	mu         sync.RWMutex
	methods    map[string]MethodInfo
	middleware []Middleware
}

// NewMethodRegistry creates a new, empty method registry.
func NewMethodRegistry() *MethodRegistry {
	return &MethodRegistry{
		methods: make(map[string]MethodInfo),
	}
}

// Register adds a method to the registry. Returns ErrDuplicateMethod if
// a method with the same name is already registered.
func (r *MethodRegistry) Register(info MethodInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.methods[info.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateMethod, info.Name)
	}
	r.methods[info.Name] = info
	return nil
}

// RegisterBatch registers multiple methods at once. If any registration
// fails, it returns the first error and no further methods are registered.
// Methods registered before the failure remain registered.
func (r *MethodRegistry) RegisterBatch(methods []MethodInfo) error {
	for _, m := range methods {
		if err := r.Register(m); err != nil {
			return err
		}
	}
	return nil
}

// Unregister removes a method from the registry. Returns true if the
// method was found and removed, false if it did not exist.
func (r *MethodRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.methods[name]; !exists {
		return false
	}
	delete(r.methods, name)
	return true
}

// Call dispatches a method call through the middleware chain and then to
// the registered handler. Returns ErrMethodNotFound if the method is not
// registered, or ErrInvalidParams if the param count does not match.
func (r *MethodRegistry) Call(method string, params []interface{}) (interface{}, error) {
	r.mu.RLock()
	info, exists := r.methods[method]
	mw := make([]Middleware, len(r.middleware))
	copy(mw, r.middleware)
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrMethodNotFound, method)
	}

	// Validate param count if not variadic.
	if info.ParamCount >= 0 && len(params) != info.ParamCount {
		return nil, fmt.Errorf("%w: %s expects %d params, got %d",
			ErrInvalidParams, method, info.ParamCount, len(params))
	}

	// Build the handler chain with middleware.
	handler := info.Handler
	// Apply middleware in reverse order so that the first added middleware
	// is the outermost (executes first).
	for i := len(mw) - 1; i >= 0; i-- {
		currentMW := mw[i]
		next := handler
		handler = func(p []interface{}) (interface{}, error) {
			return currentMW(method, p, next)
		}
	}

	return handler(params)
}

// Methods returns a sorted list of all registered method names.
func (r *MethodRegistry) Methods() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.methods))
	for name := range r.methods {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MethodsByNamespace returns a sorted list of method names belonging to
// the given namespace.
func (r *MethodRegistry) MethodsByNamespace(ns string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name, info := range r.methods {
		if info.Namespace == ns {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// HasMethod returns true if the named method is registered.
func (r *MethodRegistry) HasMethod(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.methods[name]
	return exists
}

// AddMiddleware appends a middleware to the chain. Middleware are executed
// in the order they are added (first added = outermost).
func (r *MethodRegistry) AddMiddleware(mw Middleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.middleware = append(r.middleware, mw)
}

// MethodCount returns the number of registered methods.
func (r *MethodRegistry) MethodCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.methods)
}

// GetMethodInfo returns the MethodInfo for the named method, if it exists.
// The second return value indicates whether the method was found.
func (r *MethodRegistry) GetMethodInfo(name string) (MethodInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.methods[name]
	return info, ok
}

// NamespaceFromMethod extracts the namespace from a method name.
// For example, "eth_blockNumber" returns "eth".
func NamespaceFromMethod(method string) string {
	idx := strings.Index(method, "_")
	if idx < 0 {
		return ""
	}
	return method[:idx]
}
