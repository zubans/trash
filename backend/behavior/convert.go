package behavior

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"

	"healthlogin/backend/script"
)

// factsValue превращает Facts в аргумент `f`, который получает каждый хук.
// Используются структуры, а не словари, чтобы скрипт писал f.user.is_verified,
// а опечатка в имени поля падала громко, а не возвращала молча None.
func factsValue(f Facts) starlark.Value {
	d := starlark.StringDict{
		"event":      starlark.String(f.Event),
		"config":     script.Dict(f.Config),
		"user":       actorValue(f.User),
		"viewer":     actorValue(f.Viewer),
		"customer":   actorValue(f.Customer),
		"order":      orderValue(f.Order),
		"variant":    variantValue(f.Variant),
		"claims":     starlark.MakeInt(f.Claims),
		"submission": submissionValue(f.Submission),
		"now":        starlark.MakeInt64(f.Now.Unix()),
	}
	return script.Struct("facts", d)
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
	return script.Struct("submission", starlark.StringDict{
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
	return script.Struct("actor", starlark.StringDict{
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
	return script.Struct("order", starlark.StringDict{
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
	return script.Struct("variant", starlark.StringDict{
		"id":         starlark.String(v.ID),
		"code":       starlark.String(v.Code),
		"base_price": starlark.Float(v.BasePrice),
	})
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
		"has_role": script.HasRole(),
	}
}
