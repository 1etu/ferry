package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

var (
	ErrUnsupported = errors.New("unsupported on this platform")
	ErrCanceled    = errors.New("canceled by the user")
)

const appDirName = "Ferry"

const SendToLinkName = "Send to iPhone.lnk"

type RegistryRoot string

const UserRegistryRoot RegistryRoot = `Software\Microsoft\Windows\CurrentVersion`

type UninstallEntry struct {
	DisplayName     string
	DisplayVersion  string
	DisplayIcon     string
	Publisher       string
	InstallLocation string
	UninstallString string
	EstimatedSizeKB uint32
}

func RunAtLogin() (bool, error) {
	return UserRegistryRoot.RunAtLogin()
}

func SetRunAtLogin(exePath string, enabled bool) error {
	return UserRegistryRoot.SetRunAtLogin(exePath, enabled)
}

func WriteUninstallEntry(e UninstallEntry) error {
	return UserRegistryRoot.WriteUninstallEntry(e)
}

func RemoveUninstallEntry() error {
	return UserRegistryRoot.RemoveUninstallEntry()
}

func Hostname() (string, error) {
	name, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("read hostname: %w", err)
	}
	label := normalizeHostname(name)
	if label == "" {
		return "", fmt.Errorf("hostname %q has no usable label", name)
	}
	return label, nil
}

func normalizeHostname(name string) string {
	label, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(name)), ".")
	return label
}

func isWebView2Version(pv string) bool {
	pv = strings.TrimSpace(pv)
	return pv != "" && pv != "0.0.0.0"
}

type NetworkProfile string

const (
	ProfilePrivate NetworkProfile = "private"
	ProfilePublic  NetworkProfile = "public"
	ProfileUnknown NetworkProfile = "unknown"
)

type Firewall struct {
	Profile NetworkProfile
	Allowed bool
}

type scriptRunner func(ctx context.Context, script string, env ...string) ([]byte, error)

const firewallScript = `$ErrorActionPreference = 'Stop'
$tab = [string][char]9
foreach ($connection in @(Get-NetConnectionProfile -ErrorAction SilentlyContinue)) { 'category' + $tab + $connection.NetworkCategory }
$filters = @(Get-NetFirewallApplicationFilter -Program $env:FERRY_FIREWALL_PROGRAM -ErrorAction SilentlyContinue)
if ($filters.Count -gt 0) {
	foreach ($rule in @($filters | Get-NetFirewallRule)) { @('rule', $rule.Direction, $rule.Action, $rule.Enabled, $rule.Profile) -join $tab }
}
`

const anyFirewallProfile = "any"

type firewallRule struct {
	isInboundAllow bool
	isInboundBlock bool
	profiles       map[string]bool
}

func checkFirewall(ctx context.Context, exePath string, run scriptRunner) (Firewall, error) {
	out, err := run(ctx, firewallScript, "FERRY_FIREWALL_PROGRAM="+exePath)
	if err != nil {
		return Firewall{Profile: ProfileUnknown}, fmt.Errorf("query firewall for %s: %w", exePath, err)
	}
	return parseFirewall(string(out))
}

func parseFirewall(out string) (Firewall, error) {
	var categories []string
	var rules []firewallRule
	for line := range strings.Lines(out) {
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		fields := strings.Split(trimmed, "\t")
		switch {
		case fields[0] == "category" && len(fields) == 2:
			categories = append(categories, firewallProfileOf(fields[1]))
		case fields[0] == "rule" && len(fields) == 5:
			rules = append(rules, parseFirewallRule(fields[1:]))
		default:
			return Firewall{Profile: ProfileUnknown}, fmt.Errorf("parse firewall line %q", line)
		}
	}
	return Firewall{Profile: networkProfile(categories), Allowed: isAllowed(categories, rules)}, nil
}

func parseFirewallRule(fields []string) firewallRule {
	direction, action, enabled, profileList := fields[0], fields[1], fields[2], fields[3]
	isEnabledInbound := strings.EqualFold(direction, "Inbound") && strings.EqualFold(enabled, "True")
	profiles := make(map[string]bool)
	for profile := range strings.SplitSeq(profileList, ",") {
		profiles[strings.ToLower(strings.TrimSpace(profile))] = true
	}
	return firewallRule{
		isInboundAllow: isEnabledInbound && strings.EqualFold(action, "Allow"),
		isInboundBlock: isEnabledInbound && strings.EqualFold(action, "Block"),
		profiles:       profiles,
	}
}

func firewallProfileOf(category string) string {
	if strings.EqualFold(category, "DomainAuthenticated") {
		return "domain"
	}
	return strings.ToLower(strings.TrimSpace(category))
}

func (r firewallRule) allows(profile string) bool {
	return r.isInboundAllow && r.covers(profile)
}

func (r firewallRule) blocks(profile string) bool {
	return r.isInboundBlock && r.covers(profile)
}

func (r firewallRule) covers(profile string) bool {
	return r.profiles[anyFirewallProfile] || r.profiles[profile]
}

func isAllowed(categories []string, rules []firewallRule) bool {
	if !slices.ContainsFunc(rules, func(r firewallRule) bool { return r.isInboundAllow }) {
		return false
	}
	for _, category := range categories {
		if !anyRule(rules, category, firewallRule.allows) || anyRule(rules, category, firewallRule.blocks) {
			return false
		}
	}
	return true
}

func anyRule(rules []firewallRule, profile string, matches func(firewallRule, string) bool) bool {
	return slices.ContainsFunc(rules, func(r firewallRule) bool { return matches(r, profile) })
}

func networkProfile(categories []string) NetworkProfile {
	profile := ProfileUnknown
	for _, category := range categories {
		switch category {
		case "public":
			return ProfilePublic
		case "private":
			profile = ProfilePrivate
		}
	}
	return profile
}

func firewallRuleArgs(exePath string) string {
	return `advfirewall firewall add rule name="Ferry" dir=in action=allow program="` + exePath + `" enable=yes profile=private,public`
}
