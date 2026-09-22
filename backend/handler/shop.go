package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// maxShopImageBytes — потолок одного изображения товара.
const maxShopImageBytes = 10 << 20

// shopImageName — имя, которое сервер сам дал изображению. Всё прочее под
// /uploads/shop/ не отдаётся: так путь не превращается в чтение произвольного
// файла.
var shopImageName = regexp.MustCompile(`^[0-9a-f-]{36}\.(jpg|png|webp|gif)$`)

var shopImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// ShopHandler обслуживает магазин: витрину и покупку для покупателя, каталог,
// обработку покупок и выручку для админки.
type ShopHandler struct {
	shop *service.ShopService
}

// NewShopHandler создаёт ShopHandler.
func NewShopHandler(shop *service.ShopService) *ShopHandler {
	return &ShopHandler{shop: shop}
}

// RegisterUserRoutes подключает маршруты покупателя. purchase — ограничитель
// частоты на саму покупку.
//
// Эндпоинта отмены у покупателя нет: возврат оформляется только через чат
// поддержки (implementation_plan_shop.md §4.4).
func (h *ShopHandler) RegisterUserRoutes(r chi.Router, purchase func(http.Handler) http.Handler) {
	r.Get("/shop/products", h.Storefront)
	r.Get("/shop/products/{id}", h.Product)
	r.Get("/shop/pickup-points", h.PickupPoints)
	r.With(purchase).Post("/shop/orders", h.Purchase)
	r.Get("/shop/orders", h.MyOrders)
	r.Get("/shop/orders/{id}", h.MyOrder)
	r.Get("/me/perks", h.MyPerks)
}

// RegisterAdminRoutes подключает админку магазина. Три раздела прав, а не
// один: заводить товары, обрабатывать заказы и выводить деньги — разные люди.
func (h *ShopHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("shop.view")).Get("/admin/shop/products", h.AdminProducts)
	r.With(can("shop.view")).Get("/admin/shop/products/{id}", h.AdminProduct)
	r.With(can("shop.create")).Post("/admin/shop/products", h.AdminCreateProduct)
	r.With(can("shop.edit")).Put("/admin/shop/products/{id}", h.AdminUpdateProduct)
	r.With(can("shop.edit")).Post("/admin/shop/images", h.AdminUploadImage)
	r.With(can("shop.view")).Get("/admin/shop/pickup-points", h.AdminPickupPoints)
	r.With(can("shop.edit")).Post("/admin/shop/pickup-points", h.AdminCreatePickupPoint)
	r.With(can("shop.edit")).Put("/admin/shop/pickup-points/{id}", h.AdminUpdatePickupPoint)

	r.With(can("shop_orders.view")).Get("/admin/shop/orders", h.AdminOrders)
	r.With(can("shop_orders.view")).Get("/admin/shop/orders/count", h.AdminOrdersCount)
	r.With(can("shop_orders.view")).Get("/admin/shop/orders/{id}", h.AdminOrder)
	r.With(can("shop_orders.view")).Get("/admin/shop/orders/{id}/refund-quote", h.AdminRefundQuote)
	r.With(can("shop_orders.edit")).Post("/admin/shop/orders/{id}/status", h.AdminSetStatus)
	r.With(can("shop_orders.edit")).Post("/admin/shop/orders/{id}/cancel", h.AdminCancel)
	r.With(can("shop_orders.view")).Get("/admin/users/{id}/shop", h.AdminUserShop)
	r.With(can("shop_orders.edit")).Post("/admin/users/{id}/perks", h.AdminGrantPerk)
	r.With(can("shop_orders.edit")).Delete("/admin/perks/{id}", h.AdminRevokePerk)

	r.With(can("shop_revenue.view")).Get("/admin/finances/shop", h.AdminRevenue)
	r.With(can("shop_revenue.edit")).Post("/admin/finances/shop/payout", h.AdminPayout)
}

// writeShopError отвечает кодом отказа, который клиент переводит, а всё
// прочее — 500 без подробностей.
func writeShopError(w http.ResponseWriter, err error) {
	var shopErr *service.ShopError
	if errors.As(err, &shopErr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(shopErr.Status)
		_ = json.NewEncoder(w).Encode(shopErr)
		return
	}
	log.Printf("[shop] %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (h *ShopHandler) caller(w http.ResponseWriter, r *http.Request) *repository.User {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
	return user
}

func (h *ShopHandler) idParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

// Storefront обслуживает GET /shop/products.
func (h *ShopHandler) Storefront(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	out, err := h.shop.Storefront(r.Context(), user, r.URL.Query().Get("category"))
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, out)
}

