package apiserver

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestRegisterMethods_AccountLoginCancelBoundToConcreteHandler(t *testing.T) {
	srv := &Server{methods: make(map[string]Handler)}
	srv.registerMethods()

	handler, ok := srv.methods["account/login/cancel"]
	if !ok || handler == nil {
		t.Fatalf("account/login/cancel handler not registered")
	}
	handlerName := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	if !strings.Contains(handlerName, "accountLoginCancel") {
		t.Fatalf("account/login/cancel should bind accountLoginCancel, got %s", handlerName)
	}
}
