package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Behaviors is the core's side of a behaviour script: it turns database rows
// into the facts a script is allowed to see, calls the hook, and turns the
// answer back into the errors the rest of the service layer already speaks.
//
// Every method is nil-safe. A deployment with no scripts, and every test that
// does not care about them, passes nil and gets exactly the behaviour the
// service had before behaviours existed.
type Behaviors struct {
	engine  *behavior.Engine
	claims  repository.ServiceClaimRepository
	catalog repository.ServiceCatalogRepository
}

// NewBehaviors wires the engine to the claim store. claims may be nil, in which
// case once-per-user services cannot be enforced and are refused rather than
// silently allowed twice.
func NewBehaviors(engine *behavior.Engine, claims repository.ServiceClaimRepository) *Behaviors {
	if engine == nil {
		return nil
	}
	return &Behaviors{engine: engine, claims: claims}
}

// WithCatalog lets the behaviours compile the scripts stored on catalog nodes —
// the special services written in the admin panel.
func (b *Behaviors) WithCatalog(catalog repository.ServiceCatalogRepository) *Behaviors {
	if b != nil {
		b.catalog = catalog
	}
	return b
}

// codeFor is the behaviour a node actually runs. A node with a script of its own
// runs that script, registered under its id; otherwise it runs the library
// behaviour it names. Everything below goes through this, so "which script"
// is decided in one place.
func (b *Behaviors) codeFor(node *repository.ServiceNode) string {
	if node == nil {
		return ""
	}
	if node.HasOwnScript() {
		return behavior.NodeCode(node.ID.String())
	}
	return node.BehaviorCode
}

// nodeSources renders a node's own script as the two files the engine compiles.
func nodeSources(node *repository.ServiceNode) []behavior.SourceFile {
	files := make([]behavior.SourceFile, 0, 2)
	if strings.TrimSpace(node.BehaviorConstants) != "" {
		files = append(files, behavior.SourceFile{Name: behavior.ConfigFile, Src: []byte(node.BehaviorConstants)})
	}
	return append(files, behavior.SourceFile{Name: "behavior.star", Src: []byte(node.BehaviorSource)})
}

// Validate compiles a candidate script without registering it. The admin panel
// calls it before saving: a script that does not compile would take the service
// off sale silently, so it is refused while somebody is still looking at it.
func (b *Behaviors) Validate(node *repository.ServiceNode) error {
	if b == nil || node == nil || !node.HasOwnScript() {
		return nil
	}
	return b.engine.Validate(nodeSources(node))
}

// SyncNode compiles the node's own script, or unregisters it when the script was
// removed. Called right after an admin saves, so the edit applies to the next
// request on this process.
func (b *Behaviors) SyncNode(node *repository.ServiceNode) error {
	if b == nil || node == nil {
		return nil
	}
	code := behavior.NodeCode(node.ID.String())
	if !node.HasOwnScript() || node.IsDeleted() {
		b.engine.Remove(code)
		return nil
	}
	return b.engine.CompileFiles(code, nodeSources(node))
}

// RemoveNode unregisters a node's script, for the paths that hold only its id —
// a retired node stops being special immediately, not at the next resync.
func (b *Behaviors) RemoveNode(nodeID uuid.UUID) {
	if b == nil {
		return
	}
	b.engine.Remove(behavior.NodeCode(nodeID.String()))
}

