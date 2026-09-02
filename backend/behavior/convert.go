package behavior

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// factsValue превращает Facts в аргумент `f`, который получает каждый хук.
// Используются структуры, а не словари, чтобы скрипт писал f.user.is_verified,
// а опечатка в имени поля падала громко, а не возвращала молча None.
func factsValue(f Facts) starlark.Value {
	d := starlark.StringDict{
		"event":      starlark.String(f.Event),
		"config":     configValue(f.Config),
		"user":       actorValue(f.User),
		"viewer":     actorValue(f.Viewer),
		"customer":   actorValue(f.Customer),
		"order":      orderValue(f.Order),
		"variant":    variantValue(f.Variant),
		"claims":     starlark.MakeInt(f.Claims),
		"submission": submissionValue(f.Submission),
		"now":        starlark.MakeInt64(f.Now.Unix()),
	}
	return starlarkstruct.FromStringDict(starlark.String("facts"), d)
}

// submissionValue отдаёт исход проверки данных. Только исход: значения, с
// которыми сравнивали отправку, в скрипт не попадают никогда.
func submissionValue(s *SubmissionFacts) starlark.Value {
	if s == nil {
		return starlark.None
	}
	matches := starlark.NewDict(len(s.Matches))
	keys := make([]string, 0, len(s.Matches))
	for field := range s.Matches {
		keys = append(keys, field)
	}
	sort.Strings(keys)
	for _, field := range keys {
		_ = matches.SetKey(starlark.String(field), starlark.Bool(s.Matches[field]))
	}
	return starlarkstruct.FromStringDict(starlark.String("submission"), starlark.StringDict{
		"attempt":   starlark.MakeInt(s.Attempt),
		"all_match": starlark.Bool(s.AllMatch),
		"matches":   matches,
		"escalated": starlark.Bool(s.Escalated),
	})
}

func actorValue(a *Actor) starlark.Value {
	if a == nil {
		return starlark.None
	}
	roles := make([]starlark.Value, 0, len(a.Roles))
	for _, r := range a.Roles {
		roles = append(roles, starlark.String(r))
	}
	return starlarkstruct.FromStringDict(starlark.String("actor"), starlark.StringDict{
		"id":          starlark.String(a.ID),
		"role":        starlark.String(a.Role),
		"roles":       starlark.NewList(roles),
		"is_verified": starlark.Bool(a.IsVerified),
		"age":         starlark.MakeInt(a.Age),
		"status":      starlark.String(a.Status),
	})
}

func orderValue(o *OrderFacts) starlark.Value {
	if o == nil {
		return starlark.None
	}
	executor := starlark.Value(starlark.None)
	if o.ExecutorID != "" {
		executor = starlark.String(o.ExecutorID)
	}
	return starlarkstruct.FromStringDict(starlark.String("order"), starlark.StringDict{
		"id":          starlark.String(o.ID),
		"status":      starlark.String(o.Status),
		"customer_id": starlark.String(o.CustomerID),
		"executor_id": executor,
		"amount":      starlark.Float(o.Amount),
		"is_urgent":   starlark.Bool(o.IsUrgent),
		"is_asap":     starlark.Bool(o.IsAsap),
	})
}

func variantValue(v *VariantFacts) starlark.Value {
	if v == nil {
		return starlark.None
	}
	return starlarkstruct.FromStringDict(starlark.String("variant"), starlark.StringDict{
		"id":         starlark.String(v.ID),
		"code":       starlark.String(v.Code),
		"base_price": starlark.Float(v.BasePrice),
	})
}

// configValue отдаёт конфигурацию узла словарём, чтобы скрипт мог писать
// f.config.get("reward", 0) и продолжал работать, когда ключ так и не задали.
func configValue(cfg map[string]interface{}) starlark.Value {
	d := starlark.NewDict(len(cfg))
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_ = d.SetKey(starlark.String(k), goToStarlark(cfg[k]))
	}
	return d
}

