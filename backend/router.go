package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"healthlogin/backend/handler"
	"healthlogin/backend/metrics"
	"healthlogin/backend/middleware"
	"healthlogin/backend/photoproof"
)

// appHandlers — все обработчики, которые роутер разводит по маршрутам. Каждый
// регистрирует свои маршруты сам, в одном стиле: Register<Группа>Routes(r) для
// открытых и аутентифицированных групп и Register<Группа>Routes(r, can) там,
// где маршрут охраняет право. Точные пути, методы и охрана остаются у
// обработчика, а роутер решает только, в какой группе middleware они живут.
type appHandlers struct {
	public               *handler.PublicHandler
	passport             *handler.PassportHandler
	admin                *handler.AdminHandler
	profile              *handler.ProfileHandler
	wallet               *handler.WalletHandler
	roles                *handler.RoleHandler
	orders               *handler.OrderHandler
	executorVerification *handler.ExecutorVerificationHandler
	shifts               *handler.ShiftHandler
	bids                 *handler.BidHandler
	chat                 *handler.ChatHandler
	shop                 *handler.ShopHandler
	geo                  *handler.GeoHandler
	serviceCatalog       *handler.ServiceCatalogHandler
	appReleases          *handler.AppReleaseHandler
	reviews              *handler.ReviewHandler
	executorGeo          *handler.ExecutorGeoHandler
	behavior             *handler.BehaviorHandler
	disputes             *handler.DisputeHandler
	penalties            *handler.PenaltyHandler
	photoProof           *photoproof.Handler
	mail                 *handler.MailHandler
	achievements         *handler.AchievementHandler
}

// limiters — ограничители частоты для эндпоинтов, которые есть смысл перебирать.
type limiters struct {
	login         *middleware.RateLimiter
	passwordReset *middleware.RateLimiter
	register      *middleware.RateLimiter
	geo           *middleware.RateLimiter
	// refresh ограничивается отдельно и щедрее входа: это не подбор учётных
	// данных, а обмен уже выданного токена, и ключ здесь — адрес клиента. За
	// NAT мобильного оператора под одним адресом сидят сотни приложений, и
	// общий с /login лимит отказывал бы им в обновлении, то есть выбрасывал бы
	// их из аккаунта.
	refresh *middleware.RateLimiter
	// shopPurchase: покупка идемпотентна по request_id, но каждая попытка
	// держит блокировку товара — частые повторы с одного адреса ограничиваются.
	shopPurchase *middleware.RateLimiter
}

// routerConfig — что роутеру нужно сверх обработчиков.
type routerConfig struct {
	auth *middleware.AuthMiddleware
	// allowsOrigin — доверенные источники браузера, общие с проверкой Origin у
	// WebSocket чата.
	allowsOrigin func(origin string) bool
	limits       limiters
	// releasesDir — откуда раздаются APK.
	releasesDir string
	// legacyRoot монтирует API ещё и в корне — для установленных APK,
	// появившихся раньше префикса /api.
	legacyRoot bool
	// maxBodyBytes — потолок тела запроса для всего роутера.
	maxBodyBytes int64
}

// newRouter собирает роутер. Все маршруты, кроме файловых, живут в
// registerAPIRoutes, который монтируется под /api/* и, пока включён
// LEGACY_ROOT_ROUTES, ещё и в корне. Оба монтирования несут одни и те же
// middleware аутентификации и авторизации.
func newRouter(h appHandlers, cfg routerConfig) chi.Router {
	r := chi.NewRouter()
	// StripQueryToken выполняется до логгера, чтобы учётные данные, переданные
	// параметром запроса, никогда не попадали в лог доступа.
	r.Use(middleware.StripQueryToken)
	r.Use(middleware.CORS(cfg.allowsOrigin))
	r.Use(middleware.SecurityHeaders)
	r.Use(chiMiddleware.Recoverer)
	// Внутри Recoverer, чтобы паника считалась той самой 500, которую клиент
	// действительно получил, а не пропадала из счётчиков запросов целиком.
	r.Use(metrics.Middleware)
	r.Use(chiMiddleware.Logger)
	r.Use(middleware.MaxBodyBytes(cfg.maxBodyBytes))

	// Основное монтирование: /api/* (веб через nginx + пересобранное мобильное приложение).
	r.Route("/api", func(r chi.Router) {
		registerAPIRoutes(r, h, cfg)
		registerFileRoutes(r, h, cfg)
	})
	if cfg.legacyRoot {
		registerAPIRoutes(r, h, cfg)
	}

	// APK релизов публичны по замыслу. Загруженные вложения чата — нет:
	// их отдаёт аутентифицированный обработчик, проверяющий, что вызывающий
	// участвует в переписке, которой принадлежит файл.
	r.Get("/releases/*", http.StripPrefix("/releases/", http.FileServer(http.Dir(cfg.releasesDir))).ServeHTTP)
	registerFileRoutes(r, h, cfg)
	return r
}

