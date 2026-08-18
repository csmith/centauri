package certificate

import (
	"context"
	"log/slog"
)

// ProviderConfig contains everything needed to assemble a certificate provider.
type ProviderConfig struct {
	// Store is where certificates are persisted.
	Store Store
	// Lego, if set, is used to create an ACME-backed supplier. If the supplier cannot be created a warning
	// is logged and the provider continues with the selfsigned supplier only.
	Lego *LegoSupplierConfig
	// PreferredSuppliers lists the names of the suppliers to use, in order of preference.
	PreferredSuppliers []string
	// WildcardDomains lists domains for which a single wildcard certificate should be requested.
	WildcardDomains []string
	// UseStaples enables requesting OCSP staples from suppliers.
	UseStaples bool
}

// NewProvider assembles a certificate provider from the given configuration: a certificate Manager backed
// by the configured store and suppliers, wrapped in a WildcardResolver.
func NewProvider(ctx context.Context, config ProviderConfig) *WildcardResolver {
	suppliers := make(map[string]Supplier)
	suppliers["selfsigned"] = NewSelfSignedSupplier()

	if config.Lego != nil {
		if legoSupplier, err := NewLegoSupplier(ctx, config.Lego); err != nil {
			slog.Warn("Unable to create lego certificate supplier", "error", err)
		} else {
			suppliers["lego"] = legoSupplier
		}
	}

	return NewWildcardResolver(
		NewManager(config.Store, suppliers, config.PreferredSuppliers, config.UseStaples),
		config.WildcardDomains,
	)
}