// goToStarlark конвертирует значение, разобранное из JSON. Всё, чего он не
// распознал, становится строковым Go-представлением, а не ошибкой: ключ
// конфигурации, который скрипт не использует, не должен ломать хук.
func goToStarlark(v interface{}) starlark.Value {
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
			items = append(items, goToStarlark(item))
		}
		return starlark.NewList(items)
	case map[string]interface{}:
		return configValue(t)
	default:
		return starlark.String(fmt.Sprint(t))
	}
}

// starlarkToGo конвертирует обратно — для манифеста, читаемого при загрузке.
func starlarkToGo(v starlark.Value) interface{} {
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
			out = append(out, starlarkToGo(t.Index(i)))
		}
		return out
	case *starlark.Dict:
		out := make(map[string]interface{}, t.Len())
		for _, k := range t.Keys() {
			value, _, _ := t.Get(k)
			out[fmt.Sprint(starlarkToGo(k))] = starlarkToGo(value)
		}
		return out
	default:
		return v.String()
	}
}

// effectValue — это Effect, пока он внутри скрипта: непрозрачный, нехешируемый и
// без читаемых полей, так что скрипт может только создать его и вернуть.
type effectValue struct{ effect Effect }

func (e *effectValue) String() string {
	return fmt.Sprintf("effect(%s)", e.effect.Kind)
}
func (e *effectValue) Type() string          { return "effect" }
func (e *effectValue) Freeze()               {}
func (e *effectValue) Truth() starlark.Bool  { return starlark.True }
func (e *effectValue) Hash() (uint32, error) { return 0, fmt.Errorf("effect is unhashable") }

// predeclared — окружение, в котором компилируется каждый скрипт. В нём
// конструкторы эффектов и больше ничего: ни print, ни load, ни ввода-вывода.
func predeclared() starlark.StringDict {
	return starlark.StringDict{
		"complete_order": starlark.NewBuiltin("complete_order", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var orderID, reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "order_id", &orderID, "reason?", &reason); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectCompleteOrder, OrderID: orderID, Reason: reason}}, nil
		}),
		"cancel_order": starlark.NewBuiltin("cancel_order", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var orderID, reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "order_id", &orderID, "reason?", &reason); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectCancelOrder, OrderID: orderID, Reason: reason}}, nil
		}),
		"pay_bonus": starlark.NewBuiltin("pay_bonus", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var to, key, reason, orderID string
			var amount float64
			// Комиссия — по явному согласию: вознаграждение не оплачено заказчиком,
			// поэтому из него ничего не удерживается, пока поведение не попросит.
			commission := false
			if err := starlark.UnpackArgs(b.Name(), args, kwargs,
				"to", &to, "amount", &amount, "key", &key,
				"order_id?", &orderID, "reason?", &reason, "commission?", &commission); err != nil {
				return nil, err
			}
			if key == "" {
				// Отказ здесь, а не в момент применения, чтобы ошибка всплыла в
				// собственных тестах скрипта: выплата без ключа повторялась бы при
				// каждой переотправке события.
				return nil, fmt.Errorf("pay_bonus requires a non-empty key")
			}
			return &effectValue{Effect{
				Kind: EffectPayBonus, UserID: to, Amount: amount, Key: key,
				OrderID: orderID, Reason: reason, Commission: commission,
			}}, nil
		}),
		"verify_user": starlark.NewBuiltin("verify_user", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var userID, orderID, reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "user_id", &userID, "order_id?", &orderID, "reason?", &reason); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectVerifyUser, UserID: userID, OrderID: orderID, Reason: reason}}, nil
		}),
		"system_message": starlark.NewBuiltin("system_message", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var orderID, text string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "order_id", &orderID, "text", &text); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectSystemMessage, OrderID: orderID, Text: text}}, nil
		}),
		"escalate": starlark.NewBuiltin("escalate", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var orderID, reason string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "order_id", &orderID, "reason?", &reason); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectEscalate, OrderID: orderID, Reason: reason}}, nil
		}),
		"has_role": starlark.NewBuiltin("has_role", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
		}),
	}
}
