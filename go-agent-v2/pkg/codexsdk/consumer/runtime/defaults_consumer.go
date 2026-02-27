package runtime

import "reflect"

func SetDefaultFunc[T any](slot *T, fallback T) {
	if slot == nil {
		return
	}
	value := reflect.ValueOf(*slot)
	if value.IsValid() && value.Kind() == reflect.Func && !value.IsNil() {
		return
	}
	*slot = fallback
}
