package rpc

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// -- Register and Call --

func TestMethodRegistry_RegisterAndCall(t *testing.T) {
	reg := NewMethodRegistry()

	err := reg.Register(MethodInfo{
		Name:       "eth_blockNumber",
		Handler:    func(params []interface{}) (interface{}, error) { return "0x10", nil },
		ParamCount: 0,
		Namespace:  "eth",
	})
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}

	result, err := reg.Call("eth_blockNumber", nil)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if result != "0x10" {
		t.Fatalf("result = %v, want 0x10", result)
	}
}

func TestMethodRegistry_CallWithParams(t *testing.T) {
	reg := NewMethodRegistry()

	err := reg.Register(MethodInfo{
		Name: "eth_getBalance",
		Handler: func(params []interface{}) (interface{}, error) {
			return fmt.Sprintf("balance:%v", params[0]), nil
		},
		ParamCount: 2,
		Namespace:  "eth",
	})
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}

	result, err := reg.Call("eth_getBalance", []interface{}{"0xabc", "latest"})
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if result != "balance:0xabc" {
		t.Fatalf("result = %v, want balance:0xabc", result)
	}
}

// -- Namespace grouping --

func TestMethodRegistry_NamespaceGrouping(t *testing.T) {
	reg := NewMethodRegistry()

	methods := []MethodInfo{
		{Name: "eth_blockNumber", Handler: noopHandler, Namespace: "eth"},
		{Name: "eth_getBalance", Handler: noopHandler, Namespace: "eth"},
		{Name: "eth_gasPrice", Handler: noopHandler, Namespace: "eth"},
		{Name: "debug_traceTransaction", Handler: noopHandler, Namespace: "debug"},
		{Name: "net_version", Handler: noopHandler, Namespace: "net"},
	}
	if err := reg.RegisterBatch(methods); err != nil {
		t.Fatalf("RegisterBatch error: %v", err)
	}

	ethMethods := reg.MethodsByNamespace("eth")
	if len(ethMethods) != 3 {
		t.Fatalf("eth methods count = %d, want 3", len(ethMethods))
	}

	debugMethods := reg.MethodsByNamespace("debug")
	if len(debugMethods) != 1 {
		t.Fatalf("debug methods count = %d, want 1", len(debugMethods))
	}

	netMethods := reg.MethodsByNamespace("net")
	if len(netMethods) != 1 {
		t.Fatalf("net methods count = %d, want 1", len(netMethods))
	}

	// Non-existent namespace.
	adminMethods := reg.MethodsByNamespace("admin")
	if len(adminMethods) != 0 {
		t.Fatalf("admin methods count = %d, want 0", len(adminMethods))
	}
}

// -- Middleware execution order --

func TestMethodRegistry_MiddlewareExecutionOrder(t *testing.T) {
	reg := NewMethodRegistry()

	var order []string

	reg.AddMiddleware(func(method string, params []interface{}, next MethodHandler) (interface{}, error) {
		order = append(order, "mw1-before")
		result, err := next(params)
		order = append(order, "mw1-after")
		return result, err
	})

	reg.AddMiddleware(func(method string, params []interface{}, next MethodHandler) (interface{}, error) {
		order = append(order, "mw2-before")
		result, err := next(params)
		order = append(order, "mw2-after")
		return result, err
	})

	reg.Register(MethodInfo{
		Name: "test_method",
		Handler: func(params []interface{}) (interface{}, error) {
			order = append(order, "handler")
			return "ok", nil
		},
		ParamCount: 0,
		Namespace:  "test",
	})

	result, err := reg.Call("test_method", nil)
	if err != nil {
		t.Fatalf("Call error: %v", err)
	}
	if result != "ok" {
		t.Fatalf("result = %v, want ok", result)
	}

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("order length = %d, want %d: %v", len(order), len(expected), order)
	}
	for i, want := range expected {
		if order[i] != want {
			t.Fatalf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

func TestMethodRegistry_MiddlewareCanShortCircuit(t *testing.T) {
	reg := NewMethodRegistry()

	reg.AddMiddleware(func(method string, params []interface{}, next MethodHandler) (interface{}, error) {
		return nil, errors.New("blocked")
	})

	reg.Register(MethodInfo{
		Name: "test_blocked",
		Handler: func(params []interface{}) (interface{}, error) {
			t.Fatal("handler should not be called")
			return nil, nil
		},
		ParamCount: 0,
		Namespace:  "test",
	})

	_, err := reg.Call("test_blocked", nil)
	if err == nil || err.Error() != "blocked" {
		t.Fatalf("expected 'blocked' error, got: %v", err)
	}
}

// -- Duplicate registration --

func TestMethodRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewMethodRegistry()

	err := reg.Register(MethodInfo{
		Name:    "eth_test",
		Handler: noopHandler,
	})
	if err != nil {
		t.Fatalf("first Register error: %v", err)
	}

	err = reg.Register(MethodInfo{
		Name:    "eth_test",
		Handler: noopHandler,
	})
	if !errors.Is(err, ErrDuplicateMethod) {
		t.Fatalf("expected ErrDuplicateMethod, got: %v", err)
	}
}

// -- Unregister --

func TestMethodRegistry_Unregister(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{
		Name:    "eth_remove",
		Handler: noopHandler,
	})

	if !reg.HasMethod("eth_remove") {
		t.Fatal("method should exist before unregister")
	}

	ok := reg.Unregister("eth_remove")
	if !ok {
		t.Fatal("Unregister should return true")
	}

	if reg.HasMethod("eth_remove") {
		t.Fatal("method should not exist after unregister")
	}

	// Unregister non-existent method.
	ok = reg.Unregister("eth_nonexistent")
	if ok {
		t.Fatal("Unregister of non-existent method should return false")
	}
}

