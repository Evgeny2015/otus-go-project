// Package config provides configuration loading, merging, and validation
// for the system monitoring daemon. It supports:
//   - YAML configuration files (parsed with a minimal built-in parser)
//   - Command-line flag overrides
//   - Environment variable overrides
//   - Configuration validation with sensible defaults
//
// The YAML parser implemented here handles the subset of YAML used by
// this project's config files. It does NOT support the full YAML spec.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config represents the complete daemon configuration.
// It is populated from YAML config file, then overridden by CLI flags.
type Config struct {
	// Monitoring contains metric collection and aggregation settings.
	Monitoring MonitoringConfig `yaml:"monitoring"`

	// Server contains gRPC server settings.
	Server ServerConfig `yaml:"server"`
}

// ---------------------------------------------------------------------------
// MonitoringConfig
// ---------------------------------------------------------------------------

// MonitoringConfig controls which collectors are active and the default
// collection interval and aggregation window.
type MonitoringConfig struct {
	// EnabledCollectors lists the names of collectors that should run.
	// Valid names: cpu, disk, network, filesystem, load, toptalkers.
	EnabledCollectors []string `yaml:"enabled_collectors"`

	// DefaultInterval is the default N value (seconds) between metric
	// broadcasts to clients. Clients may override this in their request.
	DefaultInterval int `yaml:"default_interval"`

	// DefaultWindow is the default M value (seconds) for the sliding
	// window aggregation. Clients may override this in their request.
	DefaultWindow int `yaml:"default_window"`
}

// ---------------------------------------------------------------------------
// ServerConfig
// ---------------------------------------------------------------------------

