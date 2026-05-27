package collector

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

// testHelperExecCommand creates an execCommand function that runs a command
// producing the given output string, or a command that fails.
//
// On Windows, we use PowerShell's Write-Output which handles multi-line strings.
// On Unix, we use "echo" directly.
func testHelperExecCommand(output string, err error) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if err != nil {
			// Use a command that will fail
			if runtime.GOOS == "windows" {
				return exec.CommandContext(ctx, "cmd", "/c", "exit", "1")
			}
			return exec.CommandContext(ctx, "false")
		}
		// Use echo to produce the fake output
		if runtime.GOOS == "windows" {
			// Use PowerShell to handle multi-line output properly
			return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '"+output+"'")
		}
		return exec.CommandContext(ctx, "echo", output)
	}
}

// ---------------------------------------------------------------------------
// Tests for collectLoad dispatch function
// ---------------------------------------------------------------------------

func TestCollectLoad_UnsupportedPlatform(t *testing.T) {
	// collectLoad dispatches based on runtime.GOOS.
	// We can't change runtime.GOOS at runtime, but we can verify that
	// the function doesn't panic and returns a predictable error on failure.
	_, err := collectLoad(context.Background())
	if err != nil {
		t.Logf("collectLoad returned error (expected on some platforms): %v", err)
	}
}