// -- Param count validation --

func TestMethodRegistry_ParamCountValidation(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{
		Name:       "test_exact",
		Handler:    noopHandler,
		ParamCount: 2,
	})

	// Wrong number of params.
	_, err := reg.Call("test_exact", []interface{}{"only_one"})
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("expected ErrInvalidParams, got: %v", err)
	}

	// Correct number of params.
	_, err = reg.Call("test_exact", []interface{}{"a", "b"})
	if err != nil {
		t.Fatalf("Call with correct params error: %v", err)
	}

	// Zero params when 2 expected.
	_, err = reg.Call("test_exact", nil)
	if !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("expected ErrInvalidParams for nil params, got: %v", err)
	}
}

func TestMethodRegistry_VariadicParamCount(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{
		Name: "test_variadic",
		Handler: func(params []interface{}) (interface{}, error) {
			return len(params), nil
		},
		ParamCount: -1, // variadic
	})

	// Any number of params should work.
	for _, count := range []int{0, 1, 5, 10} {
		params := make([]interface{}, count)
		result, err := reg.Call("test_variadic", params)
		if err != nil {
			t.Fatalf("Call with %d params error: %v", count, err)
		}
		if result.(int) != count {
			t.Fatalf("expected %d, got %d", count, result)
		}
	}
}

// -- Method not found --

func TestMethodRegistry_MethodNotFound(t *testing.T) {
	reg := NewMethodRegistry()

	_, err := reg.Call("nonexistent", nil)
	if !errors.Is(err, ErrMethodNotFound) {
		t.Fatalf("expected ErrMethodNotFound, got: %v", err)
	}
}

// -- Deprecated method flagging --

func TestMethodRegistry_DeprecatedMethod(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{
		Name:       "eth_old",
		Handler:    noopHandler,
		Deprecated: true,
		Namespace:  "eth",
	})

	info, ok := reg.GetMethodInfo("eth_old")
	if !ok {
		t.Fatal("method should exist")
	}
	if !info.Deprecated {
		t.Fatal("method should be marked deprecated")
	}
}

// -- Batch registration --

func TestMethodRegistry_BatchRegistration(t *testing.T) {
	reg := NewMethodRegistry()

	methods := []MethodInfo{
		{Name: "batch_a", Handler: noopHandler, Namespace: "batch"},
		{Name: "batch_b", Handler: noopHandler, Namespace: "batch"},
		{Name: "batch_c", Handler: noopHandler, Namespace: "batch"},
	}

	err := reg.RegisterBatch(methods)
	if err != nil {
		t.Fatalf("RegisterBatch error: %v", err)
	}

	if reg.MethodCount() != 3 {
		t.Fatalf("MethodCount = %d, want 3", reg.MethodCount())
	}

	all := reg.Methods()
	if len(all) != 3 {
		t.Fatalf("Methods length = %d, want 3", len(all))
	}
}

func TestMethodRegistry_BatchRegistration_StopsOnDuplicate(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{Name: "existing", Handler: noopHandler})

	methods := []MethodInfo{
		{Name: "new_a", Handler: noopHandler},
		{Name: "existing", Handler: noopHandler}, // duplicate
		{Name: "new_b", Handler: noopHandler},     // should not be registered
	}

	err := reg.RegisterBatch(methods)
	if !errors.Is(err, ErrDuplicateMethod) {
		t.Fatalf("expected ErrDuplicateMethod, got: %v", err)
	}

	// "new_a" should have been registered before the error.
	if !reg.HasMethod("new_a") {
		t.Fatal("new_a should be registered")
	}
	// "new_b" should not have been registered.
	if reg.HasMethod("new_b") {
		t.Fatal("new_b should not be registered")
	}
}

// -- Concurrent access --

func TestMethodRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewMethodRegistry()

	// Pre-register a method.
	reg.Register(MethodInfo{
		Name: "concurrent_test",
		Handler: func(params []interface{}) (interface{}, error) {
			return "ok", nil
		},
		ParamCount: 0,
		Namespace:  "test",
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	// Concurrent calls.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := reg.Call("concurrent_test", nil)
			if err != nil {
				errCh <- err
				return
			}
			if result != "ok" {
				errCh <- fmt.Errorf("unexpected result: %v", result)
			}
		}()
	}

	// Concurrent registrations.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent_%d", idx)
			reg.Register(MethodInfo{
				Name:       name,
				Handler:    noopHandler,
				ParamCount: 0,
				Namespace:  "test",
			})
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.Methods()
			_ = reg.MethodCount()
			_ = reg.HasMethod("concurrent_test")
			_ = reg.MethodsByNamespace("test")
		}()
	}

	// Concurrent unregistrations (some may not exist).
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("concurrent_%d", idx)
			reg.Unregister(name)
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}

// -- Methods list sorting --

func TestMethodRegistry_MethodsSorted(t *testing.T) {
	reg := NewMethodRegistry()

	names := []string{"z_method", "a_method", "m_method", "b_method"}
	for _, n := range names {
		reg.Register(MethodInfo{Name: n, Handler: noopHandler})
	}

	all := reg.Methods()
	for i := 1; i < len(all); i++ {
		if all[i] < all[i-1] {
			t.Fatalf("Methods not sorted: %v", all)
		}
	}
}

// -- HasMethod --

func TestMethodRegistry_HasMethod(t *testing.T) {
	reg := NewMethodRegistry()

	if reg.HasMethod("foo") {
		t.Fatal("empty registry should not have method")
	}

	reg.Register(MethodInfo{Name: "foo", Handler: noopHandler})
	if !reg.HasMethod("foo") {
		t.Fatal("should have method after register")
	}
}

// -- MethodCount --

func TestMethodRegistry_MethodCount(t *testing.T) {
	reg := NewMethodRegistry()

	if reg.MethodCount() != 0 {
		t.Fatalf("empty registry count = %d, want 0", reg.MethodCount())
	}

	reg.Register(MethodInfo{Name: "a", Handler: noopHandler})
	reg.Register(MethodInfo{Name: "b", Handler: noopHandler})
	if reg.MethodCount() != 2 {
		t.Fatalf("count = %d, want 2", reg.MethodCount())
	}

	reg.Unregister("a")
	if reg.MethodCount() != 1 {
		t.Fatalf("count after unregister = %d, want 1", reg.MethodCount())
	}
}

// -- NamespaceFromMethod --

func TestNamespaceFromMethod(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{"eth_blockNumber", "eth"},
		{"debug_traceTransaction", "debug"},
		{"net_version", "net"},
		{"noNamespace", ""},
		{"web3_clientVersion", "web3"},
	}
	for _, tt := range tests {
		got := NamespaceFromMethod(tt.method)
		if got != tt.want {
			t.Errorf("NamespaceFromMethod(%q) = %q, want %q", tt.method, got, tt.want)
		}
	}
}

// -- Middleware receives correct method name --

func TestMethodRegistry_MiddlewareReceivesMethodName(t *testing.T) {
	reg := NewMethodRegistry()

	var receivedMethod string
	reg.AddMiddleware(func(method string, params []interface{}, next MethodHandler) (interface{}, error) {
		receivedMethod = method
		return next(params)
	})

	reg.Register(MethodInfo{
		Name:       "eth_test",
		Handler:    noopHandler,
		ParamCount: 0,
	})

	reg.Call("eth_test", nil)

	if receivedMethod != "eth_test" {
		t.Fatalf("middleware received method = %q, want eth_test", receivedMethod)
	}
}

// -- Handler error propagation --

func TestMethodRegistry_HandlerErrorPropagation(t *testing.T) {
	reg := NewMethodRegistry()

	expectedErr := errors.New("handler failed")
	reg.Register(MethodInfo{
		Name: "err_method",
		Handler: func(params []interface{}) (interface{}, error) {
			return nil, expectedErr
		},
		ParamCount: 0,
	})

	_, err := reg.Call("err_method", nil)
	if err != expectedErr {
		t.Fatalf("expected handler error, got: %v", err)
	}
}

// -- Description field --

func TestMethodRegistry_DescriptionField(t *testing.T) {
	reg := NewMethodRegistry()

	reg.Register(MethodInfo{
		Name:        "eth_chainId",
		Handler:     noopHandler,
		Description: "Returns the chain ID",
		ParamCount:  0,
		Namespace:   "eth",
	})

	info, ok := reg.GetMethodInfo("eth_chainId")
	if !ok {
		t.Fatal("method should exist")
	}
	if info.Description != "Returns the chain ID" {
		t.Fatalf("description = %q, want 'Returns the chain ID'", info.Description)
	}
}

// noopHandler is a handler that returns nil.
func noopHandler(params []interface{}) (interface{}, error) {
	return nil, nil
}
