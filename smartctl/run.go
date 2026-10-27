package smartctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Exit status bits of smartctl (see smartctl(8), RETURN VALUES). The first
// two mean no data was read; the others describe the disk.
const (
	ExitCommandLine    = 1 << 0 // the command line did not parse
	ExitOpenFailed     = 1 << 1 // the device did not open, or it is asleep (see Options.NoWake)
	ExitCommandFailed  = 1 << 2 // a SMART command failed, or data has a checksum error
	ExitFailing        = 1 << 3 // SMART status: the disk is failing
	ExitPrefailFailing = 1 << 4 // a prefailure attribute is at or below its threshold
	ExitPastFailing    = 1 << 5 // an attribute was at or below its threshold in the past
	ExitErrorLog       = 1 << 6 // the device error log has entries
	ExitSelfTestLog    = 1 << 7 // the self-test log has errors
)

// ErrAsleep is returned by Options.NoWake reads of a disk in standby.
var ErrAsleep = errors.New("smartctl: the disk is in standby")

// Options adjusts how a device is read.
type Options struct {
	// Type is smartctl's -d device type (sat, nvme, scsi, …); empty lets
	// smartctl choose.
	Type string
	// NoWake skips a disk that is in standby rather than spinning it up.
	NoWake bool
	// Command is the smartctl executable; "smartctl" by default.
	Command string
}

// asleepStatus is the exit status smartctl uses for a skipped standby disk.
const asleepStatus = 2

func (o Options) args(extra ...string) []string {
	args := []string{"--json=c"}
	if o.Type != "" {
		args = append(args, "-d", o.Type)
	}
	if o.NoWake {
		args = append(args, "-n", fmt.Sprintf("standby,%d", asleepStatus))
	}
	return append(args, extra...)
}

func (o Options) command() string {
	if o.Command != "" {
		return o.Command
	}
	return "smartctl"
}

// run runs smartctl and returns its JSON output. smartctl exits non-zero
// for disks with problems while still printing a full report, so a
// non-zero exit is only an error when nothing could be read.
func (o Options) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, o.command(), o.args(args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("smartctl: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("smartctl: %w", err)
	}
	return out, ctx.Err()
}

// Collect reads a device's full report (smartctl -a). A disk with problems
// is still returned without an error: see SmartInfo.SmartStatus and the
// exit status in SmartInfo.SmartCtl. Only a report with no data is an error,
// and it is returned along with what smartctl said.
func Collect(ctx context.Context, device string, opts Options) (*SmartInfo, error) {
	out, err := opts.run(ctx, "-a", device)
	if err != nil {
		return nil, err
	}
	info, err := Read(out)
	if err != nil {
		return nil, err
	}
	return info, info.Err()
}

// Err describes why a report holds no data, or returns nil when it does.
func (info *SmartInfo) Err() error {
	status := info.SmartCtl.ExitStatus
	if status&(ExitCommandLine|ExitOpenFailed) == 0 {
		return nil
	}
	var messages []string
	for _, m := range info.SmartCtl.Messages {
		if strings.Contains(m.String, "STANDBY") || strings.Contains(m.String, "SLEEP") {
			return ErrAsleep
		}
		if m.Severity == "error" || len(info.SmartCtl.Messages) == 1 {
			messages = append(messages, m.String)
		}
	}
	if len(messages) == 0 {
		return fmt.Errorf("smartctl: exit status %d", status)
	}
	return errors.New(strings.Join(messages, "; "))
}

// Device is a device smartctl found.
type Device struct {
	Name     string `json:"name"`      // e.g. /dev/sda
	InfoName string `json:"info_name"` // e.g. "/dev/sda [SAT]"
	Type     string `json:"type"`      // the -d type to read it with
	Protocol string `json:"protocol"`  // ATA, NVMe, SCSI
}

// Scan lists the devices smartctl can read (smartctl --scan).
func Scan(ctx context.Context, opts Options) ([]Device, error) {
	opts.NoWake = false // not a read
	out, err := opts.run(ctx, "--scan")
	if err != nil {
		return nil, err
	}
	var result struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("smartctl: %w", err)
	}
	return result.Devices, nil
}

// Self-test kinds for SelfTest.
const (
	TestShort      = "short"
	TestLong       = "long"
	TestConveyance = "conveyance"
	TestAbort      = "abort"
)

// SelfTest starts a self-test on the device, which runs in the background
// on the disk itself, or aborts the one running. Progress shows in the
// device's report.
func SelfTest(ctx context.Context, device, kind string, opts Options) error {
	var args []string
	switch kind {
	case TestShort, TestLong, TestConveyance:
		args = []string{"-t", kind, device}
	case TestAbort:
		args = []string{"-X", device}
	default:
		return fmt.Errorf("smartctl: unknown self-test %q", kind)
	}
	opts.NoWake = false // a test needs the disk awake
	out, err := opts.run(ctx, args...)
	if err != nil {
		return err
	}
	info, err := Read(out)
	if err != nil {
		return err
	}
	if err := info.Err(); err != nil {
		return err
	}
	if info.SmartCtl.ExitStatus&ExitCommandFailed != 0 {
		for _, m := range info.SmartCtl.Messages {
			return errors.New(m.String)
		}
		return errors.New("smartctl: the device refused the self-test")
	}
	return nil
}

// Check reads a device's full report, waking it if needed.
//
// Deprecated: use Collect, which takes a context and options.
func Check(dev string) (*SmartInfo, error) {
	return Collect(context.Background(), dev, Options{})
}

// Read parses a smartctl --json report.
func Read(data []byte) (*SmartInfo, error) {
	info := &SmartInfo{}
	if err := json.Unmarshal(data, info); err != nil {
		return nil, fmt.Errorf("smartctl: %w", err)
	}
	return info, nil
}

// Open parses a saved smartctl --json report.
func Open(path string) (*SmartInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Read(data)
}
