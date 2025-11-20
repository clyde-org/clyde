package routing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"io"
	"sync"

	"clyde/pkg/metrics"

	"github.com/go-logr/logr"
	cid "github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	mc "github.com/multiformats/go-multicodec"
	mh "github.com/multiformats/go-multihash"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/afero"
	"github.com/libp2p/go-libp2p/core/network"
)

// FileSystem interface defines methods that abstract over conventional file operations
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(path string, data []byte, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
}

// InMemoryFileSystem is a concrete implementation using afero's MemMapFs
type InMemoryFileSystem struct {
	fs afero.Fs
}

// NewInMemoryFileSystem creates a new instance of InMemoryFileSystem
func NewInMemoryFileSystem() *InMemoryFileSystem {
	return &InMemoryFileSystem{
		fs: afero.NewMemMapFs(),
	}
}

// MkdirAll creates directories, analogous to os.MkdirAll
func (ims *InMemoryFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return ims.fs.MkdirAll(path, perm)
}

// WriteFile writes data to a file, analogous to os.WriteFile
func (ims *InMemoryFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	return afero.WriteFile(ims.fs, path, data, perm)
}

// ReadFile reads the content of a file, analogous to os.ReadFile
func (ims *InMemoryFileSystem) ReadFile(path string) ([]byte, error) {
	return afero.ReadFile(ims.fs, path)
}

const (
	// KeyTTL is a timeslice used in the passive synchronisation mechanism within the state tracker
	KeyTTL = 2 * time.Minute
	// KeyExchangeProtocol is used for direct interaction between peers
	KeyExchangeProtocol = "/clyde/keys/1.0.0"
)

type P2PRouterConfig struct {
	DataDir    string
	Libp2pOpts []libp2p.Option
	IncludeImages []string
}

func (cfg *P2PRouterConfig) Apply(opts ...P2PRouterOption) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(cfg); err != nil {
			return err
		}
	}
	return nil
}

type P2PRouterOption func(cfg *P2PRouterConfig) error

func WithLibP2POptions(opts ...libp2p.Option) P2PRouterOption {
	return func(cfg *P2PRouterConfig) error {
		cfg.Libp2pOpts = opts
		return nil
	}
}

func WithDataDir(dataDir string) P2PRouterOption {
	return func(cfg *P2PRouterConfig) error {
		cfg.DataDir = dataDir
		return nil
	}
}

func WithIncludeImages(includeImages []string) P2PRouterOption {
	return func(cfg *P2PRouterConfig) error {
		cfg.IncludeImages = includeImages
		return nil
	}
}

var _ Router = &P2PRouter{}

type P2PRouter struct {
	bootstrapper Bootstrapper
	host         host.Host
	kdht         *dht.IpfsDHT
	rd           *routing.RoutingDiscovery
	registryPort uint16
	// Used exclusively by handleKeyRequest to return current local keys
	localKeysProvider func(ctx context.Context) (string, error)
	// For processing peer addresses in conjunction with the peer index key implementation (e.g., map the resolved net address to remote peer id)
	peerIDByAddr map[netip.AddrPort]peer.ID
	mxPeerIDs	 sync.RWMutex
}

