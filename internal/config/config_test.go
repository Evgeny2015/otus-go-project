package config

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// Tests: DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Check defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.Server.GRPCPort)
	}
	if cfg.Server.MaxClients != 100 {
		t.Errorf("MaxClients = %d, want 100", cfg.Server.MaxClients)
	}
	if cfg.Monitoring.DefaultInterval != 5 {
		t.Errorf("DefaultInterval = %d, want 5", cfg.Monitoring.DefaultInterval)
	}
	if cfg.Monitoring.DefaultWindow != 15 {
		t.Errorf("DefaultWindow = %d, want 15", cfg.Monitoring.DefaultWindow)
	}

	expectedCollectors := []string{"cpu", "disk", "network", "filesystem", "load"}
	if len(cfg.Monitoring.EnabledCollectors) != len(expectedCollectors) {
		t.Errorf("EnabledCollectors = %v, want %v", cfg.Monitoring.EnabledCollectors, expectedCollectors)
	}
	for i, c := range cfg.Monitoring.EnabledCollectors {
		if c != expectedCollectors[i] {
			t.Errorf("EnabledCollectors[%d] = %s, want %s", i, c, expectedCollectors[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Load
// ---------------------------------------------------------------------------

func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("Load(\"\") returned nil")
	}
	// Should return defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.Server.GRPCPort)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent_config_file_12345.yaml")
	if err != nil {
		t.Fatalf("Load(nonexistent) error = %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("Load(nonexistent) returned nil")
	}
	// Should return defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.Server.GRPCPort)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	yaml := `
server:
  grpc_port: 9090
  max_clients: 50

monitoring:
  default_interval: 10
  default_window: 30
  enabled_collectors:
    - cpu
    - load
    - disk
`
	tmpFile, err := os.CreateTemp("", "config_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("GRPCPort = %d, want 9090", cfg.Server.GRPCPort)
	}
	if cfg.Server.MaxClients != 50 {
		t.Errorf("MaxClients = %d, want 50", cfg.Server.MaxClients)
	}
	if cfg.Monitoring.DefaultInterval != 10 {
		t.Errorf("DefaultInterval = %d, want 10", cfg.Monitoring.DefaultInterval)
	}
	if cfg.Monitoring.DefaultWindow != 30 {
		t.Errorf("DefaultWindow = %d, want 30", cfg.Monitoring.DefaultWindow)
	}
	if len(cfg.Monitoring.EnabledCollectors) != 3 {
		t.Errorf("EnabledCollectors = %v, want [cpu load disk]", cfg.Monitoring.EnabledCollectors)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	yaml := `
server:
  grpc_port: not_an_integer
`
	tmpFile, err := os.CreateTemp("", "config_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Fatal("Load() expected error for invalid YAML, got nil")
	}
}

func TestLoad_PartialYAML(t *testing.T) {
	// Only override some fields, others should remain defaults
	yaml := `
server:
  grpc_port: 9090
`
	tmpFile, err := os.CreateTemp("", "config_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("GRPCPort = %d, want 9090", cfg.Server.GRPCPort)
	}
	// Should remain default
	if cfg.Monitoring.DefaultInterval != 5 {
		t.Errorf("DefaultInterval = %d, want 5 (default)", cfg.Monitoring.DefaultInterval)
	}
}

// ---------------------------------------------------------------------------
// Tests: Validate
// ---------------------------------------------------------------------------

func TestValidate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	warnings, errors := Validate(cfg)

	if len(errors) != 0 {
		t.Errorf("errors = %v, want empty", errors)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want empty", warnings)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.GRPCPort = 0

	_, errors := Validate(cfg)
	if len(errors) == 0 {
		t.Fatal("expected error for port 0, got none")
	}
	if !strings.Contains(errors[0], "grpc_port") {
		t.Errorf("error = %q, want grpc_port error", errors[0])
	}
}

func TestValidate_PortTooHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.GRPCPort = 70000

	_, errors := Validate(cfg)
	if len(errors) == 0 {
		t.Fatal("expected error for port 70000, got none")
	}
}

func TestValidate_NegativeMaxClients(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.MaxClients = -1

	_, errors := Validate(cfg)
	if len(errors) == 0 {
		t.Fatal("expected error for negative MaxClients, got none")
	}
}

func TestValidate_InvalidInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.DefaultInterval = 0

	_, errors := Validate(cfg)
	if len(errors) == 0 {
		t.Fatal("expected error for interval 0, got none")
	}
}

func TestValidate_InvalidWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.DefaultWindow = 0

	_, errors := Validate(cfg)
	if len(errors) == 0 {
		t.Fatal("expected error for window 0, got none")
	}
}

func TestValidate_WindowSmallerThanInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.DefaultInterval = 30
	cfg.Monitoring.DefaultWindow = 10

	warnings, errors := Validate(cfg)
	if len(errors) != 0 {
		t.Errorf("errors = %v, want empty", errors)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for window < interval, got none")
	}
	if !strings.Contains(warnings[0], "default_window") {
		t.Errorf("warning = %q, want default_window warning", warnings[0])
	}
}

func TestValidate_UnknownCollector(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"cpu", "unknown_collector"}

	warnings, errors := Validate(cfg)
	if len(errors) != 0 {
		t.Errorf("errors = %v, want empty", errors)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for unknown collector, got none")
	}
}

func TestValidate_DuplicateCollector(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{"cpu", "cpu"}

	warnings, errors := Validate(cfg)
	if len(errors) != 0 {
		t.Errorf("errors = %v, want empty", errors)
	}
	hasDuplicateWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "duplicate") {
			hasDuplicateWarning = true
		}
	}
	if !hasDuplicateWarning {
		t.Errorf("warnings = %v, want duplicate entry warning", warnings)
	}
}

func TestValidate_EmptyCollectors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitoring.EnabledCollectors = []string{}

	warnings, errors := Validate(cfg)
	if len(errors) != 0 {
		t.Errorf("errors = %v, want empty", errors)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for empty collectors, got none")
	}
}

// ---------------------------------------------------------------------------
// Tests: CollectorEnabled
// ---------------------------------------------------------------------------

func TestCollectorEnabled(t *testing.T) {
	mc := MonitoringConfig{
		EnabledCollectors: []string{"cpu", "disk", "network"},
	}

	tests := []struct {
		name    string
		enabled bool
	}{
		{"cpu", true},
		{"disk", true},
		{"network", true},
		{"load", false},
		{"filesystem", false},
		{"toptalkers", false},
		{"CPU", true},  // case insensitive
		{"DISK", true}, // case insensitive
		{"UNKNOWN", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mc.CollectorEnabled(tt.name)
			if got != tt.enabled {
				t.Errorf("CollectorEnabled(%q) = %v, want %v", tt.name, got, tt.enabled)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: RegisterFlags
// ---------------------------------------------------------------------------

func TestRegisterFlags(t *testing.T) {
	cfg := DefaultConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	RegisterFlags(cfg, fs)

	// Verify flags are registered
	portFlag := fs.Lookup("port")
	if portFlag == nil {
		t.Fatal("port flag not registered")
	}
	if portFlag.DefValue != "50051" {
		t.Errorf("port default = %s, want 50051", portFlag.DefValue)
	}

	maxClientsFlag := fs.Lookup("max-clients")
	if maxClientsFlag == nil {
		t.Fatal("max-clients flag not registered")
	}

	collectorsFlag := fs.Lookup("collectors")
	if collectorsFlag == nil {
		t.Fatal("collectors flag not registered")
	}

	intervalFlag := fs.Lookup("interval")
	if intervalFlag == nil {
		t.Fatal("interval flag not registered")
	}

	windowFlag := fs.Lookup("window")
	if windowFlag == nil {
		t.Fatal("window flag not registered")
	}

	configFlag := fs.Lookup("config")
	if configFlag == nil {
		t.Fatal("config flag not registered")
	}
}

func TestRegisterFlags_NilFlagSet(t *testing.T) {
	cfg := DefaultConfig()

	// Should use pflag.CommandLine and not panic
	RegisterFlags(cfg, nil)
}

func TestRegisterFlags_OverrideValues(t *testing.T) {
	cfg := DefaultConfig()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)

	RegisterFlags(cfg, fs)

	// Parse flags to override values
	err := fs.Parse([]string{
		"--port", "9090",
		"--max-clients", "50",
		"--interval", "10",
		"--window", "30",
		"--collectors", "cpu,load,disk",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("GRPCPort = %d, want 9090", cfg.Server.GRPCPort)
	}
	if cfg.Server.MaxClients != 50 {
		t.Errorf("MaxClients = %d, want 50", cfg.Server.MaxClients)
	}
	if cfg.Monitoring.DefaultInterval != 10 {
		t.Errorf("DefaultInterval = %d, want 10", cfg.Monitoring.DefaultInterval)
	}
	if cfg.Monitoring.DefaultWindow != 30 {
		t.Errorf("DefaultWindow = %d, want 30", cfg.Monitoring.DefaultWindow)
	}
}

// ---------------------------------------------------------------------------
// Tests: GetConfigFilePath
// ---------------------------------------------------------------------------

func TestGetConfigFilePath(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("config", "/path/to/config.yaml", "config file path")

	path := GetConfigFilePath(fs)
	if path != "/path/to/config.yaml" {
		t.Errorf("GetConfigFilePath() = %s, want /path/to/config.yaml", path)
	}
}

func TestGetConfigFilePath_Default(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("config", "", "config file path")

	path := GetConfigFilePath(fs)
	if path != "" {
		t.Errorf("GetConfigFilePath() = %s, want empty", path)
	}
}

func TestGetConfigFilePath_NilFlagSet(t *testing.T) {
	path := GetConfigFilePath(nil)
	// Should not panic, returns empty string or whatever is in CommandLine
	_ = path
}

// ---------------------------------------------------------------------------
// Tests: OverrideFromEnv
// ---------------------------------------------------------------------------

func TestOverrideFromEnv(t *testing.T) {
	cfg := DefaultConfig()

	// Set environment variables
	os.Setenv("MON_GRPC_PORT", "8080")
	os.Setenv("MON_MAX_CLIENTS", "200")
	os.Setenv("MON_INTERVAL", "15")
	os.Setenv("MON_WINDOW", "45")
	os.Setenv("MON_COLLECTORS", "cpu,load,network")
	defer func() {
		os.Unsetenv("MON_GRPC_PORT")
		os.Unsetenv("MON_MAX_CLIENTS")
		os.Unsetenv("MON_INTERVAL")
		os.Unsetenv("MON_WINDOW")
		os.Unsetenv("MON_COLLECTORS")
	}()

	OverrideFromEnv(cfg)

	if cfg.Server.GRPCPort != 8080 {
		t.Errorf("GRPCPort = %d, want 8080", cfg.Server.GRPCPort)
	}
	if cfg.Server.MaxClients != 200 {
		t.Errorf("MaxClients = %d, want 200", cfg.Server.MaxClients)
	}
	if cfg.Monitoring.DefaultInterval != 15 {
		t.Errorf("DefaultInterval = %d, want 15", cfg.Monitoring.DefaultInterval)
	}
	if cfg.Monitoring.DefaultWindow != 45 {
		t.Errorf("DefaultWindow = %d, want 45", cfg.Monitoring.DefaultWindow)
	}
	if len(cfg.Monitoring.EnabledCollectors) != 3 {
		t.Errorf("EnabledCollectors = %v, want [cpu load network]", cfg.Monitoring.EnabledCollectors)
	}
}

func TestOverrideFromEnv_InvalidValues(t *testing.T) {
	cfg := DefaultConfig()

	// Set invalid values - should be ignored
	os.Setenv("MON_GRPC_PORT", "not_a_number")
	os.Setenv("MON_INTERVAL", "also_invalid")
	defer func() {
		os.Unsetenv("MON_GRPC_PORT")
		os.Unsetenv("MON_INTERVAL")
	}()

	OverrideFromEnv(cfg)

	// Should remain at defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051 (default)", cfg.Server.GRPCPort)
	}
	if cfg.Monitoring.DefaultInterval != 5 {
		t.Errorf("DefaultInterval = %d, want 5 (default)", cfg.Monitoring.DefaultInterval)
	}
}

func TestOverrideFromEnv_EmptyCollectors(t *testing.T) {
	cfg := DefaultConfig()

	os.Setenv("MON_COLLECTORS", "")
	defer os.Unsetenv("MON_COLLECTORS")

	OverrideFromEnv(cfg)

	// Empty string should not override collectors
	if len(cfg.Monitoring.EnabledCollectors) != 5 {
		t.Errorf("EnabledCollectors = %v, want defaults (5 collectors)", cfg.Monitoring.EnabledCollectors)
	}
}

func TestOverrideFromEnv_NoEnvVars(t *testing.T) {
	cfg := DefaultConfig()

	// Ensure no MON_* env vars are set
	// Just call and verify no panic and no changes
	OverrideFromEnv(cfg)

	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.Server.GRPCPort)
	}
}

// ---------------------------------------------------------------------------
// Tests: String
// ---------------------------------------------------------------------------

func TestConfig_String(t *testing.T) {
	cfg := DefaultConfig()
	s := cfg.String()

	if !strings.Contains(s, "50051") {
		t.Errorf("String() = %q, should contain 50051", s)
	}
	if !strings.Contains(s, "cpu") {
		t.Errorf("String() = %q, should contain cpu", s)
	}
	if !strings.Contains(s, "Config:") {
		t.Errorf("String() = %q, should start with Config:", s)
	}
}

// ---------------------------------------------------------------------------
// Tests: parseYAML
// ---------------------------------------------------------------------------

func TestParseYAML_Empty(t *testing.T) {
	cfg := DefaultConfig()
	err := parseYAML("", cfg)
	if err != nil {
		t.Errorf("parseYAML('') error = %v, want nil", err)
	}
}

func TestParseYAML_Comments(t *testing.T) {
	yaml := `
# This is a comment
server:
  # Server port comment
  grpc_port: 9090
  max_clients: 50
`
	cfg := DefaultConfig()
	err := parseYAML(yaml, cfg)
	if err != nil {
		t.Fatalf("parseYAML() error = %v, want nil", err)
	}
	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("GRPCPort = %d, want 9090", cfg.Server.GRPCPort)
	}
}

func TestParseYAML_OnlyComments(t *testing.T) {
	yaml := `
# just a comment
# another comment
`
	cfg := DefaultConfig()
	err := parseYAML(yaml, cfg)
	if err != nil {
		t.Errorf("parseYAML() error = %v, want nil", err)
	}
	// Should remain defaults
	if cfg.Server.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.Server.GRPCPort)
	}
}

func TestParseYAML_UnknownFields(t *testing.T) {
	yaml := `
server:
  grpc_port: 9090
  unknown_field: value
monitoring:
  future_option: true
`
	cfg := DefaultConfig()
	err := parseYAML(yaml, cfg)
	if err != nil {
		t.Fatalf("parseYAML() error = %v, want nil", err)
	}
	// Known field should be set, unknown fields silently ignored
	if cfg.Server.GRPCPort != 9090 {
		t.Errorf("GRPCPort = %d, want 9090", cfg.Server.GRPCPort)
	}
}

// ---------------------------------------------------------------------------
// Tests: stripComment
// ---------------------------------------------------------------------------

func TestStripComment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"key: value", "key: value"},
		{"key: value # comment", "key: value "},
		{"# full line comment", ""},
		{"key: 'value#notcomment'", "key: 'value#notcomment'"},
		{"key: \"value#notcomment\"", "key: \"value#notcomment\""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripComment(tt.input)
			if got != tt.want {
				t.Errorf("stripComment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: countIndent
// ---------------------------------------------------------------------------

func TestCountIndent(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"key: value", 0},
		{"  key: value", 2},
		{"    key: value", 4},
		{"\tkey: value", 1},
		{"  \tkey: value", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := countIndent(tt.input)
			if got != tt.want {
				t.Errorf("countIndent(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: setField
// ---------------------------------------------------------------------------

func TestSetField(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		section string
		key     string
		value   string
		wantErr bool
		check   func(*Config) bool
	}{
		{"server", "grpc_port", "9090", false, func(c *Config) bool { return c.Server.GRPCPort == 9090 }},
		{"server", "max_clients", "50", false, func(c *Config) bool { return c.Server.MaxClients == 50 }},
		{"monitoring", "default_interval", "10", false, func(c *Config) bool { return c.Monitoring.DefaultInterval == 10 }},
		{"monitoring", "default_window", "30", false, func(c *Config) bool { return c.Monitoring.DefaultWindow == 30 }},
		{"server", "grpc_port", "invalid", true, nil},
		{"monitoring", "default_interval", "invalid", true, nil},
		{"unknown", "field", "value", false, nil}, // silently ignored
	}

	for _, tt := range tests {
		t.Run(tt.section+"."+tt.key, func(t *testing.T) {
			err := setField(cfg, tt.section, tt.key, tt.value)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.check != nil && !tt.check(cfg) {
				t.Errorf("check failed for %s.%s = %s", tt.section, tt.key, tt.value)
			}
		})
	}
}
