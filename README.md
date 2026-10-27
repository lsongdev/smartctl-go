# smartctl-go

Read disk health from [smartmontools](https://smartmontools.org/)' `smartctl`
in [Go](https://golang.org/), through its JSON output (smartctl 7.0 and
later). The library has no dependencies; smartctl must be installed.

```go
import "github.com/lsongdev/smartctl-go/smartctl"

devices, err := smartctl.Scan(ctx, smartctl.Options{})

// NoWake leaves a disk in standby alone (smartctl.ErrAsleep) instead of
// spinning it up.
info, err := smartctl.Collect(ctx, "/dev/sda", smartctl.Options{NoWake: true})

log.Println(info.ModelName, info.SerialNumber)
log.Println(info.SmartStatus.Passed, info.Temperature.Current, info.PowerOnTime.Hours)
for _, attr := range info.ATASmartAttributes.Table {
	log.Println(attr.ID, attr.Name, attr.Value, attr.Thresh, attr.Raw.String)
}

err = smartctl.SelfTest(ctx, "/dev/sda", smartctl.TestShort, smartctl.Options{})

info, err = smartctl.Open("./sda-report.json") // a saved smartctl --json report
```

smartctl exits non-zero for disks with problems while still printing a full
report: `Collect` returns that report without an error (check
`info.SmartStatus` and `info.SmartCtl.ExitStatus` against the `Exit…` bits),
and only fails when no data could be read, with smartctl's own message
(for example a USB bridge it does not know).

The web UI that used to live here is gone; the
[OhMyServer](https://github.com/lsongdev/ohmyserver) panel shows disk health
using this library.
