package agent

import (
	"bufio"
	"errors"
	"strconv"
	"strings"
)

func parseLinuxMemory(contents string) (uint64, error) {
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil && kilobytes > 0 {
				return kilobytes * 1024, nil
			}
			break
		}
	}
	return 0, errors.New("linux MemTotal is unavailable")
}

func parseOSRelease(contents string) (string, string) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found {
			values[key] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return values["NAME"], values["VERSION_ID"]
}
