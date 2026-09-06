package achievement

import (
	"fmt"
	"math"
	"sort"
	"time"

	"go.starlark.net/starlark"

	"healthlogin/backend/script"
)

// factsValue превращает Facts в аргумент `f`, который получает каждый хук.
// Структуры, а не словари, — чтобы опечатка в имени поля падала громко, а не
// возвращала молча None.
func factsValue(f Facts) starlark.Value {
	return script.Struct("facts", starlark.StringDict{
		"event":    starlark.String(f.Event),
		"config":   script.Dict(f.Config),
		"user":     actorValue(f.User),
		"customer": actorValue(f.Customer),
		"order":    orderValue(f.Order),
		"stats":    statsValue(f.Stats),
		"granted":  grantedValue(f.Granted),
		"now":      starlark.MakeInt64(f.Now.Unix()),
		// Календарный месяц строкой "2006-01". Starlark не умеет форматировать
		// время, а ачивке «за месяц» нужен именно календарный месяц, а не
		// тридцатидневное окно: без этого ключ выдачи пришлось бы считать
		// делением секунд, и он бы поехал относительно календаря.
		"month": starlark.String(f.Now.Format("2006-01")),
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
		"id":            starlark.String(a.ID),
		"role":          starlark.String(a.Role),
		"roles":         starlark.NewList(roles),
		"is_verified":   starlark.Bool(a.IsVerified),
		"status":        starlark.String(a.Status),
		"registered_at": timestamp(a.RegisteredAt),
		"points":        starlark.MakeInt(a.Points),
		"level":         starlark.MakeInt(a.Level),
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
		"id":           starlark.String(o.ID),
		"status":       starlark.String(o.Status),
		"customer_id":  starlark.String(o.CustomerID),
		"executor_id":  executor,
		"amount":       starlark.Float(o.Amount),
		"is_urgent":    starlark.Bool(o.IsUrgent),
		"is_asap":      starlark.Bool(o.IsAsap),
		"created_at":   timestamp(o.CreatedAt),
		"assigned_at":  timestamp(o.AssignedAt),
		"completed_at": timestamp(o.CompletedAt),
		"confirmed_at": timestamp(o.ConfirmedAt),
		"rating":       starlark.MakeInt(o.Rating),
	})
}

func statsValue(s *Stats) starlark.Value {
	if s == nil {
		s = &Stats{}
	}
	return script.Struct("stats", starlark.StringDict{
		"orders_completed":       starlark.MakeInt(s.OrdersCompleted),
		"orders_completed_month": starlark.MakeInt(s.OrdersCompletedMonth),
		"distinct_customers":     starlark.MakeInt(s.DistinctCustomers),
		"fastest_completion_min": starlark.MakeInt(s.FastestCompletionMin),
		"five_star_streak":       starlark.MakeInt(s.FiveStarStreak),
		"rating_count":           starlark.MakeInt(s.RatingCount),
		"cancels":                starlark.MakeInt(s.Cancels),
		"earned_total":           starlark.Float(s.EarnedTotal),
		"points_today":           starlark.MakeInt(s.PointsToday),
	})
}

// grantedValue отдаёт уже выданное словарём: скрипт пишет
// f.granted.get("marathon_month") и получает None, когда её не было.
func grantedValue(granted map[string]Granted) starlark.Value {
	d := starlark.NewDict(len(granted))
	codes := make([]string, 0, len(granted))
	for code := range granted {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		g := granted[code]
		_ = d.SetKey(starlark.String(code), script.Struct("granted", starlark.StringDict{
			"count":      starlark.MakeInt(g.Count),
			"points":     starlark.MakeInt(g.Points),
			"granted_at": timestamp(g.GrantedAt),
			"expires_at": timestamp(g.ExpiresAt),
		}))
	}
	return d
}

// timestamp отдаёт метку времени секундами, а нулевое время — None. Секунды, а
// не структура даты: скрипт с ними только сравнивает и вычитает, и разность в
// секундах читается рядом с minutes(20) без всякого справочника.
func timestamp(t time.Time) starlark.Value {
	if t.IsZero() {
		return starlark.None
	}
	return starlark.MakeInt64(t.Unix())
}

// effectValue — это Effect, пока он внутри скрипта: непрозрачный и без
// читаемых полей, так что скрипт может только создать его и вернуть.
type effectValue struct{ effect Effect }