// SyncAll compiles every node script in the catalog and drops the ones that are
// gone. It runs at startup and on a timer: another replica's edit, or a change
// made directly in the database, has to reach this process too.
func (b *Behaviors) SyncAll(ctx context.Context) error {
	if b == nil || b.catalog == nil {
		return nil
	}
	nodes, err := b.catalog.ListNodesWithScript(ctx)
	if err != nil {
		return err
	}

	live := make(map[string]struct{}, len(nodes))
	var failed []string
	for _, node := range nodes {
		code := behavior.NodeCode(node.ID.String())
		live[code] = struct{}{}
		if err := b.engine.CompileFiles(code, nodeSources(node)); err != nil {
			// Reported, not fatal, and deliberately not registered: the node
			// falls back to "unknown behaviour", which fails its gates closed.
			b.engine.Remove(code)
			failed = append(failed, node.Code)
			log.Printf("[behavior] node %s (%s): %v", node.Code, node.ID, err)
		}
	}
	for _, m := range b.engine.Manifests() {
		if !behavior.IsNodeCode(m.Code) {
			continue
		}
		if _, ok := live[m.Code]; !ok {
			b.engine.Remove(m.Code)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("node scripts failed to compile: %s", strings.Join(failed, ", "))
	}
	return nil
}

// ErrBehaviorUnavailable is what a caller gets when the script that governs a
// service cannot be run — it failed to compile, it timed out, it returned
// nonsense. Every gate fails closed on it: a service whose rules cannot be
// evaluated is a service nobody may order or take.
var ErrBehaviorUnavailable = errors.New("услуга временно недоступна")

// Engine exposes the compiled behaviours, for the admin endpoint that lists
// them. Nothing else needs it.
func (b *Behaviors) Engine() *behavior.Engine {
	if b == nil {
		return nil
	}
	return b.engine
}

// governs reports whether this node's rules come from a script this engine has.
func (b *Behaviors) governs(node *repository.ServiceNode) bool {
	return b != nil && node != nil && node.HasBehavior()
}

// Governs reports whether this node's rules come from a script at all. Callers
// use it to skip work that only a scripted service needs — an ordinary service
// must not pay for a mechanism it does not use.
func (b *Behaviors) Governs(node *repository.ServiceNode) bool {
	return b.governs(node)
}

// Manifest returns the static declaration behind a node's behaviour.
func (b *Behaviors) Manifest(node *repository.ServiceNode) (behavior.Manifest, bool) {
	if !b.governs(node) {
		return behavior.Manifest{}, false
	}
	return b.engine.Manifest(b.codeFor(node))
}

// Config returns the node's configuration laid over the behaviour's own
// constants — the same merge the script sees through f.config.
//
// The core needs it wherever it checks something the script also reads: the
// verifier role, for instance, is a constant in config.star, and a guard that
// looked only at the node's column would refuse what the script allows the
// moment that constant changes.
func (b *Behaviors) Config(node *repository.ServiceNode) map[string]interface{} {
	if !b.governs(node) {
		return nil
	}
	merged := map[string]interface{}{}
	if m, ok := b.engine.Manifest(b.codeFor(node)); ok {
		for k, v := range m.Defaults {
			merged[k] = v
		}
	}
	for k, v := range node.BehaviorConfig {
		merged[k] = v
	}
	return merged
}

// ConfigString reads one string setting out of that merge.
func (b *Behaviors) ConfigString(node *repository.ServiceNode, key, fallback string) string {
	if value, ok := b.Config(node)[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

// OncePerUser reports whether ordering this node has to claim it for the user.
func (b *Behaviors) OncePerUser(node *repository.ServiceNode) bool {
	m, ok := b.Manifest(node)
	return ok && m.OncePerUser
}

// ReleasesClaimOnCancel reports whether cancelling an order for this node gives
// the user their one attempt back.
func (b *Behaviors) ReleasesClaimOnCancel(node *repository.ServiceNode) bool {
	m, ok := b.Manifest(node)
	return ok && m.OncePerUser && m.ReleaseClaimOnCancel
}

// Visible decides whether a catalog node may be listed for this viewer. It is
// given the viewer's claim counts because the listing judges many nodes at once
// and must not query per node.
func (b *Behaviors) Visible(ctx context.Context, viewer *repository.User, node *repository.ServiceNode, claims map[uuid.UUID]int) bool {
	if !b.governs(node) {
		return true
	}
	facts := behavior.Facts{
		User:    actorFacts(viewer),
		Variant: variantFacts(node),
		Config:  node.BehaviorConfig,
		Claims:  claims[node.ID],
	}
	visible, err := b.engine.Visible(b.codeFor(node), facts)
	if err != nil {
		// Hidden, not shown: a service whose visibility rule cannot be run is
		// one nobody can order either (CanOrder fails the same way), and
		// listing it would only produce a refusal at checkout.
		b.report(node, behavior.HookVisible, err)
		return false
	}
	return visible
}

// CanOrder is the scripted half of canCustomerOrderVariant.
func (b *Behaviors) CanOrder(ctx context.Context, customer *repository.User, variant *repository.ServiceNode) error {
	if !b.governs(variant) {
		return nil
	}
	claims, err := b.claimCount(ctx, customer, variant)
	if err != nil {
		return err
	}
	facts := behavior.Facts{
		User:    actorFacts(customer),
		Variant: variantFacts(variant),
		Config:  variant.BehaviorConfig,
		Claims:  claims,
	}
	return b.translate(variant, behavior.HookCanOrder, b.engine.CanOrder(b.codeFor(variant), facts))
}

// CanViewOrTake is the scripted half of canViewOrTakeOrder.
func (b *Behaviors) CanViewOrTake(ctx context.Context, viewer, customer *repository.User, variant *repository.ServiceNode) error {
	if !b.governs(variant) {
		return nil
	}
	facts := behavior.Facts{
		Viewer:   actorFacts(viewer),
		Customer: actorFacts(customer),
		User:     actorFacts(customer),
		Variant:  variantFacts(variant),
		Config:   variant.BehaviorConfig,
	}
	return b.translate(variant, behavior.HookCanViewOrTake, b.engine.CanViewOrTake(b.codeFor(variant), facts))
}

// Price returns the price a behaviour dictates, if it dictates one. A script
// that prices a service overrides the catalog completely, tariff coefficients
// included: "free" has to mean free even for an ASAP order.
func (b *Behaviors) Price(ctx context.Context, variant *repository.ServiceNode) (money.Amount, bool, error) {
	if !b.governs(variant) {
		return money.Zero, false, nil
	}
	facts := behavior.Facts{
		Variant: variantFacts(variant),
		Config:  variant.BehaviorConfig,
	}
	rubles, ok, err := b.engine.Price(b.codeFor(variant), facts)
	if err != nil {
		b.report(variant, behavior.HookPrice, err)
		return money.Zero, false, ErrBehaviorUnavailable
	}
	if !ok {
		return money.Zero, false, nil
	}
	return money.FromRubles(rubles), true, nil
}

// claimCount answers "how many times has this user ordered this variant", and
// only for the behaviours that care. A behaviour that is once-per-user without
// a claim store is refused: allowing it would mean the limit silently does not
// hold.
func (b *Behaviors) claimCount(ctx context.Context, user *repository.User, variant *repository.ServiceNode) (int, error) {
	if user == nil || !b.OncePerUser(variant) {
		return 0, nil
	}
	if b.claims == nil {
		return 0, ErrBehaviorUnavailable
	}
	count, err := b.claims.CountForVariant(ctx, user.ID, variant.ID)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ClaimsFor loads a user's claims in one query, for the catalog listings.
func (b *Behaviors) ClaimsFor(ctx context.Context, user *repository.User) map[uuid.UUID]int {
	if b == nil || b.claims == nil || user == nil {
		return nil
	}
	counts, err := b.claims.CountsForUser(ctx, user.ID)
	if err != nil {
		log.Printf("[behavior] cannot read service claims of %s: %v", user.ID, err)
		return nil
	}
	return counts
}

// translate turns a script's answer into the service layer's error vocabulary:
// a refusal keeps its message, anything else becomes ErrBehaviorUnavailable and
// is reported.
func (b *Behaviors) translate(node *repository.ServiceNode, hook string, err error) error {
	if err == nil {
		return nil
	}
	var denied *behavior.DeniedError
	if errors.As(err, &denied) {
		return errors.New(denied.Message)
	}
	b.report(node, hook, err)
	return ErrBehaviorUnavailable
}

func (b *Behaviors) report(node *repository.ServiceNode, hook string, err error) {
	code := b.codeFor(node)
	log.Printf("[behavior] %s.%s on node %s: %v", code, hook, node.Code, err)
	metrics.BehaviorHookError(code, hook)
}

// Code is the behaviour a node runs, for the callers outside this file that
// need to name it — the dispatcher, when it records what asked for an effect.
func (b *Behaviors) Code(node *repository.ServiceNode) string {
	if b == nil {
		return ""
	}
	return b.codeFor(node)
}

// actorFacts renders a user for a script. Nil stays nil: "no user" is a case
// scripts have to handle (an anonymous catalog visitor), not an error.
func actorFacts(u *repository.User) *behavior.Actor {
	if u == nil {
		return nil
	}
	return &behavior.Actor{
		ID:         u.ID.String(),
		Role:       u.Role,
		Roles:      u.Roles,
		IsVerified: u.IsVerified(),
		Age:        u.GetAge(),
		Status:     u.Status,
	}
}

func variantFacts(n *repository.ServiceNode) *behavior.VariantFacts {
	if n == nil {
		return nil
	}
	v := &behavior.VariantFacts{ID: n.ID.String(), Code: n.Code}
	if n.BasePrice != nil {
		v.BasePrice = n.BasePrice.Rubles()
	}
	return v
}

func orderFacts(o *repository.Order) *behavior.OrderFacts {
	if o == nil {
		return nil
	}
	f := &behavior.OrderFacts{
		ID:         o.ID.String(),
		Status:     string(o.Status),
		CustomerID: o.CustomerID.String(),
		Amount:     o.HoldAmount.Rubles(),
		IsUrgent:   o.IsUrgent,
		IsAsap:     o.IsAsap,
	}
	if o.ExecutorID != nil {
		f.ExecutorID = o.ExecutorID.String()
	}
	return f
}