func TestCollectCPU_UnsupportedPlatform(t *testing.T) {
	_, err := collectCPU(context.Background())
	if err != nil {
		t.Logf("collectCPU returned error (expected on some platforms): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for collectLoadLinux
// ---------------------------------------------------------------------------

func TestCollectLoadLinux_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "top - 16:20:30 up 10 days,  2:15,  1 user,  load average: 0.15, 0.10, 0.05\n"
	execCommand = testHelperExecCommand(output, nil)

	load, err := collectLoadLinux(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if load.Load1 != 0.15 {
		t.Errorf("expected Load1=0.15, got %v", load.Load1)
	}
}

func TestCollectLoadLinux_EmptyOutput(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = testHelperExecCommand("", nil)

	_, err := collectLoadLinux(context.Background())
	if err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
}

func TestCollectLoadLinux_NoLoadAverage(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "top - 16:20:30 up 10 days\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectLoadLinux(context.Background())
	if err == nil {
		t.Fatal("expected error when load average not found, got nil")
	}
}

func TestCollectLoadLinux_InvalidLoadValue(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "top - ... load average: abc, 0.10, 0.05\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectLoadLinux(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid load value, got nil")
	}
}

func TestCollectLoadLinux_CommandError(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = testHelperExecCommand("", errors.New("command failed"))

	_, err := collectLoadLinux(context.Background())
	if err == nil {
		t.Fatal("expected error for command failure, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests for collectCPULinux
// ---------------------------------------------------------------------------

func TestCollectCPULinux_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "%Cpu(s):  5.0 us,  2.0 sy,  0.0 ni, 92.0 id,  1.0 wa,  0.0 hi,  0.0 si,  0.0 st\n"
	execCommand = testHelperExecCommand(output, nil)

	cpu, err := collectCPULinux(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 5.0 {
		t.Errorf("expected PercentUser=5.0, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 2.0 {
		t.Errorf("expected PercentSystem=2.0, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 92.0 {
		t.Errorf("expected PercentIdle=92.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPULinux_NoCPULine(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "top - 16:20:30 up 10 days\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectCPULinux(context.Background())
	if err == nil {
		t.Fatal("expected error when CPU line not found, got nil")
	}
}

func TestCollectCPULinux_PartialFields(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Only user and idle present
	output := "%Cpu(s):  5.0 us, 92.0 id\n"
	execCommand = testHelperExecCommand(output, nil)

	cpu, err := collectCPULinux(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 5.0 {
		t.Errorf("expected PercentUser=5.0, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 0.0 {
		t.Errorf("expected PercentSystem=0.0, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 92.0 {
		t.Errorf("expected PercentIdle=92.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPULinux_CommandError(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = testHelperExecCommand("", errors.New("command failed"))

	_, err := collectCPULinux(context.Background())
	if err == nil {
		t.Fatal("expected error for command failure, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests for collectLoadDarwin
// ---------------------------------------------------------------------------

func TestCollectLoadDarwin_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "Processes: 456 total, 3 running, 453 sleeping, 0 zombie ...\nLoad Avg: 2.15, 2.10, 2.05\nCPU usage: 10.0% user, 5.0% sys, 85.0% idle\n"
	execCommand = testHelperExecCommand(output, nil)

	load, err := collectLoadDarwin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if load.Load1 != 2.15 {
		t.Errorf("expected Load1=2.15, got %v", load.Load1)
	}
}

func TestCollectLoadDarwin_NoLoadAvgLine(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "Processes: 456 total\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectLoadDarwin(context.Background())
	if err == nil {
		t.Fatal("expected error when Load Avg line not found, got nil")
	}
}

func TestCollectLoadDarwin_InvalidFormat(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "Load Avg: abc, 2.10, 2.05\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectLoadDarwin(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid load value, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests for collectCPUDarwin
// ---------------------------------------------------------------------------

func TestCollectCPUDarwin_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "CPU usage: 10.0% user, 5.0% sys, 85.0% idle\n"
	execCommand = testHelperExecCommand(output, nil)

	cpu, err := collectCPUDarwin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 10.0 {
		t.Errorf("expected PercentUser=10.0, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 5.0 {
		t.Errorf("expected PercentSystem=5.0, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 85.0 {
		t.Errorf("expected PercentIdle=85.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPUDarwin_NoCPULine(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "Processes: 456 total\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectCPUDarwin(context.Background())
	if err == nil {
		t.Fatal("expected error when CPU usage line not found, got nil")
	}
}

func TestCollectCPUDarwin_PartialFields(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Only user and idle present
	output := "CPU usage: 10.0% user, 85.0% idle\n"
	execCommand = testHelperExecCommand(output, nil)

	cpu, err := collectCPUDarwin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 10.0 {
		t.Errorf("expected PercentUser=10.0, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 0.0 {
		t.Errorf("expected PercentSystem=0.0, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 85.0 {
		t.Errorf("expected PercentIdle=85.0, got %v", cpu.PercentIdle)
	}
}

// ---------------------------------------------------------------------------
// Tests for collectLoadWindows
// ---------------------------------------------------------------------------

func TestCollectLoadWindows_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "15\n"
	execCommand = testHelperExecCommand(output, nil)

	load, err := collectLoadWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if load.Load1 != 15.0 {
		t.Errorf("expected Load1=15.0, got %v", load.Load1)
	}
}

func TestCollectLoadWindows_MultiCore(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Multiple lines (one per core) - should take the last non-empty one
	output := "10\n25\n30\n"
	execCommand = testHelperExecCommand(output, nil)

	load, err := collectLoadWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if load.Load1 != 30.0 {
		t.Errorf("expected Load1=30.0 (last value), got %v", load.Load1)
	}
}

func TestCollectLoadWindows_EmptyOutput(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = testHelperExecCommand("", nil)

	_, err := collectLoadWindows(context.Background())
	if err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
}

func TestCollectLoadWindows_InvalidValue(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "abc\n"
	execCommand = testHelperExecCommand(output, nil)

	_, err := collectLoadWindows(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid value, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests for collectCPUWindows
// ---------------------------------------------------------------------------

func TestCollectCPUWindows_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// First call returns total usage, second call returns breakdown
	callCount := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			// First call: total CPU load percentage
			return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '25'")
		}
		// Second call: user and privileged time
		return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '17.5 7.5'")
	}

	cpu, err := collectCPUWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 17.5 {
		t.Errorf("expected PercentUser=17.5, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 7.5 {
		t.Errorf("expected PercentSystem=7.5, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 75.0 {
		t.Errorf("expected PercentIdle=75.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPUWindows_TotalUsageError(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = testHelperExecCommand("", errors.New("command failed"))

	_, err := collectCPUWindows(context.Background())
	if err == nil {
		t.Fatal("expected error when total usage command fails, got nil")
	}
}

func TestCollectCPUWindows_BreakdownFallback(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	callCount := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '50'")
		}
		// Second call fails - should use fallback 70/30 split
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "1")
	}

	cpu, err := collectCPUWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fallback: 70/30 split of totalUsage=50
	if cpu.PercentUser != 35.0 {
		t.Errorf("expected PercentUser=35.0 (70%% of 50), got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 15.0 {
		t.Errorf("expected PercentSystem=15.0 (30%% of 50), got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 50.0 {
		t.Errorf("expected PercentIdle=50.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPUWindows_BreakdownInvalidOutput(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	callCount := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '50'")
		}
		// Second call returns invalid data - should use fallback
		return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output 'invalid data'")
	}

	cpu, err := collectCPUWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fallback: 70/30 split of totalUsage=50
	if cpu.PercentUser != 35.0 {
		t.Errorf("expected PercentUser=35.0 (70%% of 50), got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 15.0 {
		t.Errorf("expected PercentSystem=15.0 (30%% of 50), got %v", cpu.PercentSystem)
	}
}

// ---------------------------------------------------------------------------
// Tests for getWindowsCPUBreakdown
// ---------------------------------------------------------------------------

func TestGetWindowsCPUBreakdown_Success(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '17.5 7.5'")
	}

	user, sys := getWindowsCPUBreakdown(context.Background(), 25.0)
	if user != 17.5 {
		t.Errorf("expected user=17.5, got %v", user)
	}
	if sys != 7.5 {
		t.Errorf("expected sys=7.5, got %v", sys)
	}
}

func TestGetWindowsCPUBreakdown_CommandError(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "cmd", "/c", "exit", "1")
	}

	user, sys := getWindowsCPUBreakdown(context.Background(), 50.0)
	// Fallback: 70/30 split
	if user != 35.0 {
		t.Errorf("expected user=35.0 (70%% of 50), got %v", user)
	}
	if sys != 15.0 {
		t.Errorf("expected sys=15.0 (30%% of 50), got %v", sys)
	}
}

func TestGetWindowsCPUBreakdown_InvalidOutput(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output 'invalid'")
	}

	user, sys := getWindowsCPUBreakdown(context.Background(), 50.0)
	// Fallback: 70/30 split
	if user != 35.0 {
		t.Errorf("expected user=35.0 (70%% of 50), got %v", user)
	}
	if sys != 15.0 {
		t.Errorf("expected sys=15.0 (30%% of 50), got %v", sys)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestCollectLoadLinux_WhitespaceInOutput(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	output := "  top - ... load average:  0.15 , 0.10 , 0.05  \n"
	execCommand = testHelperExecCommand(output, nil)

	load, err := collectLoadLinux(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if load.Load1 != 0.15 {
		t.Errorf("expected Load1=0.15, got %v", load.Load1)
	}
}

func TestCollectCPULinux_AlternatePrefix(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	// Some systems use %CPU(s) instead of %Cpu(s)
	output := "%CPU(s):  5.0 us,  2.0 sy, 92.0 id\n"
	execCommand = testHelperExecCommand(output, nil)

	cpu, err := collectCPULinux(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cpu.PercentUser != 5.0 {
		t.Errorf("expected PercentUser=5.0, got %v", cpu.PercentUser)
	}
	if cpu.PercentSystem != 2.0 {
		t.Errorf("expected PercentSystem=2.0, got %v", cpu.PercentSystem)
	}
	if cpu.PercentIdle != 92.0 {
		t.Errorf("expected PercentIdle=92.0, got %v", cpu.PercentIdle)
	}
}

func TestCollectCPUWindows_IdleClamp(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	callCount := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '110'") // > 100%
		}
		return exec.CommandContext(ctx, "powershell", "-Command", "Write-Output '77 33'")
	}

	cpu, err := collectCPUWindows(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// idle = 100 - 110 = -10, clamped to 0
	if cpu.PercentIdle != 0.0 {
		t.Errorf("expected PercentIdle=0.0 (clamped), got %v", cpu.PercentIdle)
	}
}
