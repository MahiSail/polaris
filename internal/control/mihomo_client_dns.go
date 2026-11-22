package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

// MihomoClientDNS is the form editor for the subscription's DNS section. The
// generator no longer decides any of this: a client configuration carries
// whatever the operator entered, and leaving it disabled emits no dns block at
// all so the client keeps its own settings.
type MihomoClientDNS struct {
	Enable                       bool     `json:"enable"`
	IPv6                         bool     `json:"ipv6"`
	EnhancedMode                 string   `json:"enhanced_mode"`
	FakeIPRange                  string   `json:"fake_ip_range"`
	FakeIPFilterMode             string   `json:"fake_ip_filter_mode"`
	FakeIPFilter                 []string `json:"fake_ip_filter"`
	RespectRules                 bool     `json:"respect_rules"`
	DefaultNameserver            []string `json:"default_nameserver"`
	Nameserver                   []string `json:"nameserver"`
	ProxyServerNameserver        []string `json:"proxy_server_nameserver"`
	DirectNameserver             []string `json:"direct_nameserver"`
	DirectNameserverFollowPolicy bool     `json:"direct_nameserver_follow_policy"`
}

// mihomoYAMLDNS is the emitted shape; field order is the document order.
type mihomoYAMLDNS struct {
	Enable                       bool     `yaml:"enable"`
	IPv6                         bool     `yaml:"ipv6"`
	EnhancedMode                 string   `yaml:"enhanced-mode"`
	FakeIPRange                  string   `yaml:"fake-ip-range,omitempty"`
	FakeIPFilterMode             string   `yaml:"fake-ip-filter-mode,omitempty"`
	FakeIPFilter                 []string `yaml:"fake-ip-filter,omitempty"`
	RespectRules                 bool     `yaml:"respect-rules"`
	DefaultNameserver            []string `yaml:"default-nameserver"`
	Nameserver                   []string `yaml:"nameserver"`
	ProxyServerNameserver        []string `yaml:"proxy-server-nameserver,omitempty"`
	DirectNameserver             []string `yaml:"direct-nameserver,omitempty"`
	DirectNameserverFollowPolicy bool     `yaml:"direct-nameserver-follow-policy"`
}

// defaultMihomoClientDNS is what the generator used to hard-code. It is the
// starting point for a new client configuration and what existing rows are
// migrated to, so their subscriptions keep working unchanged.
func defaultMihomoClientDNS() MihomoClientDNS {
	return MihomoClientDNS{
		Enable:                true,
		EnhancedMode:          "fake-ip",
		FakeIPRange:           "198.18.0.1/16",
		FakeIPFilterMode:      "rule",
		FakeIPFilter:          []string{"DOMAIN-SUFFIX,lan,real-ip", "DOMAIN-SUFFIX,local,real-ip", "DOMAIN-SUFFIX,localhost,real-ip", "DOMAIN-SUFFIX,msftconnecttest.com,real-ip", "DOMAIN-SUFFIX,msftncsi.com,real-ip", "DOMAIN,time.windows.com,real-ip", "DOMAIN-SUFFIX,ntp.org,real-ip", "DOMAIN-SUFFIX,nrd.nie.163.com,real-ip", "DOMAIN-SUFFIX,fp.ps.netease.com,real-ip", "MATCH,fake-ip"},
		DefaultNameserver:     []string{"223.5.5.5", "119.29.29.29"},
		Nameserver:            []string{"https://223.5.5.5/dns-query"},
		ProxyServerNameserver: []string{"https://223.5.5.5/dns-query"},
		DirectNameserver:      []string{"https://223.5.5.5/dns-query"},
		RespectRules:          true,
	}
}