// Product обслуживает GET /shop/products/{id}.
func (h *ShopHandler) Product(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	card, err := h.shop.Product(r.Context(), user, id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, card)
}

// PickupPoints обслуживает GET /shop/pickup-points.
func (h *ShopHandler) PickupPoints(w http.ResponseWriter, r *http.Request) {
	points, err := h.shop.PickupPoints(r.Context())
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, points)
}

// Purchase обслуживает POST /shop/orders.
func (h *ShopHandler) Purchase(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	var req service.PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	order, err := h.shop.Purchase(r.Context(), user, req)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, order)
}

// MyOrders обслуживает GET /shop/orders.
func (h *ShopHandler) MyOrders(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	orders, err := h.shop.MyOrders(r.Context(), user)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, orders)
}

// MyOrder обслуживает GET /shop/orders/{id}.
func (h *ShopHandler) MyOrder(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	order, err := h.shop.MyOrder(r.Context(), user, id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, order)
}

// MyPerks обслуживает GET /me/perks.
func (h *ShopHandler) MyPerks(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	perks, err := h.shop.MyPerks(r.Context(), user)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, perks)
}

// ServeImage обслуживает GET /uploads/shop/{name}.
//
// Общий /uploads/* отдаёт только вложения переписки её участникам и всегда как
// файл на скачивание. Изображение витрины не вложение: оно публично и
// показывается картинкой, поэтому у него свой маршрут без аутентификации — но
// только для имён, которые сервер выдал сам.
func (h *ShopHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !shopImageName.MatchString(name) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	full := filepath.Join(uploadsBaseDir(), "shop", name)
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	contentType := "image/jpeg"
	switch filepath.Ext(name) {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	case ".gif":
		contentType = "image/gif"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Имя уникально и файл не переписывается — кэшировать можно долго.
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	http.ServeFile(w, r, full)
}

// AdminUploadImage обслуживает POST /admin/shop/images: только изображения,
// имя файла даёт сервер.
func (h *ShopHandler) AdminUploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxShopImageBytes+(64<<10))
	if err := r.ParseMultipartForm(maxShopImageBytes); err != nil {
		writeShopError(w, &service.ShopError{Status: http.StatusBadRequest, Code: service.ShopErrValidation,
			Message: "Файл больше 10 МБ", Fields: map[string]string{"images": "Файл больше 10 МБ"}})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Тип определяется по содержимому, а не по имени и заголовку клиента:
	// «картинка.jpg» с HTML внутри не должна лечь рядом с изображениями.
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	ext, ok := shopImageTypes[http.DetectContentType(head[:n])]
	if !ok {
		writeShopError(w, &service.ShopError{Status: http.StatusBadRequest, Code: service.ShopErrValidation,
			Message: "Только JPEG, PNG, WebP или GIF", Fields: map[string]string{"images": "Только JPEG, PNG, WebP или GIF"}})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "cannot read file", http.StatusBadRequest)
		return
	}

	dir := filepath.Join(uploadsBaseDir(), "shop")
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeShopError(w, err)
		return
	}
	name := uuid.New().String() + ext
	if err := saveShopImage(filepath.Join(dir, name), file); err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, map[string]string{"url": service.ShopImagePrefix + name})
}

// saveShopImage пишет файл целиком или не оставляет ничего: недописанный файл
// удаляется, иначе на витрину попала бы обрезанная картинка.
func saveShopImage(path string, src io.Reader) error {
	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(path)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

// AdminProducts обслуживает GET /admin/shop/products.
func (h *ShopHandler) AdminProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.shop.AdminProducts(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("category"))
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, products)
}

// AdminProduct обслуживает GET /admin/shop/products/{id}.
func (h *ShopHandler) AdminProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	product, err := h.shop.AdminProduct(r.Context(), id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, product)
}

func (h *ShopHandler) saveProduct(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	var product repository.ShopProduct
	if err := json.NewDecoder(r.Body).Decode(&product); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	product.ID = id
	saved, err := h.shop.SaveProduct(r.Context(), admin.ID, &product)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, saved)
}

// AdminCreateProduct обслуживает POST /admin/shop/products.
func (h *ShopHandler) AdminCreateProduct(w http.ResponseWriter, r *http.Request) {
	h.saveProduct(w, r, uuid.Nil)
}

// AdminUpdateProduct обслуживает PUT /admin/shop/products/{id}.
func (h *ShopHandler) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	h.saveProduct(w, r, id)
}

// AdminPickupPoints обслуживает GET /admin/shop/pickup-points.
func (h *ShopHandler) AdminPickupPoints(w http.ResponseWriter, r *http.Request) {
	points, err := h.shop.AdminPickupPoints(r.Context())
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, points)
}

