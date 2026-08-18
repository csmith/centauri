package frontend

// TailscaleOptions configures the tailscale frontend.
type TailscaleOptions struct {
	// Hostname is the name to use for the node on the tailnet.
	Hostname string
	// AuthKey is the key to use when joining the tailnet, if the node has not been authorised before.
	AuthKey string
	// Mode controls whether plain HTTP or HTTPS (with a redirect from HTTP) is served. One of "http"
	// or "https".
	Mode string
	// Dir is the directory used to persist tailscale state.
	Dir string
}