// ServerConfig controls the gRPC server behaviour.
type ServerConfig struct {
	// GRPCPort is the TCP port for the gRPC server.
	GRPCPort int `yaml:"grpc_port"`

	// MaxClients is the maximum number of concurrent gRPC clients.
	// 0 means unlimited.
	MaxClients int `yaml:"max_clients"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Monitoring: MonitoringConfig{
			EnabledCollectors: []string{
				"cpu",
				"disk",
				"network",
				"filesystem",
				"load",
			},
			DefaultInterval: 5,
			DefaultWindow:   15,
		},
		Server: ServerConfig{
			GRPCPort:   50051,
			MaxClients: 100,
		},
	}
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// Load reads configuration from the given YAML file path and returns a
// Config. If the file does not exist, it returns default configuration.
// If the file exists but cannot be parsed, it returns an error.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file is optional; return defaults.
			return cfg, nil
		}
		return nil, fmt.Errorf("config: cannot read %q: %w", path, err)
	}

	if err := parseYAML(string(data), cfg); err != nil {
		return nil, fmt.Errorf("config: cannot parse %q: %w", path, err)
	}

	return cfg, nil
}

// ---------------------------------------------------------------------------
// Flag binding
// ---------------------------------------------------------------------------

// RegisterFlags binds Config fields to pflag flags so they can be
// overridden from the command line.
func RegisterFlags(cfg *Config, fs *pflag.FlagSet) {
	if fs == nil {
		fs = pflag.CommandLine
	}

	// Server flags
	fs.IntVar(&cfg.Server.GRPCPort, "port", cfg.Server.GRPCPort,
		"gRPC server port")
	fs.IntVar(&cfg.Server.MaxClients, "max-clients", cfg.Server.MaxClients,
		"maximum number of concurrent clients (0 = unlimited)")

	// Monitoring flags
	fs.StringSliceVar(&cfg.Monitoring.EnabledCollectors, "collectors",
		cfg.Monitoring.EnabledCollectors,
		"comma-separated list of enabled collectors "+
			"(cpu,disk,network,filesystem,load,toptalkers)")
	fs.IntVar(&cfg.Monitoring.DefaultInterval, "interval",
		cfg.Monitoring.DefaultInterval,
		"default collection interval in seconds (N)")
	fs.IntVar(&cfg.Monitoring.DefaultWindow, "window",
		cfg.Monitoring.DefaultWindow,
		"default aggregation window in seconds (M)")

	// Config file path (not bound to Config struct directly)
	fs.String("config", "", "path to YAML configuration file")
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Validate checks the configuration for invalid or contradictory values.
// It returns a list of warnings (non-fatal) and errors (fatal).
func Validate(cfg *Config) (warnings []string, errors []string) {
	// --- Server validation ---
	if cfg.Server.GRPCPort < 1 || cfg.Server.GRPCPort > 65535 {
		errors = append(errors,
			fmt.Sprintf("server.grpc_port %d is out of valid range [1, 65535]",
				cfg.Server.GRPCPort))
	}
	if cfg.Server.MaxClients < 0 {
		errors = append(errors,
			fmt.Sprintf("server.max_clients %d must be >= 0", cfg.Server.MaxClients))
	}

	// --- Monitoring validation ---
	if cfg.Monitoring.DefaultInterval < 1 {
		errors = append(errors,
			fmt.Sprintf("monitoring.default_interval %d must be >= 1",
				cfg.Monitoring.DefaultInterval))
	}
	if cfg.Monitoring.DefaultWindow < 1 {
		errors = append(errors,
			fmt.Sprintf("monitoring.default_window %d must be >= 1",
				cfg.Monitoring.DefaultWindow))
	}
	if cfg.Monitoring.DefaultWindow < cfg.Monitoring.DefaultInterval {
		warnings = append(warnings,
			"monitoring.default_window is smaller than default_interval; "+
				"aggregation may have insufficient data points")
	}

	// --- Collector validation ---
	validCollectors := map[string]bool{
		"cpu":        true,
		"disk":       true,
		"network":    true,
		"filesystem": true,
		"load":       true,
		"toptalkers": true,
	}

	seen := make(map[string]bool)
	for _, name := range cfg.Monitoring.EnabledCollectors {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !validCollectors[name] {
			warnings = append(warnings,
				fmt.Sprintf("monitoring.enabled_collectors: %q is not a known collector", name))
		}
		if seen[name] {
			warnings = append(warnings,
				fmt.Sprintf("monitoring.enabled_collectors: duplicate entry %q", name))
		}
		seen[name] = true
	}

	if len(cfg.Monitoring.EnabledCollectors) == 0 {
		warnings = append(warnings,
			"monitoring.enabled_collectors is empty; no metrics will be collected")
	}

	return warnings, errors
}

// ---------------------------------------------------------------------------
// Convenience helpers
// ---------------------------------------------------------------------------

// GetConfigFilePath returns the config file path from the flag set.
// It looks for the "config" flag registered by RegisterFlags.
func GetConfigFilePath(fs *pflag.FlagSet) string {
	if fs == nil {
		fs = pflag.CommandLine
	}
	path, err := fs.GetString("config")
	if err != nil {
		return ""
	}
	return path
}

// CollectorEnabled checks whether a named collector is in the enabled list.
func (m *MonitoringConfig) CollectorEnabled(name string) bool {
	for _, c := range m.EnabledCollectors {
		if strings.EqualFold(c, name) {
			return true
		}
	}
	return false
}

// String returns a human-readable summary of the configuration.
func (c *Config) String() string {
	var b strings.Builder
	b.WriteString("Config:\n")
	b.WriteString(fmt.Sprintf("  Config file: %s\n", GetConfigFilePath(pflag.CommandLine)))
	b.WriteString(fmt.Sprintf("  Server port: %d\n", c.Server.GRPCPort))
	b.WriteString(fmt.Sprintf("  Server max clients: %d\n", c.Server.MaxClients))
	b.WriteString(fmt.Sprintf("  Collectors: %s\n", strings.Join(c.Monitoring.EnabledCollectors, ", ")))
	b.WriteString(fmt.Sprintf("  Default interval: %ds\n", c.Monitoring.DefaultInterval))
	b.WriteString(fmt.Sprintf("  Default window: %ds\n", c.Monitoring.DefaultWindow))
	return b.String()
}

// ---------------------------------------------------------------------------
// Environment variable overrides
// ---------------------------------------------------------------------------

// OverrideFromEnv checks well-known environment variables and overrides
// the corresponding config fields. This is useful for containerised
// deployments where flags are inconvenient.
func OverrideFromEnv(cfg *Config) {
	envBindings := []struct {
		envVar string
		setter func(val string)
	}{
		{"MON_GRPC_PORT", func(val string) {
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Server.GRPCPort = v
			}
		}},
		{"MON_MAX_CLIENTS", func(val string) {
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Server.MaxClients = v
			}
		}},
		{"MON_INTERVAL", func(val string) {
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Monitoring.DefaultInterval = v
			}
		}},
		{"MON_WINDOW", func(val string) {
			if v, err := strconv.Atoi(val); err == nil {
				cfg.Monitoring.DefaultWindow = v
			}
		}},
		{"MON_COLLECTORS", func(val string) {
			if val != "" {
				cfg.Monitoring.EnabledCollectors = strings.Split(val, ",")
			}
		}},
	}

	for _, b := range envBindings {
		if val, ok := os.LookupEnv(b.envVar); ok {
			b.setter(val)
		}
	}
}

// ---------------------------------------------------------------------------
// Minimal YAML parser
// ---------------------------------------------------------------------------
//
// This parser handles the subset of YAML used by this project's config
// files. It supports:
//   - Scalar key: value pairs (strings, integers, booleans)
//   - Nested mappings via indentation
//   - Sequences (list items with leading "- ")
//   - Comments starting with "#"
//
// It does NOT support:
//   - Multi-line strings, flow style, anchors/aliases, etc.

// parseYAML parses a YAML string into the given Config struct.
func parseYAML(input string, cfg *Config) error {
	lines := strings.Split(input, "\n")

	// State machine: we track the current section path to know which
	// struct field to populate.
	type section struct {
		name  string   // e.g. "monitoring", "server"
		depth int      // indentation depth of this section
		keys  []string // ordered keys within this section for sequences
	}

	var stack []section

	for _, raw := range lines {
		line := stripComment(raw)
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			continue
		}

		indent := countIndent(line)
		content := strings.TrimLeft(line, " \t")

		// Check if this line opens a new section (key: with no value, or key: with nested content)
		if strings.HasSuffix(content, ":") && !strings.HasPrefix(content, "- ") {
			sectionName := strings.TrimSuffix(content, ":")
			sectionName = strings.TrimSpace(sectionName)

			// Pop stack back to correct depth
			for len(stack) > 0 && indent <= stack[len(stack)-1].depth {
				stack = stack[:len(stack)-1]
			}

			stack = append(stack, section{name: sectionName, depth: indent})
			continue
		}

		// Check if this is a list item
		if strings.HasPrefix(content, "- ") {
			listValue := strings.TrimPrefix(content, "- ")
			listValue = strings.TrimSpace(listValue)
			listValue = strings.Trim(listValue, "\"'")

			// Find the current section
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				top.keys = append(top.keys, listValue)
			}
			continue
		}

		// Key: value pair
		colonIdx := strings.Index(content, ":")
		if colonIdx < 0 {
			continue
		}

		key := strings.TrimSpace(content[:colonIdx])
		val := strings.TrimSpace(content[colonIdx+1:])
		val = strings.Trim(val, "\"'")

		// Determine which section we're in
		sectionPath := ""
		for _, s := range stack {
			if sectionPath != "" {
				sectionPath += "."
			}
			sectionPath += s.name
		}

		// Pop stack if indentation decreased
		for len(stack) > 0 && indent <= stack[len(stack)-1].depth {
			stack = stack[:len(stack)-1]
		}

		// Update the config based on section path + key
		if err := setField(cfg, sectionPath, key, val); err != nil {
			return err
		}
	}

	// After parsing all lines, propagate collected list values
	// (sequences) into the config.
	for _, s := range stack {
		if len(s.keys) > 0 {
			switch s.name {
			case "enabled_collectors":
				cfg.Monitoring.EnabledCollectors = append([]string{}, s.keys...)
			}
		}
	}

	return nil
}

// setField sets a field in the Config struct based on a dotted path.
func setField(cfg *Config, section, key, value string) error {
	path := section
	if path != "" {
		path += "."
	}
	path += key

	switch path {
	// Server section
	case "server.grpc_port":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("server.grpc_port: invalid integer %q", value)
		}
		cfg.Server.GRPCPort = v
	case "server.max_clients":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("server.max_clients: invalid integer %q", value)
		}
		cfg.Server.MaxClients = v

	// Monitoring section
	case "monitoring.default_interval":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("monitoring.default_interval: invalid integer %q", value)
		}
		cfg.Monitoring.DefaultInterval = v
	case "monitoring.default_window":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("monitoring.default_window: invalid integer %q", value)
		}
		cfg.Monitoring.DefaultWindow = v

	// Unknown field — not an error, just skip (allows future config additions)
	default:
		// Silently ignore unknown keys for forward compatibility.
	}

	return nil
}

// stripComment removes YAML comments (everything from # to end of line),
// respecting quoted strings.
func stripComment(line string) string {
	inQuote := false
	for i := 0; i < len(line); i++ {
		if line[i] == '\'' || line[i] == '"' {
			inQuote = !inQuote
		}
		if line[i] == '#' && !inQuote {
			return line[:i]
		}
	}
	return line
}

// countIndent returns the number of leading spaces/tabs in a line.
func countIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			count++
		} else {
			break
		}
	}
	return count
}
