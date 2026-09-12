package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gokayybaz/bazusop/internal/serviceinventory"
)

type ServiceCollector struct {
	collect func(context.Context) ([]serviceinventory.Fact, error)
	now     func() time.Time
}

func NewServiceCollector() *ServiceCollector {
	return &ServiceCollector{collect: collectPlatformServices, now: time.Now}
}

func (collector *ServiceCollector) Collect(ctx context.Context) (serviceinventory.Snapshot, error) {
	if collector == nil || collector.collect == nil || collector.now == nil {
		return serviceinventory.Snapshot{}, errors.New("service collector is incomplete")
	}
	services, err := collector.collect(ctx)
	if err != nil {
		return serviceinventory.Snapshot{}, err
	}
	if len(services) > 5000 {
		return serviceinventory.Snapshot{}, errors.New("service snapshot exceeds 5000 entries")
	}
	sort.Slice(services, func(left, right int) bool { return services[left].Name < services[right].Name })
	return serviceinventory.Snapshot{ObservedAt: collector.now().UTC(), Services: services}, nil
}

func parseSystemdServices(unitsOutput, unitFilesOutput string) ([]serviceinventory.Fact, error) {
	services := make(map[string]serviceinventory.Fact)
	for _, line := range strings.Split(unitsOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			return nil, fmt.Errorf("invalid systemd service row %q", line)
		}
		fact := serviceinventory.Fact{Name: fields[0], State: systemdRuntimeState(fields[2]), StartupType: "unknown"}
		if len(fields) > 4 {
			fact.DisplayName = strings.Join(fields[4:], " ")
		}
		services[fact.Name] = fact
	}
	for _, line := range strings.Split(unitFilesOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ".service") {
			return nil, fmt.Errorf("invalid systemd unit-file row %q", line)
		}
		fact, exists := services[fields[0]]
		if !exists {
			fact = serviceinventory.Fact{Name: fields[0], State: "stopped"}
		}
		fact.StartupType = systemdStartupType(fields[1])
		services[fact.Name] = fact
	}
	result := make([]serviceinventory.Fact, 0, len(services))
	for _, service := range services {
		result = append(result, service)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

func systemdRuntimeState(activeState string) string {
	switch activeState {
	case "active":
		return "running"
	case "inactive":
		return "stopped"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func systemdStartupType(unitFileState string) string {
	switch unitFileState {
	case "enabled", "enabled-runtime", "alias", "linked", "linked-runtime", "generated":
		return "automatic"
	case "disabled", "masked", "masked-runtime":
		return "disabled"
	case "static", "indirect":
		return "manual"
	default:
		return "unknown"
	}
}
