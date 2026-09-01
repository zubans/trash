package behavior

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// factsValue renders Facts as the `f` argument every hook receives. Structs are
// used rather than dicts so a script reads f.user.is_verified and a typo in a
// field name fails loudly instead of silently yielding None.
func factsValue(f Facts) starlark.Value {
	d := starlark.StringDict{
		"event":    starlark.String(f.Event),
		"config":   configValue(f.Config),
		"user":     actorValue(f.User),
		"viewer":   actorValue(f.Viewer),
		"customer": actorValue(f.Customer),
		"order":    orderValue(f.Order),
		"variant":  variantValue(f.Variant),
		"claims":   starlark.MakeInt(f.Claims),
		"now":      starlark.MakeInt64(f.Now.Unix()),
	}
	return starlarkstruct.FromStringDict(starlark.String("facts"), d)
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

// configValue renders the node's configuration as a dict, so a script can use
// f.config.get("reward", 0) and keep working when a key was never set.
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

// goToStarlark converts a value decoded from JSON. Anything it does not
// recognise becomes its Go rendering as a string rather than an error: a
// configuration key the script does not use must not break a hook.
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

// starlarkToGo converts back, for the manifest read at load time.
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

// effectValue is an Effect while it is inside a script: opaque, unhashable and
// with no fields to read, so a script can only build one and hand it back.
type effectValue struct{ effect Effect }

func (e *effectValue) String() string {
	return fmt.Sprintf("effect(%s)", e.effect.Kind)
}
func (e *effectValue) Type() string          { return "effect" }
func (e *effectValue) Freeze()               {}
func (e *effectValue) Truth() starlark.Bool  { return starlark.True }
func (e *effectValue) Hash() (uint32, error) { return 0, fmt.Errorf("effect is unhashable") }

// predeclared is the environment every script is compiled against. It contains
// the effect constructors and nothing else — no print, no load, no I/O.
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
			// Commission is opt-in: a reward is not something a customer paid
			// for, so nothing is withheld from it unless the behaviour says so.
			commission := false
			if err := starlark.UnpackArgs(b.Name(), args, kwargs,
				"to", &to, "amount", &amount, "key", &key,
				"order_id?", &orderID, "reason?", &reason, "commission?", &commission); err != nil {
				return nil, err
			}
			if key == "" {
				// Refused here rather than at apply time so the mistake shows up
				// in the script's own tests: a payment with no key would be made
				// again on every redelivery of the event.
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