func NewP2PRouter(ctx context.Context, addr string, bs Bootstrapper, registryPortStr string, opts ...P2PRouterOption) (*P2PRouter, error) {
	cfg := P2PRouterConfig{}
	err := cfg.Apply(opts...)
	if err != nil {
		return nil, err
	}

	registryPort, err := strconv.ParseUint(registryPortStr, 10, 16)
	if err != nil {
		return nil, err
	}

	multiAddrs, err := listenMultiaddrs(addr)
	if err != nil {
		return nil, err
	}
	addrFactoryOpt := libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
		var ip4Ma, ip6Ma ma.Multiaddr
		for _, addr := range addrs {
			if manet.IsIPLoopback(addr) {
				continue
			}
			if isIp6(addr) {
				ip6Ma = addr
				continue
			}
			ip4Ma = addr
		}
		if ip6Ma != nil {
			return []ma.Multiaddr{ip6Ma}
		}
		if ip4Ma != nil {
			return []ma.Multiaddr{ip4Ma}
		}
		return nil
	})
	libp2pOpts := []libp2p.Option{
		libp2p.ListenAddrs(multiAddrs...),
		libp2p.PrometheusRegisterer(metrics.DefaultRegisterer),
		addrFactoryOpt,
	}
	if cfg.DataDir != "" {
		peerKey, err := loadOrCreatePrivateKey(ctx, cfg.DataDir)
		if err != nil {
			return nil, err
		}
		libp2pOpts = append(libp2pOpts, libp2p.Identity(peerKey))
	}
	libp2pOpts = append(libp2pOpts, cfg.Libp2pOpts...)
	host, err := libp2p.New(libp2pOpts...)
	if err != nil {
		return nil, fmt.Errorf("could not create host: %w", err)
	}
	if len(host.Addrs()) != 1 {
		addrs := []string{}
		for _, addr := range host.Addrs() {
			addrs = append(addrs, addr.String())
		}
		return nil, fmt.Errorf("expected single host address but got %d %s", len(addrs), strings.Join(addrs, ", "))
	}

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.ProtocolPrefix("/spegel"),
		dht.DisableValues(),
		dht.MaxRecordAge(KeyTTL),
		dht.BootstrapPeersFunc(bootstrapFunc(ctx, bs, host)),
	}
	kdht, err := dht.New(ctx, host, dhtOpts...)
	if err != nil {
		return nil, fmt.Errorf("could not create distributed hash table: %w", err)
	}
	rd := routing.NewRoutingDiscovery(kdht)

	// Initialising the P2PRouter with elements related to peer discovery
	r := &P2PRouter {
		bootstrapper:		bs,
		host:				host,
		kdht:				kdht,
		rd:					rd,
		registryPort:		uint16(registryPort),
		peerIDByAddr:		make(map[netip.AddrPort]peer.ID),
		localKeysProvider:	nil,
	}

	// Set the inbound handler for peer key exchange as without it the peers connot communicate with each other directly
	host.SetStreamHandler(KeyExchangeProtocol, r.handleKeyRequest)

	return r, nil
}

func (r *P2PRouter) Run(ctx context.Context) (err error) {
	self := fmt.Sprintf("%s/p2p/%s", r.host.Addrs()[0].String(), r.host.ID().String())
	logr.FromContextOrDiscard(ctx).WithName("p2p").Info("starting p2p router", "id", self)
	if err := r.kdht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("could not bootstrap distributed hash table: %w", err)
	}
	defer func() {
		cerr := r.host.Close()
		if cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	err = r.bootstrapper.Run(ctx, self)
	if err != nil {
		return err
	}
	return nil
}

