package resolver

import (
	"context"
	"errors"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
)

// Domain Scopes.
var (
	// Localhost Domain
	// Handling: Respond with 127.0.0.1 and ::1 to A and AAAA queries, respectively.
	// See RFC6761.
	localhostDomain = ".localhost."

	// Invalid Domain
	// Handling: Always respond with NXDOMAIN.
	// See RFC6761.
	invalidDomain = ".invalid."

	// Internal Special-Use Domain
	// Used by Portmaster for special addressing.
	internalSpecialUseDomains = []string{
		"." + InternalSpecialUseDomain,
	}

	// Multicast DNS
	// Handling: Send to nameservers with matching search scope, then MDNS
	// See RFC6762.
	multicastDomains = []string{
		".local.",
		".254.169.in-addr.arpa.",
		".8.e.f.ip6.arpa.",
		".9.e.f.ip6.arpa.",
		".a.e.f.ip6.arpa.",
		".b.e.f.ip6.arpa.",
	}

	// Special-Use Domain Names
	// Handling: Send to nameservers with matching search scope, then local and system assigned nameservers
	// IANA Ref: https://www.iana.org/assignments/special-use-domain-names
	specialUseDomains = []string{
		// RFC8375: Designated for non-unique use in residential home networks.
		".home.arpa.",

		// RFC6762 (Appendix G): Non-official, but officially listed, private use domains.
		".intranet.",
		".internal.",
		".private.",
		".corp.",
		".home.",
		".lan.",

		// RFC6761: IPv4 private-address reverse-mapping domains.
		".10.in-addr.arpa.",
		".16.172.in-addr.arpa.",
		".17.172.in-addr.arpa.",
		".18.172.in-addr.arpa.",
		".19.172.in-addr.arpa.",
		".20.172.in-addr.arpa.",
		".21.172.in-addr.arpa.",
		".22.172.in-addr.arpa.",
		".23.172.in-addr.arpa.",
		".24.172.in-addr.arpa.",
		".25.172.in-addr.arpa.",
		".26.172.in-addr.arpa.",
		".27.172.in-addr.arpa.",
		".28.172.in-addr.arpa.",
		".29.172.in-addr.arpa.",
		".30.172.in-addr.arpa.",
		".31.172.in-addr.arpa.",
		".168.192.in-addr.arpa.",

		// RFC4193: IPv6 private-address reverse-mapping domains.
		".d.f.ip6.arpa.",
		".c.f.ip6.arpa.",

		// RFC6761: Special use domains for documentation and testing.
		".example.",
		".example.com.",
		".example.net.",
		".example.org.",
		".test.",
	}

	// Special-Service Domain Names
	// Handling: Send to nameservers with matching search scope, then local and system assigned nameservers.
	specialServiceDomains = []string{
		// RFC7686: Tor Hidden Services
		".onion.",

		// Namecoin: Blockchain based nameservice, https://www.namecoin.org/
		".bit.",
	}
)

func domainInScope(dotPrefixedFQDN string, scopeList []string) bool {
	for _, scope := range scopeList {
		if strings.HasSuffix(dotPrefixedFQDN, scope) {
			return true
		}
	}
	return false
}

// GetResolversInScope returns all resolvers that are in scope the resolve the given query and options.
func GetResolversInScope(ctx context.Context, q *Query) (selected []*Resolver, primarySource string, tryAll bool) { //nolint:gocognit // TODO
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	// Internal use domains
	if domainInScope(q.dotPrefixedFQDN, internalSpecialUseDomains) {
		return envResolvers, ServerSourceEnv, false
	}

	// Special connectivity domains
	if netenv.IsConnectivityDomain(q.FQDN) && len(systemResolvers) > 0 {
		// Do not do compliance checks for connectivity domains.
		selected = append(selected, systemResolvers...) // dhcp assigned resolvers
		return selected, ServerSourceOperatingSystem, false
	}

	// Prioritize search scopes
	for _, scope := range localScopes {
		if strings.HasSuffix(q.dotPrefixedFQDN, scope.Domain) {
			selected = addResolvers(ctx, q, selected, scope.Resolvers)
		}
	}

	// Handle multicast domains
	if domainInScope(q.dotPrefixedFQDN, multicastDomains) {
		selected = addResolvers(ctx, q, selected, mDNSResolvers)
		selected = addResolvers(ctx, q, selected, localResolvers)
		selected = addResolvers(ctx, q, selected, systemResolvers)
		return selected, ServerSourceMDNS, true
	}

	// Special use domains
	if domainInScope(q.dotPrefixedFQDN, specialUseDomains) ||
		domainInScope(q.dotPrefixedFQDN, specialServiceDomains) {
		selected = addResolvers(ctx, q, selected, localResolvers)
		selected = addResolvers(ctx, q, selected, systemResolvers)
		return selected, "special", true
	}

	// Global domains
	selected = addResolvers(ctx, q, selected, globalResolvers)
	return selected, ServerSourceConfigured, false
}

func addResolvers(ctx context.Context, q *Query, selected []*Resolver, addResolvers []*Resolver) []*Resolver {
addNextResolver:
	for _, resolver := range addResolvers {
		// check for compliance
		if err := resolver.checkCompliance(ctx, q); err != nil {
			log.Tracer(ctx).Tracef("skipping non-compliant resolver %s: %s", resolver.Info.DescriptiveName(), err)
			continue
		}

		// deduplicate
		for _, selectedResolver := range selected {
			if selectedResolver.Info.ID() == resolver.Info.ID() {
				continue addNextResolver
			}
		}

		// add compliant and unique resolvers to selected resolvers
		selected = append(selected, resolver)
	}
	return selected
}

var (
	errInsecureProtocol = errors.New("insecure protocols disabled")
	errAssignedServer   = errors.New("assigned (dhcp) nameservers disabled")
	errMulticastDNS     = errors.New("multicast DNS disabled")
)

func (q *Query) checkCompliance() error {
	// RFC6761 - always respond with nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, invalidDomain) {
		return ErrNotFound
	}

	// RFC6761 - respond with 127.0.0.1 and ::1 to A and AAAA queries respectively, else nxdomain
	if strings.HasSuffix(q.dotPrefixedFQDN, localhostDomain) {
		switch uint16(q.QType) {
		case dns.TypeA, dns.TypeAAAA:
			return ErrLocalhost
		default:
			return ErrNotFound
		}
	}

	// special TLDs
	if dontResolveSpecialDomains(q.SecurityLevel) &&
		domainInScope(q.dotPrefixedFQDN, specialServiceDomains) {
		return ErrSpecialDomainsDisabled
	}

	return nil
}

func (resolver *Resolver) checkCompliance(_ context.Context, q *Query) error {
	if noInsecureProtocols(q.SecurityLevel) {
		switch resolver.Info.Type {
		case ServerTypeDNS:
			return errInsecureProtocol
		case ServerTypeTCP:
			return errInsecureProtocol
		case ServerTypeDoT:
			// compliant
		case ServerTypeDoH:
			// compliant
		case ServerTypeEnv:
			// compliant (data is sourced from local network only and is highly limited)
		default:
			return errInsecureProtocol
		}
	}

	if noAssignedNameservers(q.SecurityLevel) {
		if resolver.Info.Source == ServerSourceOperatingSystem {
			return errAssignedServer
		}
	}

	if noMulticastDNS(q.SecurityLevel) {
		if resolver.Info.Source == ServerSourceMDNS {
			return errMulticastDNS
		}
	}

	return nil
}