// registerFileRoutes — раздача загруженных файлов. Регистрируется и в корне, и
// под /api независимо от LEGACY_ROOT_ROUTES: nginx проксирует /uploads/, а
// ссылки в сообщениях и карточках товаров записаны без префикса.
func registerFileRoutes(r chi.Router, h appHandlers, cfg routerConfig) {
	// Изображения витрины публичны, в отличие от вложений чата: у них свой
	// маршрут, отдающий только файлы, которые сервер назвал сам.
	h.shop.RegisterFileRoutes(r)
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.RequireAuth)
		h.chat.RegisterFileRoutes(r)
	})
}

// registerAPIRoutes навешивает каждый обработчик на переданный chi.Router.
func registerAPIRoutes(r chi.Router, h appHandlers, cfg routerConfig) {
	lim := cfg.limits
	h.public.RegisterPublicRoutes(r, handler.AuthLimiters{
		Register:      lim.register.Middleware,
		Login:         lim.login.Middleware,
		Refresh:       lim.refresh.Middleware,
		PasswordReset: lim.passwordReset.Middleware,
	})
	h.geo.RegisterPublicRoutes(r, lim.geo.Middleware)
	h.profile.RegisterPublicRoutes(r)
	// OptionalAuth, чтобы каталог мог прятать услуги «только для верифицированных»
	// от неверифицированных заказчиков, оставаясь доступным анонимным посетителям.
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.OptionalAuth)
		h.serviceCatalog.RegisterPublicRoutes(r)
	})
	h.appReleases.RegisterPublicRoutes(r)
	h.reviews.RegisterPublicRoutes(r)

	// Аутентифицированные маршруты заказчика. ADMIN включён, чтобы поддержка могла
	// действовать от имени заказчика из админ-панели.
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.RequireAuth)
		r.Use(middleware.RequireRole("CUSTOMER", "ADMIN"))
		h.orders.RegisterCustomerRoutes(r)
		h.bids.RegisterCustomerRoutes(r)
		h.disputes.RegisterCustomerRoutes(r)
	})

	// Аутентифицированные общие маршруты (заказчик + исполнитель + админ).
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.RequireAuth)
		r.Use(middleware.RequireRole("CUSTOMER", "EXECUTOR", "ADMIN"))
		h.public.RegisterUserRoutes(r, lim.passwordReset.Middleware)
		h.penalties.RegisterUserRoutes(r)
		h.profile.RegisterUserRoutes(r)
		h.wallet.RegisterUserRoutes(r)
		h.chat.RegisterUserRoutes(r)
		// Внутренняя почта: сюда приходят выданные ачивки, купоны на
		// подарки, акции и новости. Она есть у всех ролей, потому что
		// новость адресуется человеку, а не его роли в заказе.
		h.mail.RegisterUserRoutes(r)
		h.reviews.RegisterUserRoutes(r)
		// Магазин: витрина и покупка открыты любой роли — какие товары
		// кому видны, решают роли на самом товаре.
		h.shop.RegisterUserRoutes(r, lim.shopPurchase.Middleware)
		h.passport.RegisterUserRoutes(r)
		h.achievements.RegisterUserRoutes(r)
	})

	// Аутентифицированные маршруты исполнителя.
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.RequireAuth)
		r.Use(middleware.RequireRole("EXECUTOR", "MODERATOR", "ADMIN"))
		h.shifts.RegisterExecutorRoutes(r)
		h.executorGeo.RegisterExecutorRoutes(r)
		h.orders.RegisterExecutorRoutes(r)
		h.bids.RegisterExecutorRoutes(r)
		h.disputes.RegisterExecutorRoutes(r)
		h.photoProof.RegisterExecutorRoutes(r)
		h.behavior.RegisterExecutorRoutes(r)
		h.executorVerification.RegisterExecutorRoutes(r)
		h.achievements.RegisterExecutorRoutes(r)
	})

	// Аутентифицированные маршруты админа.
	//
	// Группу открывает не роль ADMIN, а наличие хоть одного права в разделах
	// панели: роль «финансист» с одной галочкой «сверка» обязана дойти до
	// своей страницы. Что именно ей там можно, решает право на каждом
	// маршруте — раздел плюс действие, из каталога service/permission.go.
	// Администратор проходит любую из этих проверок: он суперпользователь, и
	// снятая где-то галочка не должна уметь запереть его снаружи панели.
	r.Group(func(r chi.Router) {
		r.Use(cfg.auth.RequireAuth)
		r.Use(cfg.auth.RequireAdminPanel)
		can := cfg.auth.RequirePermission

		h.executorGeo.RegisterAdminRoutes(r, can)
		h.admin.RegisterAdminRoutes(r, can)
		h.roles.RegisterAdminRoutes(r, can)
		h.chat.RegisterAdminRoutes(r, can)
		h.orders.RegisterAdminRoutes(r, can)
		h.behavior.RegisterAdminRoutes(r, can)
		h.penalties.RegisterAdminRoutes(r, can)
		h.disputes.RegisterAdminRoutes(r, can)
		h.serviceCatalog.RegisterAdminRoutes(r, can)
		h.appReleases.RegisterAdminRoutes(r, can)
		h.achievements.RegisterAdminRoutes(r, can)
		h.mail.RegisterAdminRoutes(r, can)
		h.photoProof.RegisterAdminRoutes(r, can)
		h.shop.RegisterAdminRoutes(r, can)
		h.passport.RegisterAdminRoutes(r, can)
	})
}