func (r *P2PRouter) Ready(ctx context.Context) (bool, error) {
	addrInfos, err := r.bootstrapper.Get(ctx)
	if err != nil {
		return false, err
	}
	if len(addrInfos) == 0 {
		return false, nil
	}
	if len(addrInfos) == 1 {
		matches, err := hostMatches(*host.InfoFromHost(r.host), addrInfos[0])
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	if r.kdht.RoutingTable().Size() > 0 {
		return true, nil
	}
	err = r.kdht.Bootstrap(ctx)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (r *P2PRouter) Resolve(ctx context.Context, key string, count int) (<-chan netip.AddrPort, error) {
	log := logr.FromContextOrDiscard(ctx).WithValues("host", r.host.ID().String(), "key", key, "count", count)
	log.Info("starting Resolve")

	c, err := createCid(key)
	if err != nil {
		log.Error(err, "failed to create CID")
		return nil, err
	}

	// If using unlimited retries (count=0), ensure that the peer address channel
	// does not become blocking by using a reasonable non-zero buffer size.
	peerBufferSize := count
	if peerBufferSize == 0 {
		peerBufferSize = 20
	}
	log.Info("initializing channels", "peerBufferSize", peerBufferSize)

	addrInfoCh := r.rd.FindProvidersAsync(ctx, c, count)
	peerCh := make(chan netip.AddrPort, peerBufferSize)

	// Special case implementation for specifically resolving peer index key, accepting multiple addresses per provider
	if key == PeerIndexKey {
		go func() {
			resolveTimer := prometheus.NewTimer(metrics.ResolveDurHistogram.WithLabelValues("libp2p"))
			for addrInfo := range addrInfoCh {
				resolveTimer.ObserveDuration()

				// Go through all reported addresses
				for _, maddr := range addrInfo.Addrs {
					ip, err := manet.ToIP(maddr)
					if err != nil {
						log.Error(
							err, 
							"could not get ip address")
						continue
					}
					ipAddr, ok := netip.AddrFromSlice(ip)
					if !ok {
						log.Error(
							errors.New("ip is not based on ipv4 or ipv6"), 
							"could not convert ip")
						continue
					}
					ap := netip.AddrPortFrom(ipAddr, r.registryPort)

					// Record mapping of address to relevant peer id
					r.mxPeerIDs.Lock()
					r.peerIDByAddr[ap] = addrInfo.ID
					r.mxPeerIDs.Unlock()

					select {
					case peerCh <- ap:
					default:
						log.V(4).Info("peer index: dropped peer, channel full")
					}
				}
			}
			close(peerCh)
		}()
		return peerCh, nil
	}

	go func() {
		defer close(peerCh)
		resolveTimer := prometheus.NewTimer(metrics.ResolveDurHistogram.WithLabelValues("libp2p"))
		defer resolveTimer.ObserveDuration()

		for addrInfo := range addrInfoCh {
			log.Info("received provider from addrInfoCh",
				"peerID", addrInfo.ID.String(),
				"numAddrs", len(addrInfo.Addrs),
			)

			if len(addrInfo.Addrs) != 1 {
				addrs := []string{}
				for _, addr := range addrInfo.Addrs {
					addrs = append(addrs, addr.String())
				}
				log.Info("unexpected number of addresses",
					"addresses", strings.Join(addrs, ", "),
				)
				continue
			}

			ip, err := manet.ToIP(addrInfo.Addrs[0])
			if err != nil {
				log.Error(err, "could not extract IP from multiaddr")
				continue
			}

			ipAddr, ok := netip.AddrFromSlice(ip)
			if !ok {
				log.Error(errors.New("invalid IP"), "conversion to netip.Addr failed", "rawIP", ip.String())
				continue
			}

			peer := netip.AddrPortFrom(ipAddr, r.registryPort)
			log.Info("constructed peer address", "peer", peer.String())

			// Don't block if the client has disconnected before reading all values from the channel
			select {
			case peerCh <- peer:
				log.Info("peer sent to peerCh", "peer", peer.String())
			default:
				log.V(4).Info("peer dropped: peer channel is full", "peer", peer.String())
			}
		}

		log.Info("addrInfoCh closed, finishing Resolve goroutine")
	}()

	return peerCh, nil
}

func (r *P2PRouter) Advertise(ctx context.Context, keys []string) error {
	for _, key := range keys {
		c, err := createCid(key)
		if err != nil {
			return err
		}
		err = r.rd.Provide(ctx, c, false)
		if err != nil {
			return err
		}
	}
	return nil
}

// Used to serve information about local keys to other peers
func (r *P2PRouter) ServeKeys(ctx context.Context, data string) error {
	logr.FromContextOrDiscard(ctx).Info("serving information about local keys as a json string representation to other peers", 
		"length", len(data))

	r.localKeysProvider = func(_ context.Context) (string, error) {
		return data, nil
	}
	
	return nil
}

// Fetches keys from a peer discovered via Resolve(PeerIndexKey, ...)
func (r *P2PRouter) FetchPeerKeys(ctx context.Context, peerAddr netip.AddrPort) (string, error) {
	log := logr.FromContextOrDiscard(ctx).WithValues("peer", peerAddr)

	// Look up the peer by its id detected during Resolve(PeerIndexKey, ...)
	r.mxPeerIDs.RLock()
	pid, ok := r.peerIDByAddr[peerAddr]
	r.mxPeerIDs.RUnlock()
	if !ok {
		return "", fmt.Errorf(
			"unknown peer id for %v; ensure Resolve(%q, ...) ran first", 
			peerAddr, 
			PeerIndexKey)
	}

	// Build a dialable address information data structure with the resolved ip/port and the known peer identifier
	maddr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", peerAddr.Addr().String(), peerAddr.Port()))

	if err != nil {
		return "", err
	}

	pai := peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{maddr}}

	if err := r.host.Connect(ctx, pai); err != nil {
		return "", err
	}

	s, err := r.host.NewStream(ctx, pid, KeyExchangeProtocol)
	
	if err != nil {
		return "", err
	}
	
	defer s.Close()

	jsonData, err := io.ReadAll(s)
	
	if err != nil {
		return "", fmt.Errorf(
			"failed to read json data from stream: %w", 
			err)
	}

	log.Info("fetched peer keys", "length", len(jsonData))
	return string(jsonData), nil
}

// Special key request handler
func (r * P2PRouter) handleKeyRequest(s network.Stream) {
	defer s.Close()
	if r.localKeysProvider == nil {
		_, _ = s.Write([]byte("[]"))
		return
	}
	keys, err := r.localKeysProvider(context.Background())
	if err != nil {
		_, _ = s.Write([]byte("[]"))
		return
	}

	_, _ = s.Write([]byte(keys))
}



func bootstrapFunc(ctx context.Context, bootstrapper Bootstrapper, h host.Host) func() []peer.AddrInfo {
	log := logr.FromContextOrDiscard(ctx).WithName("p2p")
	return func() []peer.AddrInfo {
		bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer bootstrapCancel()

		// TODO (phillebaba): Consider if we should do a best effort bootstrap without host address.
		hostAddrs := h.Addrs()
		if len(hostAddrs) == 0 {
			return nil
		}
		var hostPort ma.Component
		ma.ForEach(hostAddrs[0], func(c ma.Component) bool {
			if c.Protocol().Code == ma.P_TCP {
				hostPort = c
				return false
			}
			return true
		})

		addrInfos, err := bootstrapper.Get(bootstrapCtx)
		if err != nil {
			log.Error(err, "could not get bootstrap addresses")
			return nil
		}
		filteredAddrInfos := []peer.AddrInfo{}
		for _, addrInfo := range addrInfos {
			// Skip addresses that match host.
			matches, err := hostMatches(*host.InfoFromHost(h), addrInfo)
			if err != nil {
				log.Error(err, "could not compare host with address")
				continue
			}
			if matches {
				log.Info("skipping bootstrap peer that is same as host")
				continue
			}

			// Add port to address if it is missing.
			modifiedAddrs := []ma.Multiaddr{}
			for _, addr := range addrInfo.Addrs {
				hasPort := false
				ma.ForEach(addr, func(c ma.Component) bool {
					if c.Protocol().Code == ma.P_TCP {
						hasPort = true
						return false
					}
					return true
				})
				if hasPort {
					modifiedAddrs = append(modifiedAddrs, addr)
					continue
				}
				modifiedAddrs = append(modifiedAddrs, ma.Join(addr, &hostPort))
			}
			addrInfo.Addrs = modifiedAddrs

			// Resolve ID if it is missing.
			if addrInfo.ID != "" {
				filteredAddrInfos = append(filteredAddrInfos, addrInfo)
				continue
			}
			addrInfo.ID = "id"
			err = h.Connect(bootstrapCtx, addrInfo)
			var mismatchErr sec.ErrPeerIDMismatch
			if !errors.As(err, &mismatchErr) {
				log.Error(err, "could not get peer id")
				continue
			}
			addrInfo.ID = mismatchErr.Actual
			filteredAddrInfos = append(filteredAddrInfos, addrInfo)
		}
		if len(filteredAddrInfos) == 0 {
			log.Info("no bootstrap nodes found")
			return nil
		}
		return filteredAddrInfos
	}
}

func listenMultiaddrs(addr string) ([]ma.Multiaddr, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	tcpComp, err := ma.NewMultiaddr(fmt.Sprintf("/tcp/%s", p))
	if err != nil {
		return nil, err
	}
	ipComps := []ma.Multiaddr{}
	ip := net.ParseIP(h)
	if ip.To4() != nil {
		ipComp, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%s", h))
		if err != nil {
			return nil, fmt.Errorf("could not create host multi address: %w", err)
		}
		ipComps = append(ipComps, ipComp)
	} else if ip.To16() != nil {
		ipComp, err := ma.NewMultiaddr(fmt.Sprintf("/ip6/%s", h))
		if err != nil {
			return nil, fmt.Errorf("could not create host multi address: %w", err)
		}
		ipComps = append(ipComps, ipComp)
	}
	if len(ipComps) == 0 {
		ipComps = []ma.Multiaddr{manet.IP6Unspecified, manet.IP4Unspecified}
	}
	multiAddrs := []ma.Multiaddr{}
	for _, ipComp := range ipComps {
		multiAddrs = append(multiAddrs, ipComp.Encapsulate(tcpComp))
	}
	return multiAddrs, nil
}

func isIp6(m ma.Multiaddr) bool {
	c, _ := ma.SplitFirst(m)
	if c == nil || c.Protocol().Code != ma.P_IP6 {
		return false
	}
	return true
}

func createCid(key string) (cid.Cid, error) {
	pref := cid.Prefix{
		Version:  1,
		Codec:    uint64(mc.Raw),
		MhType:   mh.SHA2_256,
		MhLength: -1,
	}
	c, err := pref.Sum([]byte(key))
	if err != nil {
		return cid.Cid{}, err
	}
	return c, nil
}

func hostMatches(host, addrInfo peer.AddrInfo) (bool, error) {
	// Skip self when address ID matches host ID.
	if host.ID != "" && addrInfo.ID != "" {
		return host.ID == addrInfo.ID, nil
	}

	// Skip self when IP matches
	hostIP, err := manet.ToIP(host.Addrs[0])
	if err != nil {
		return false, err
	}
	for _, addr := range addrInfo.Addrs {
		addrIP, err := manet.ToIP(addr)
		if err != nil {
			return false, err
		}
		if hostIP.Equal(addrIP) {
			return true, nil
		}
	}

	return false, nil
}

func loadOrCreatePrivateKey(ctx context.Context, dataDir string) (crypto.PrivKey, error) { //nolint: ireturn // LibP2P returns interfaces so we also have to.
	keyPath := filepath.Join(dataDir, "private.key")
	log := logr.FromContextOrDiscard(ctx).WithValues("path", keyPath)
	fs := NewInMemoryFileSystem()
	err := fs.MkdirAll(dataDir, os.FileMode(0o755))
	// err := os.MkdirAll(dataDir, 0o755)
	if err != nil {
		return nil, err
	}
	// b, err := os.ReadFile(keyPath)
	b, err := fs.ReadFile(keyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		log.Info("creating a new private key")
		privKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}
		rawBytes, err := privKey.Raw()
		if err != nil {
			return nil, err
		}
		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(ed25519.PrivateKey(rawBytes))
		if err != nil {
			return nil, err
		}
		block := &pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: pkcs8Bytes,
		}
		pemData := pem.EncodeToMemory(block)
		// err = os.WriteFile(keyPath, pemData, 0o600)
		err = fs.WriteFile(keyPath, pemData, os.FileMode(0o600))
		if err != nil {
			return nil, err
		}
		return privKey, nil
	}
	log.Info("loading the private key from data directory")
	block, _ := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("invalid PEM block type %s", block.Type)
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	edKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an Ed25519 private key")
	}
	privKey, err := crypto.UnmarshalEd25519PrivateKey(edKey)
	if err != nil {
		return nil, err
	}
	return privKey, nil
}
