package collect

import (
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"
)

func collectTemperatures() []TempSensor {
	stats, err := sensors.SensorsTemperatures()
	if err != nil || len(stats) == 0 {
		return nil
	}
	out := make([]TempSensor, 0, len(stats))
	seen := map[string]struct{}{}
	for _, s := range stats {
		temp := s.Temperature
		if temp == 0 && s.High == 0 && s.Critical == 0 {
			continue
		}
		// Skip obviously invalid / unset readings.
		if temp < -50 || temp > 200 {
			continue
		}
		sensor := strings.TrimSpace(s.SensorKey)
		if sensor == "" {
			sensor = "unknown"
		}
		if _, ok := seen[sensor]; ok {
			continue
		}
		seen[sensor] = struct{}{}
		out = append(out, TempSensor{
			Sensor: sensor,
			Label:  sensor,
			TempC:  round2(temp),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
