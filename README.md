# smartctl-go

A simple [smartctl](https://smartmontools.org/) parser written in [Go](https://golang.org/).

```go
import "github.com/lsongdev/smartctl-go/smartctl"

info, err := smartctl.Check("/dev/disk1s1")
info, err := smartctl.Open("./sda-report.json")
info, err := smartctl.Read([]byte{})

log.Println(info.ModelName)
log.Println(info.SerialNumber)
log.Println(info.SmartStatus)
log.Println(info.Temperature)
log.Println(info.PowerCycleCount)
log.Println(info.PowerOnTime)
for _, attr := range info.ATASmartAttributes.Table {
  log.Println(attr)
}
```