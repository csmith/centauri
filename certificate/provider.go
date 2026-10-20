package certificate

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ProviderConfig contains everything needed to assemble a certificate provider.
type ProviderConfig struct {
	// Store is where certificates are persisted.
	Store Store
	// Lego, if set, is used to create an ACME-backed supplier. NewProvider waits briefly for the supplier's
	// first attempt to connect to the ACME server; if that doesn't succeed, it keeps retrying with backoff in
	// the background. Until then, previously obtained certificates are still served, but new ones cannot be
	// obtained.
	Lego *LegoSupplierConfig
	// OnLegoConnected, if set, is called if the lego supplier connects to the ACME server after NewProvider
	// has returned.
	OnLegoConnected func()
	// PreferredSuppliers lists the names of the suppliers to use, in order of preference.
	PreferredSuppliers []string
	// WildcardDomains lists domains for which a single wildcard certificate should be requested.
	WildcardDomains []string
	// UseStaples enables requesting OCSP staples from suppliers.
	UseStaples bool
}

// NewProvider assembles a certificate provider from the given configuration: a certificate Manager backed
// by the configured store and suppliers, wrapped in a WildcardResolver. The context controls the lifetime
// of any background connection attempts.
func NewProvider(ctx context.Context, config ProviderConfig) (*WildcardResolver, error) {
	suppliers := make(map[string]Supplier)
	suppliers["selfsigned"] = NewSelfSignedSupplier()

	if config.Lego != nil {
		legoSupplier, err := NewLegoSupplier(config.Lego)
		if err != nil {
			return nil, fmt.Errorf("unable to create lego certificate supplier: %w", err)
		}

		suppliers["lego"] = legoSupplier
		connectLego(ctx, legoSupplier, config.OnLegoConnected)
	}

	return NewWildcardResolver(
		NewManager(config.Store, suppliers, config.PreferredSuppliers, config.UseStaples),
		config.WildcardDomains,
	), nil
}

// legoStartupWait is the maximum time to wait for the first attempt to connect to the ACME server.
const legoStartupWait = 10 * time.Second

// connectLego connects the supplier to its ACME server in the background, waiting up to legoStartupWait for
// the first attempt to finish. If the supplier connects after that, onConnected is called.
func connectLego(ctx context.Context, supplier *LegoSupplier, onConnected func()) {
	attempted := make(chan struct{})
	connected := make(chan struct{})
	go func() {
		if err := supplier.connectWithBackoff(ctx, attempted); err != nil {
			slog.Debug("Stopped connecting to ACME server", "error", err)
			return
		}
		close(connected)
	}()

	select {
	case <-attempted:
	case <-time.After(legoStartupWait):
		slog.Warn("ACME server is slow to respond, continuing to connect in the background", "waited", legoStartupWait)
	}

	if _, err := supplier.currentCertifier(); err == nil || onConnected == nil {
		return
	}

	go func() {
		select {
		case <-connected:
			onConnected()
		case <-ctx.Done():
		}
	}()
}
