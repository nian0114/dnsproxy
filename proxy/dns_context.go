package proxy

import (
	"net"
	"net/http"
	"time"

	"github.com/nian0114/dnsproxy/proxyutil"
	"github.com/nian0114/dnsproxy/upstream"
	"github.com/ameshkov/dnscrypt/v2"
	"github.com/lucas-clemente/quic-go"
	"github.com/miekg/dns"
)

// DNSContext represents a DNS request message context
type DNSContext struct {
	Proto Proto
	// Req is the request message.
	Req *dns.Msg
	// Res is the response message.
	Res *dns.Msg
	// Addr is the address of the client.
	Addr net.Addr
	// StartTime is the moment when request processing started.
	StartTime time.Time
	// Upstream is the upstream that resolved the request.  In case of cached
	// response it's nil.
	Upstream upstream.Upstream
	// CachedUpstreamAddr is the address of the upstream which the answer was
	// cached with.  It's empty for responses resolved by the upstream server.
	CachedUpstreamAddr string

	// CustomUpstreamConfig is only used for current request.  The Resolve
	// method of Proxy uses it instead of the default servers if it's not nil.
	CustomUpstreamConfig *UpstreamConfig

	// Conn is the underlying client connection.  It is nil if Proto is
	// ProtoDNSCrypt, ProtoHTTPS, or ProtoQUIC.
	Conn net.Conn

	// localIP - local IP address (for UDP socket to call udpMakeOOBWithSrc)
	localIP net.IP

	// HTTPRequest - HTTP request (for DOH only)
	HTTPRequest *http.Request
	// HTTPResponseWriter - HTTP response writer (for DOH only)
	HTTPResponseWriter http.ResponseWriter

	// DNSCryptResponseWriter - necessary to respond to a DNSCrypt query
	DNSCryptResponseWriter dnscrypt.ResponseWriter

	// QUICStream is the QUIC stream from which we got the query.  For
	// ProtoQUIC only.
	QUICStream quic.Stream

	// QUICSession is the QUIC session from which we got the query.  For
	// ProtoQUIC only.
	QUICSession quic.Session

	// RequestID is an opaque numerical identifier of this request that is
	// guaranteed to be unique across requests processed by a single Proxy
	// instance.
	RequestID uint64

	// ReqECS is the EDNS Client Subnet used in the request.
	ReqECS *net.IPNet

	// adBit is the authenticated data flag from the request.
	adBit bool
	// hasEDNS0 reflects if the request has EDNS0 RRs.
	hasEDNS0 bool
	// doBit is the DNSSEC OK flag from request's EDNS0 RR if presented.
	doBit bool
	// udpSize is the UDP buffer size from request's EDNS0 RR if presented,
	// or default otherwise.
	udpSize uint16
}

// calcFlagsAndSize lazily calculates some values required for Resolve method.
func (dctx *DNSContext) calcFlagsAndSize() {
	if dctx.udpSize != 0 || dctx.Req == nil {
		return
	}

	dctx.adBit = dctx.Req.AuthenticatedData
	dctx.udpSize = defaultUDPBufSize
	if o := dctx.Req.IsEdns0(); o != nil {
		dctx.hasEDNS0 = true
		dctx.doBit = o.Do()
		dctx.udpSize = o.UDPSize()
	}
}

// scrub prepares the d.Res to be written.  Truncation is applied as well if
// necessary.
func (dctx *DNSContext) scrub() {
	if dctx.Res == nil || dctx.Req == nil {
		return
	}

	// We should guarantee that all the values we need are calculated.
	dctx.calcFlagsAndSize()

	// RFC-6891 (https://tools.ietf.org/html/rfc6891) states that response
	// mustn't contain an EDNS0 RR if the request doesn't include it.
	//
	// See https://github.com/nian0114/dnsproxy/issues/132.
	if dctx.hasEDNS0 && dctx.Res.IsEdns0() == nil {
		dctx.Res.SetEdns0(dctx.udpSize, dctx.doBit)
	}

	dctx.Res.Truncate(proxyutil.DNSSize(dctx.Proto == ProtoUDP, dctx.Req))
	// Some devices require DNS message compression.
	dctx.Res.Compress = true
}