func (h *ShopHandler) savePickupPoint(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	var point repository.ShopPickupPoint
	if err := json.NewDecoder(r.Body).Decode(&point); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	point.ID = id
	saved, err := h.shop.SavePickupPoint(r.Context(), admin.ID, &point)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, saved)
}

// AdminCreatePickupPoint обслуживает POST /admin/shop/pickup-points.
func (h *ShopHandler) AdminCreatePickupPoint(w http.ResponseWriter, r *http.Request) {
	h.savePickupPoint(w, r, uuid.Nil)
}

// AdminUpdatePickupPoint обслуживает PUT /admin/shop/pickup-points/{id}.
func (h *ShopHandler) AdminUpdatePickupPoint(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	h.savePickupPoint(w, r, id)
}

// AdminOrders обслуживает GET /admin/shop/orders: ?status=&product_id=&from=&to=&q=&refund=1&limit=&offset=.
func (h *ShopHandler) AdminOrders(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, offset := pageParams(r)
	filter := repository.ShopOrderFilter{
		Status: query.Get("status"), Search: query.Get("q"),
		RefundRequested: query.Get("refund") == "1", Limit: limit, Offset: offset,
	}
	if id, err := uuid.Parse(query.Get("product_id")); err == nil {
		filter.ProductID = &id
	}
	if from, ok := parseDay(query.Get("from")); ok {
		filter.From = &from
	}
	if to, ok := parseDay(query.Get("to")); ok {
		// Конец периода включительно: «по 20 сентября» — это весь день.
		end := to.AddDate(0, 0, 1)
		filter.To = &end
	}
	orders, total, err := h.shop.AdminOrders(r.Context(), filter)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"orders": orders, "total": total})
}

func parseDay(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	return t, err == nil
}

// AdminOrdersCount обслуживает GET /admin/shop/orders/count — бейдж меню.
func (h *ShopHandler) AdminOrdersCount(w http.ResponseWriter, r *http.Request) {
	paid, err := h.shop.CountPaid(r.Context())
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, map[string]int{"paid": paid})
}

// AdminOrder обслуживает GET /admin/shop/orders/{id}.
func (h *ShopHandler) AdminOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	order, err := h.shop.AdminOrder(r.Context(), id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, order)
}

// AdminRefundQuote обслуживает GET /admin/shop/orders/{id}/refund-quote.
func (h *ShopHandler) AdminRefundQuote(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	quote, err := h.shop.RefundQuote(r.Context(), id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, quote)
}

// AdminSetStatus обслуживает POST /admin/shop/orders/{id}/status.
func (h *ShopHandler) AdminSetStatus(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Track  string `json:"track"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	order, err := h.shop.SetStatus(r.Context(), admin.ID, id, strings.ToUpper(req.Status), req.Track)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, order)
}

// AdminCancel обслуживает POST /admin/shop/orders/{id}/cancel.
func (h *ShopHandler) AdminCancel(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var req service.CancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	order, err := h.shop.Cancel(r.Context(), admin.ID, id, req)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, order)
}

// AdminUserShop обслуживает GET /admin/users/{id}/shop — покупки и привилегии
// на карточке пользователя.
func (h *ShopHandler) AdminUserShop(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	history, err := h.shop.UserHistory(r.Context(), id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, history)
}

// AdminGrantPerk обслуживает POST /admin/users/{id}/perks.
func (h *ShopHandler) AdminGrantPerk(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var req service.GrantPerkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	perk, err := h.shop.GrantPerk(r.Context(), admin.ID, id, req)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, perk)
}

// AdminRevokePerk обслуживает DELETE /admin/perks/{id}.
func (h *ShopHandler) AdminRevokePerk(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	perk, err := h.shop.RevokePerk(r.Context(), admin.ID, id)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, perk)
}

// AdminRevenue обслуживает GET /admin/finances/shop?from=&to=. По умолчанию —
// последние 30 дней.
func (h *ShopHandler) AdminRevenue(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	to := now
	from := now.AddDate(0, 0, -30)
	if day, ok := parseDay(r.URL.Query().Get("from")); ok {
		from = day
	}
	if day, ok := parseDay(r.URL.Query().Get("to")); ok {
		to = day.AddDate(0, 0, 1)
	}
	revenue, err := h.shop.Revenue(r.Context(), from, to)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, revenue)
}

// AdminPayout обслуживает POST /admin/finances/shop/payout.
func (h *ShopHandler) AdminPayout(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	balance, err := h.shop.Payout(r.Context(), admin.ID, req.Amount)
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, map[string]money.Amount{"balance": balance})
}
