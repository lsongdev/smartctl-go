package smartctl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadReport(t *testing.T) {
	info, err := Open("testdata/samsung-850-pro.json")
	if err != nil {
		t.Fatal(err)
	}
	if info.Err() != nil || info.ModelName != "Samsung SSD 850 PRO 1TB" || !info.SmartStatus.Passed ||
		info.Temperature.Current != 30 || info.PowerOnTime.Hours != 98678 || info.RotationRate != 0 || !info.Trim.Supported {
		t.Fatalf("report %+v", info)
	}
	if len(info.ATASmartAttributes.Table) != 14 || info.ATASmartAttributes.Table[0].Name != "Reallocated_Sector_Ct" {
		t.Fatalf("attributes %+v", info.ATASmartAttributes.Table)
	}
}

func TestReportWithoutData(t *testing.T) {
	info, err := Open("testdata/usb-bridge.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := info.Err(); err == nil || !strings.Contains(err.Error(), "Unknown USB bridge") {
		t.Fatalf("err = %v", err)
	}
}

// fakeSmartctl writes a script that prints a report and exits with code.
func fakeSmartctl(t *testing.T, report string, code int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smartctl")
	script := "#!/bin/sh\necho \"$@\" > \"$0.args\"\ncat <<'END'\n" + report + "\nEND\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCollect(t *testing.T) {
	data, _ := os.ReadFile("testdata/samsung-850-pro.json")
	// A failing disk exits non-zero but its report is still returned.
	cmd := fakeSmartctl(t, strings.TrimSpace(string(data)), 4)
	info, err := Collect(context.Background(), "/dev/sda", Options{Command: cmd, NoWake: true, Type: "sat"})
	if err != nil || info.ModelName == "" {
		t.Fatalf("collect: %v", err)
	}
	args, _ := os.ReadFile(cmd + ".args")
	if strings.TrimSpace(string(args)) != "--json=c -d sat -n standby,2 -a /dev/sda" {
		t.Fatalf("args %q", args)
	}

	asleep := `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]}}`
	if _, err := Collect(context.Background(), "/dev/sdb", Options{Command: fakeSmartctl(t, asleep, 2), NoWake: true}); !errors.Is(err, ErrAsleep) {
		t.Fatalf("asleep: %v", err)
	}
}

func TestScanAndSelfTest(t *testing.T) {
	scan := `{"devices":[{"name":"/dev/sda","info_name":"/dev/sda","type":"scsi","protocol":"SCSI"},{"name":"/dev/nvme0","info_name":"/dev/nvme0","type":"nvme","protocol":"NVMe"}]}`
	devices, err := Scan(context.Background(), Options{Command: fakeSmartctl(t, scan, 0)})
	if err != nil || len(devices) != 2 || devices[1].Type != "nvme" {
		t.Fatalf("scan %+v %v", devices, err)
	}
	cmd := fakeSmartctl(t, `{"smartctl":{"exit_status":0}}`, 0)
	if err := SelfTest(context.Background(), "/dev/sda", TestShort, Options{Command: cmd, NoWake: true}); err != nil {
		t.Fatal(err)
	}
	if args, _ := os.ReadFile(cmd + ".args"); strings.TrimSpace(string(args)) != "--json=c -t short /dev/sda" {
		t.Fatalf("args %q", args)
	}
	if err := SelfTest(context.Background(), "/dev/sda", "erase", Options{Command: cmd}); err == nil {
		t.Fatal("unknown test accepted")
	}
	refused := fakeSmartctl(t, `{"smartctl":{"exit_status":4,"messages":[{"string":"Can't start self-test without aborting current test","severity":"error"}]}}`, 4)
	if err := SelfTest(context.Background(), "/dev/sda", TestLong, Options{Command: refused}); err == nil || !strings.Contains(err.Error(), "aborting current test") {
		t.Fatalf("refused: %v", err)
	}
}
