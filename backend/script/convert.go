package script

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Struct собирает значение, к полям которого скрипт обращается через точку.
// Структуры, а не словари, — чтобы скрипт писал f.user.is_verified, а опечатка
// в имени поля падала громко, а не возвращала молча None.
func Struct(name string, fields starlark.StringDict) starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String(name), fields)
}

// Dict отдаёт конфигурацию словарём, чтобы скрипт мог писать
// f.config.get("reward", 0) и продолжал работать, когда ключ так и не задали.
func Dict(cfg map[string]interface{}) starlark.Value {
	d := starlark.NewDict(len(cfg))
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_ = d.SetKey(starlark.String(k), ToStarlark(cfg[k]))
	}
	return d
}

// ToStarlark конвертирует значение, разобранное из JSON. Всё, чего он не
// распознал, становится строковым Go-представлением, а не ошибкой: ключ
// конфигурации, который скрипт не использует, не должен ломать хук.
func ToStarlark(v interface{}) starlark.Value {
	switch t := v.(type) {
	case nil:
		return starlark.None
	case bool:
		return starlark.Bool(t)
	case string:
		return starlark.String(t)
	case float64:
		return starlark.Float(t)
	case int:
		return starlark.MakeInt(t)
	case int64:
		return starlark.MakeInt64(t)
	case []interface{}:
		items := make([]starlark.Value, 0, len(t))
		for _, item := range t {
			items = append(items, ToStarlark(item))
		}
		return starlark.NewList(items)
	case map[string]interface{}:
		return Dict(t)
	default:
		return starlark.String(fmt.Sprint(t))
	}
}

// ToGo конвертирует обратно — для манифеста, читаемого при загрузке.
func ToGo(v starlark.Value) interface{} {
	switch t := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(t)
	case starlark.String:
		return string(t)
	case starlark.Int:
		i, _ := t.Int64()
		return float64(i)
	case starlark.Float:
		return float64(t)
	case *starlark.List:
		out := make([]interface{}, 0, t.Len())
		for i := 0; i < t.Len(); i++ {
			out = append(out, ToGo(t.Index(i)))
		}
		return out
	case *starlark.Dict:
		out := make(map[string]interface{}, t.Len())
		for _, k := range t.Keys() {
			value, _, _ := t.Get(k)
			out[fmt.Sprint(ToGo(k))] = ToGo(value)
		}
		return out
	default:
		return v.String()
	}
}

// HasRole повторяет проверку роли так, как её понимает Go, и отдаёт её
// скриптам обеих областей: и услуга, и ачивка спрашивают об этом одинаково.
func HasRole() *starlark.Builtin {
	return starlark.NewBuiltin("has_role", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var actor starlark.Value
		var role string
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "actor", &actor, "role", &role); err != nil {
			return nil, err
		}
		s, ok := actor.(*starlarkstruct.Struct)
		if !ok {
			return starlark.False, nil
		}
		if primary, err := s.Attr("role"); err == nil {
			if str, ok := primary.(starlark.String); ok && string(str) == role {
				return starlark.True, nil
			}
		}
		rolesAttr, err := s.Attr("roles")
		if err != nil {
			return starlark.False, nil
		}
		list, ok := rolesAttr.(*starlark.List)
		if !ok {
			return starlark.False, nil
		}
		for i := 0; i < list.Len(); i++ {
			if str, ok := list.Index(i).(starlark.String); ok && string(str) == role {
				return starlark.True, nil
			}
		}
		return starlark.False, nil
	})
}

// Duration отдаёт конструкторы интервалов в секундах: скрипт сравнивает метки
// времени, которые ядро передаёт секундами, и должен делать это читаемо —
// `o.confirmed_at - o.created_at > minutes(20)`.
func Duration(name string, seconds int64) *starlark.Builtin {
	return starlark.NewBuiltin(name, func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var count float64
		if err := starlark.UnpackArgs(b.Name(), args, kwargs, "count", &count); err != nil {
			return nil, err
		}
		return starlark.MakeInt64(int64(count * float64(seconds))), nil
	})
}
