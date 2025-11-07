package routing

import (
	"context"
	"net/netip"
)

// Router implements the discovery of content.
type Router interface {
	// Ready returns true when the router is ready.
	Ready(ctx context.Context) (bool, error)
	// Resolve asynchronously discovers addresses that can serve the content defined by the give key.
	Resolve(ctx context.Context, key string, count int) (<-chan netip.AddrPort, error)
	// Advertise broadcasts that the current router can serve the content.
	Advertise(ctx context.Context, keys []string) error
	// Fetches keys from a remote peer address discovered via calling Resolve(PeerIndexKey, ...)
	FetchPeerKeys(ctx context.Context, peer netip.AddrPort) (string, error)
	// Exposes the currently maintained local set of content keys (digests, etc.) of this peer
	ServeKeys(ctx context.Context, data string) error

}

// Well-known key used to advertise peer presence so that all peers can be found easily
const PeerIndexKey = "__peer_index__"
