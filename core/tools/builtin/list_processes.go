package builtin

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

type processInfo struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

func newListProcessesTool(_ runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "list_processes",
		Description: "List running processes with optional filters",
		Source:      "builtin",
		Parameters: objectSchema(nil, map[string]types.SchemaProperty{
			"pid":           {Type: "integer", Description: "Exact process ID filter"},
			"name_contains": {Type: "string", Description: "Substring filter against the process name"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pid, err := intArg(arguments, "pid", 0)
			if err != nil {
				return "", err
			}
			nameContains, _, err := optionalStringArg(arguments, "name_contains")
			if err != nil {
				return "", err
			}

			processes, err := listProcesses(pid)
			if err != nil {
				return "", err
			}
			if nameContains != "" {
				needle := strings.ToLower(nameContains)
				filtered := processes[:0]
				for _, process := range processes {
					if strings.Contains(strings.ToLower(process.Name), needle) {
						filtered = append(filtered, process)
					}
				}
				processes = filtered
			}

			sort.Slice(processes, func(i int, j int) bool {
				return processes[i].PID < processes[j].PID
			})

			return jsonResult(struct {
				Processes []processInfo `json:"processes"`
			}{Processes: processes})
		},
	}
}

func listProcesses(pid int) ([]processInfo, error) {
	if runtime.GOOS == "windows" {
		return listWindowsProcesses(pid)
	}
	return listUnixProcesses(pid)
}

func listWindowsProcesses(pid int) ([]processInfo, error) {
	args := []string{"/fo", "csv", "/nh"}
	if pid > 0 {
		args = append(args, "/fi", fmt.Sprintf("PID eq %d", pid))
	}
	output, err := exec.Command("tasklist", args...).Output()
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(string(output)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	processes := make([]processInfo, 0, len(records))
	for _, record := range records {
		if len(record) < 2 {
			continue
		}
		name := strings.TrimSpace(record[0])
		if name == "INFO: No tasks are running which match the specified criteria." || name == "" {
			continue
		}
		processID, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}
		processes = append(processes, processInfo{PID: processID, Name: name, Command: name})
	}
	return processes, nil
}

func listUnixProcesses(pid int) ([]processInfo, error) {
	args := []string{"-eo", "pid=,ppid=,comm=,args="}
	if pid > 0 {
		args = []string{"-p", strconv.Itoa(pid), "-o", "pid=,ppid=,comm=,args="}
	}
	output, err := exec.Command("ps", args...).Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	processes := make([]processInfo, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		processID, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		parentID, _ := strconv.Atoi(fields[1])
		command := fields[2]
		if len(fields) > 3 {
			command = strings.Join(fields[3:], " ")
		}
		processes = append(processes, processInfo{PID: processID, PPID: parentID, Name: fields[2], Command: command})
	}
	return processes, nil
}