// adoptHardCodedMihomoClientDNS gives rows written before the DNS section
// became editable exactly the settings the generator used to write, so their
// subscriptions do not change under the operator.
func (s *Store) adoptHardCodedMihomoClientDNS(ctx context.Context) error {
	encoded, err := json.Marshal(mihomoClientDNSV3{Mode: "form", DNS: defaultMihomoClientDNS()})
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE mihomo_client_configs_v3 SET dns_json = ? WHERE dns_json = ''`, string(encoded)); err != nil {
		return fmt.Errorf("migrate Mihomo client DNS settings: %w", err)
	}
	return nil
}

func normalizeMihomoDNSServers(servers []string, field string) ([]string, error) {
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if strings.ContainsAny(server, "\r\n") {
			return nil, fmt.Errorf("%s entries cannot contain line breaks", field)
		}
		result = append(result, server)
	}
	return result, nil
}

// Mihomo rejects a default-nameserver that is not a plain IP, because that
// list is what resolves the other resolvers' host names. A URL is accepted as
// long as its host is already an address.
func validateMihomoDefaultNameserver(server string) error {
	if server == "system" || net.ParseIP(server) != nil {
		return nil
	}
	host := server
	if parsed, err := url.Parse(server); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	if bare, _, err := net.SplitHostPort(host); err == nil {
		host = bare
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("default-nameserver %q must be a plain IP address", server)
	}
	return nil
}

func normalizeMihomoClientDNS(config *MihomoClientConfig) error {
	config.DNSMode = strings.ToLower(strings.TrimSpace(config.DNSMode))
	if config.DNSMode == "" || config.DNSMode == "structured" {
		config.DNSMode = "form"
	}
	config.RawDNS = strings.TrimSpace(strings.ReplaceAll(config.RawDNS, "\r\n", "\n"))
	switch config.DNSMode {
	case "form":
		if err := normalizeMihomoClientDNSForm(config); err != nil {
			return err
		}
		raw, err := renderMihomoDNSYAML(config.DNS, config.RawDNS)
		if err != nil {
			return fmt.Errorf("render DNS YAML: %w", err)
		}
		config.RawDNS = raw
		return nil
	case "text":
		if err := validateMihomoClientRawDNS(config.RawDNS); err != nil {
			return err
		}
		parsed, err := parseMihomoClientRawDNS(config.RawDNS)
		if err != nil {
			return fmt.Errorf("parse DNS YAML: %w", err)
		}
		config.DNS = parsed
		return nil
	default:
		return errors.New("DNS mode must be form or text")
	}
}

func normalizeMihomoClientDNSForm(config *MihomoClientConfig) error {
	dns := &config.DNS
	dns.EnhancedMode = strings.ToLower(strings.TrimSpace(dns.EnhancedMode))
	dns.FakeIPFilterMode = strings.ToLower(strings.TrimSpace(dns.FakeIPFilterMode))
	dns.FakeIPRange = strings.TrimSpace(dns.FakeIPRange)
	if !dns.Enable {
		// The section is off, so nothing is emitted and the half-filled
		// values are kept as they are for when it is switched back on.
		return nil
	}
	if dns.EnhancedMode == "" {
		dns.EnhancedMode = "fake-ip"
	}
	switch dns.EnhancedMode {
	case "normal", "fake-ip", "redir-host":
	default:
		return fmt.Errorf("DNS enhanced mode %q is not supported", dns.EnhancedMode)
	}
	var err error
	if dns.FakeIPFilter, err = normalizeMihomoDNSServers(dns.FakeIPFilter, "fake-ip-filter"); err != nil {
		return err
	}
	if dns.EnhancedMode == "fake-ip" {
		if dns.FakeIPFilterMode == "" {
			dns.FakeIPFilterMode = "blacklist"
		}
		switch dns.FakeIPFilterMode {
		case "blacklist", "whitelist", "rule":
		default:
			return fmt.Errorf("fake-ip filter mode %q is not supported", dns.FakeIPFilterMode)
		}
		prefix, err := netip.ParsePrefix(dns.FakeIPRange)
		if err != nil || !prefix.Addr().Is4() {
			return errors.New("fake-ip-range must be an IPv4 CIDR such as 198.18.0.1/16")
		}
	} else {
		dns.FakeIPRange, dns.FakeIPFilterMode, dns.FakeIPFilter = "", "", nil
	}
	for _, servers := range []struct {
		values *[]string
		field  string
	}{
		{&dns.DefaultNameserver, "default-nameserver"},
		{&dns.Nameserver, "nameserver"},
		{&dns.ProxyServerNameserver, "proxy-server-nameserver"},
		{&dns.DirectNameserver, "direct-nameserver"},
	} {
		if *servers.values, err = normalizeMihomoDNSServers(*servers.values, servers.field); err != nil {
			return err
		}
	}
	// The three checks below are the ones Mihomo itself refuses to start on.
	if len(dns.Nameserver) == 0 {
		return errors.New("nameserver is required when the DNS section is enabled")
	}
	if len(dns.DefaultNameserver) == 0 {
		return errors.New("default-nameserver needs at least one entry")
	}
	for _, server := range dns.DefaultNameserver {
		if err := validateMihomoDefaultNameserver(server); err != nil {
			return err
		}
	}
	if dns.RespectRules && len(dns.ProxyServerNameserver) == 0 {
		return errors.New("proxy-server-nameserver is required when DNS follows the routing rules")
	}
	return nil
}

// unwrapMihomoRawDNSNode extracts the inner DNS mapping if the user wrote an outer "dns:" key,
// preventing duplicate "dns:\n  dns:" serialization in subscriptions while still accepting standard Clash configs.
func unwrapMihomoRawDNSNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	target := node
	if target.Kind == yaml.DocumentNode {
		if len(target.Content) == 0 {
			return nil
		}
		target = target.Content[0]
	}
	if target.Kind != yaml.MappingNode {
		return target
	}
	if len(target.Content) == 2 && strings.EqualFold(strings.TrimSpace(target.Content[0].Value), "dns") && target.Content[1].Kind == yaml.MappingNode {
		return target.Content[1]
	}
	return target
}

func renderMihomoDNSYAML(dns MihomoClientDNS, existingRaw string) (string, error) {
	if !dns.Enable {
		return "", nil
	}
	section := mihomoYAMLDNS{
		Enable:                       true,
		IPv6:                         dns.IPv6,
		EnhancedMode:                 dns.EnhancedMode,
		FakeIPRange:                  dns.FakeIPRange,
		FakeIPFilterMode:             dns.FakeIPFilterMode,
		FakeIPFilter:                 dns.FakeIPFilter,
		RespectRules:                 dns.RespectRules,
		DefaultNameserver:            dns.DefaultNameserver,
		Nameserver:                   dns.Nameserver,
		ProxyServerNameserver:        dns.ProxyServerNameserver,
		DirectNameserver:             dns.DirectNameserver,
		DirectNameserverFollowPolicy: dns.DirectNameserverFollowPolicy,
	}

	trimmed := strings.TrimSpace(existingRaw)
	if trimmed == "" {
		out, err := yaml.Marshal(section)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(trimmed), &doc); err != nil {
		out, err := yaml.Marshal(section)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	target := unwrapMihomoRawDNSNode(&doc)
	if target == nil || target.Kind != yaml.MappingNode {
		out, err := yaml.Marshal(section)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	setField := func(key string, val any) {
		var valNode yaml.Node
		if err := valNode.Encode(val); err != nil {
			return
		}
		for i := 0; i < len(target.Content); i += 2 {
			if target.Content[i].Value == key {
				target.Content[i+1] = &valNode
				return
			}
		}
		var keyNode yaml.Node
		keyNode.Encode(key)
		target.Content = append(target.Content, &keyNode, &valNode)
	}

	removeField := func(key string) {
		for i := 0; i < len(target.Content); i += 2 {
			if target.Content[i].Value == key {
				target.Content = append(target.Content[:i], target.Content[i+2:]...)
				return
			}
		}
	}

	setField("enable", section.Enable)
	setField("ipv6", section.IPv6)
	setField("enhanced-mode", section.EnhancedMode)
	if section.EnhancedMode == "fake-ip" {
		setField("fake-ip-range", section.FakeIPRange)
		setField("fake-ip-filter-mode", section.FakeIPFilterMode)
		if len(section.FakeIPFilter) > 0 {
			setField("fake-ip-filter", section.FakeIPFilter)
		} else {
			removeField("fake-ip-filter")
		}
	} else {
		removeField("fake-ip-range")
		removeField("fake-ip-filter-mode")
		removeField("fake-ip-filter")
	}
	setField("respect-rules", section.RespectRules)
	setField("default-nameserver", section.DefaultNameserver)
	setField("nameserver", section.Nameserver)
	if len(section.ProxyServerNameserver) > 0 {
		setField("proxy-server-nameserver", section.ProxyServerNameserver)
	} else {
		removeField("proxy-server-nameserver")
	}
	if len(section.DirectNameserver) > 0 {
		setField("direct-nameserver", section.DirectNameserver)
	} else {
		removeField("direct-nameserver")
	}
	setField("direct-nameserver-follow-policy", section.DirectNameserverFollowPolicy)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		out, err := yaml.Marshal(section)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	return strings.TrimSpace(string(out)), nil
}

func parseMihomoClientRawDNS(raw string) (MihomoClientDNS, error) {
	if raw == "" {
		res := defaultMihomoClientDNS()
		res.Enable = false
		return res, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &document); err != nil {
		return MihomoClientDNS{}, err
	}
	target := unwrapMihomoRawDNSNode(&document)
	if target == nil || target.Kind != yaml.MappingNode {
		return MihomoClientDNS{}, errors.New("DNS text must be a YAML mapping of dns options")
	}

	var intermediate struct {
		Enable                            *bool    `yaml:"enable"`
		IPv6                              *bool    `yaml:"ipv6"`
		IPv6Hyphen                        *bool    `yaml:"ipv-6"`
		EnhancedMode                      string   `yaml:"enhanced-mode"`
		EnhancedModeSnake                 string   `yaml:"enhanced_mode"`
		FakeIPRange                       string   `yaml:"fake-ip-range"`
		FakeIPRangeSnake                  string   `yaml:"fake_ip_range"`
		FakeIPFilterMode                  string   `yaml:"fake-ip-filter-mode"`
		FakeIPFilterModeSnake             string   `yaml:"fake_ip_filter_mode"`
		FakeIPFilter                      []string `yaml:"fake-ip-filter"`
		FakeIPFilterSnake                 []string `yaml:"fake_ip_filter"`
		RespectRules                      *bool    `yaml:"respect-rules"`
		RespectRulesSnake                 *bool    `yaml:"respect_rules"`
		DefaultNameserver                 []string `yaml:"default-nameserver"`
		DefaultNameserverSnake            []string `yaml:"default_nameserver"`
		Nameserver                        []string `yaml:"nameserver"`
		ProxyServerNameserver             []string `yaml:"proxy-server-nameserver"`
		ProxyServerNameserverSnake        []string `yaml:"proxy_server_nameserver"`
		DirectNameserver                  []string `yaml:"direct-nameserver"`
		DirectNameserverSnake             []string `yaml:"direct_nameserver"`
		DirectNameserverFollowPolicy      *bool    `yaml:"direct-nameserver-follow-policy"`
		DirectNameserverFollowPolicySnake *bool    `yaml:"direct_nameserver_follow_policy"`
	}

	if err := target.Decode(&intermediate); err != nil {
		return MihomoClientDNS{}, err
	}

	res := defaultMihomoClientDNS()
	if intermediate.Enable != nil {
		res.Enable = *intermediate.Enable
	} else {
		res.Enable = true
	}
	if intermediate.IPv6 != nil {
		res.IPv6 = *intermediate.IPv6
	} else if intermediate.IPv6Hyphen != nil {
		res.IPv6 = *intermediate.IPv6Hyphen
	} else {
		res.IPv6 = false
	}
	mode := intermediate.EnhancedMode
	if mode == "" {
		mode = intermediate.EnhancedModeSnake
	}
	if mode != "" {
		res.EnhancedMode = mode
	}

	fakeRange := intermediate.FakeIPRange
	if fakeRange == "" {
		fakeRange = intermediate.FakeIPRangeSnake
	}
	if fakeRange != "" {
		res.FakeIPRange = fakeRange
	}

	filterMode := intermediate.FakeIPFilterMode
	if filterMode == "" {
		filterMode = intermediate.FakeIPFilterModeSnake
	}
	if filterMode != "" {
		res.FakeIPFilterMode = filterMode
	}

	if intermediate.FakeIPFilter != nil {
		res.FakeIPFilter = intermediate.FakeIPFilter
	} else if intermediate.FakeIPFilterSnake != nil {
		res.FakeIPFilter = intermediate.FakeIPFilterSnake
	}

	if intermediate.RespectRules != nil {
		res.RespectRules = *intermediate.RespectRules
	} else if intermediate.RespectRulesSnake != nil {
		res.RespectRules = *intermediate.RespectRulesSnake
	}

	if intermediate.DefaultNameserver != nil {
		res.DefaultNameserver = intermediate.DefaultNameserver
	} else if intermediate.DefaultNameserverSnake != nil {
		res.DefaultNameserver = intermediate.DefaultNameserverSnake
	}

	if intermediate.Nameserver != nil {
		res.Nameserver = intermediate.Nameserver
	}

	if intermediate.ProxyServerNameserver != nil {
		res.ProxyServerNameserver = intermediate.ProxyServerNameserver
	} else if intermediate.ProxyServerNameserverSnake != nil {
		res.ProxyServerNameserver = intermediate.ProxyServerNameserverSnake
	}

	if intermediate.DirectNameserver != nil {
		res.DirectNameserver = intermediate.DirectNameserver
	} else if intermediate.DirectNameserverSnake != nil {
		res.DirectNameserver = intermediate.DirectNameserverSnake
	}

	if intermediate.DirectNameserverFollowPolicy != nil {
		res.DirectNameserverFollowPolicy = *intermediate.DirectNameserverFollowPolicy
	} else if intermediate.DirectNameserverFollowPolicySnake != nil {
		res.DirectNameserverFollowPolicy = *intermediate.DirectNameserverFollowPolicySnake
	}

	return res, nil
}

// validateMihomoClientRawDNS only checks what can be judged without running
// Mihomo: that the text is a YAML mapping, plus the two conditions Mihomo
// refuses to start on.
func validateMihomoClientRawDNS(raw string) error {
	if raw == "" {
		return nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &document); err != nil {
		return fmt.Errorf("DNS text is not valid YAML: %w", err)
	}
	target := unwrapMihomoRawDNSNode(&document)
	if target == nil || target.Kind != yaml.MappingNode {
		return errors.New("DNS text must be a YAML mapping of dns options")
	}
	var section struct {
		Enable                bool     `yaml:"enable"`
		Nameserver            []string `yaml:"nameserver"`
		RespectRules          bool     `yaml:"respect-rules"`
		ProxyServerNameserver []string `yaml:"proxy-server-nameserver"`
	}
	if err := target.Decode(&section); err != nil {
		return fmt.Errorf("DNS text is not a valid dns section: %w", err)
	}
	if section.Enable && len(section.Nameserver) == 0 {
		return errors.New("nameserver is required when the DNS section is enabled")
	}
	if section.RespectRules && len(section.ProxyServerNameserver) == 0 {
		return errors.New("proxy-server-nameserver is required when DNS follows the routing rules")
	}
	return nil
}

// mihomoClientDNSNode renders the configured section, or nil when nothing
// should be written and the client is left to its own DNS settings.
// DNS configuration is a single source of truth: if RawDNS has content, it is used
// directly to preserve all options (including fallback, nameserver-policy);
// otherwise config.DNS is encoded. Mode distinction is eliminated from output generation.
func mihomoClientDNSNode(config MihomoClientConfig) (*yaml.Node, error) {
	if config.RawDNS != "" {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(config.RawDNS), &node); err != nil {
			return nil, fmt.Errorf("encode DNS section: %w", err)
		}
		target := unwrapMihomoRawDNSNode(&node)
		if target == nil {
			return nil, nil
		}
		var section struct {
			Enable *bool `yaml:"enable"`
		}
		if err := target.Decode(&section); err == nil && section.Enable != nil && !*section.Enable {
			return nil, nil
		}
		return target, nil
	}
	if !config.DNS.Enable {
		return nil, nil
	}
	var node yaml.Node
	section := mihomoYAMLDNS{
		Enable:                       true,
		IPv6:                         config.DNS.IPv6,
		EnhancedMode:                 config.DNS.EnhancedMode,
		FakeIPRange:                  config.DNS.FakeIPRange,
		FakeIPFilterMode:             config.DNS.FakeIPFilterMode,
		FakeIPFilter:                 config.DNS.FakeIPFilter,
		RespectRules:                 config.DNS.RespectRules,
		DefaultNameserver:            config.DNS.DefaultNameserver,
		Nameserver:                   config.DNS.Nameserver,
		ProxyServerNameserver:        config.DNS.ProxyServerNameserver,
		DirectNameserver:             config.DNS.DirectNameserver,
		DirectNameserverFollowPolicy: config.DNS.DirectNameserverFollowPolicy,
	}
	if err := node.Encode(section); err != nil {
		return nil, fmt.Errorf("encode DNS section: %w", err)
	}
	return &node, nil
}