func (e *effectValue) String() string        { return fmt.Sprintf("effect(%s)", e.effect.Kind) }
func (e *effectValue) Type() string          { return "effect" }
func (e *effectValue) Freeze()               {}
func (e *effectValue) Truth() starlark.Bool  { return starlark.True }
func (e *effectValue) Hash() (uint32, error) { return 0, fmt.Errorf("effect is unhashable") }

// grantValue — Grant внутри скрипта, такой же непрозрачный.
type grantValue struct{ grant Grant }

func (g *grantValue) String() string        { return "grant()" }
func (g *grantValue) Type() string          { return "grant" }
func (g *grantValue) Freeze()               {}
func (g *grantValue) Truth() starlark.Bool  { return starlark.True }
func (g *grantValue) Hash() (uint32, error) { return 0, fmt.Errorf("grant is unhashable") }

// wholeNumber приводит числовой аргумент к целому, принимая и int, и float.
// Отсутствующий аргумент — ноль: у него свой смысл («взять вес из настройки»),
// и отличать его от нуля явного не нужно.
func wholeNumber(builtin, name string, value starlark.Value) (int, error) {
	if value == nil {
		return 0, nil
	}
	if _, isNone := value.(starlark.NoneType); isNone {
		return 0, nil
	}
	number, ok := starlark.AsFloat(value)
	if !ok {
		return 0, fmt.Errorf("%s: %s is %s, want a number", builtin, name, value.Type())
	}
	return int(math.Round(number)), nil
}

// predeclared — окружение, в котором компилируется каждая ачивка: конструктор
// выдачи, два эффекта, помощники времени и проверка роли. Ни print, ни load, ни
// ввода-вывода — и никакого способа назначить комиссию.
func predeclared() starlark.StringDict {
	return starlark.StringDict{
		"grant": starlark.NewBuiltin("grant", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var key, orderID, reason string
			// Числа принимаются как есть и приводятся ниже, а не распаковкой
			// в int или float. Оба варианта встречаются в одном и том же поле:
			// скрипт пишет points = 25, а конфигурация ачивки приходит из
			// JSONB, где целых нет вовсе и f.config["weight"] — это float даже
			// когда админ ввёл «5». Требование одного из двух ломало бы скрипт
			// на самом обычном способе задать вес.
			var points, lifetimeDays starlark.Value
			var effects *starlark.List
			if err := starlark.UnpackArgs(b.Name(), args, kwargs,
				"key?", &key, "points?", &points, "lifetime_days?", &lifetimeDays,
				"order_id?", &orderID, "reason?", &reason, "effects?", &effects); err != nil {
				return nil, err
			}
			pointsValue, err := wholeNumber(b.Name(), "points", points)
			if err != nil {
				return nil, err
			}
			lifetimeValue, err := wholeNumber(b.Name(), "lifetime_days", lifetimeDays)
			if err != nil {
				return nil, err
			}
			g := Grant{
				Key: key, OrderID: orderID, Reason: reason,
				Points: pointsValue, LifetimeDays: lifetimeValue,
			}
			if effects != nil {
				for i := 0; i < effects.Len(); i++ {
					item, ok := effects.Index(i).(*effectValue)
					if !ok {
						return nil, fmt.Errorf("grant: effects[%d] is %s, want an effect", i, effects.Index(i).Type())
					}
					g.Effects = append(g.Effects, item.effect)
				}
			}
			return &grantValue{g}, nil
		}),
		"gift": starlark.NewBuiltin("gift", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var code string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "code", &code); err != nil {
				return nil, err
			}
			if code == "" {
				// Отказ здесь, а не при применении, чтобы ошибка всплыла в пробном
				// прогоне скрипта, а не в тишине диспетчера.
				return nil, fmt.Errorf("gift requires a non-empty code")
			}
			return &effectValue{Effect{Kind: EffectGift, GiftCode: code}}, nil
		}),
		"notify": starlark.NewBuiltin("notify", func(t *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			var subject, text string
			if err := starlark.UnpackArgs(b.Name(), args, kwargs, "text", &text, "subject?", &subject); err != nil {
				return nil, err
			}
			return &effectValue{Effect{Kind: EffectNotify, Subject: subject, Text: text}}, nil
		}),
		"minutes":  script.Duration("minutes", 60),
		"hours":    script.Duration("hours", 3600),
		"days":     script.Duration("days", 86400),
		"has_role": script.HasRole(),
	}
}
