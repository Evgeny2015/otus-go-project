package collector

import "os/exec"

// execCommand is a variable so tests can override it with a fake command runner.
var execCommand = exec.CommandContext
