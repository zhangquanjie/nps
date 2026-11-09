package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/proxy"
	"ehang.io/nps/server/tool"
)

func listenTCP(t *file.Tunnel) (*proxy.TunnelModeServer, error) {
	if !tool.ServerPortAllowed(t.Port) {
		return nil, fmt.Errorf("TCP port %d is not allowed", t.Port)
	}
	service := proxy.NewTunnelModeServer(proxy.ProcessTunnel, Bridge, t)
	if err := service.Listen(); err != nil {
		return nil, fmt.Errorf("cannot open TCP port %d: %w", t.Port, err)
	}
	return service, nil
}

func runTCP(t *file.Tunnel, service *proxy.TunnelModeServer) {
	RunList.Store(t.Id, service)
	go service.Serve()
	startFlowStore()
}

// AddTCPTunnels reserves every listener before publishing any of the tasks.
func AddTCPTunnels(tasks []*file.Tunnel) error {
	listeners := make([]*proxy.TunnelModeServer, 0, len(tasks))
	for _, task := range tasks {
		listener, err := listenTCP(task)
		if err != nil {
			for _, opened := range listeners {
				opened.Close()
			}
			return err
		}
		listeners = append(listeners, listener)
	}

	db := file.GetDb().JsonDb
	for i, task := range tasks {
		db.Tasks.Store(task.Id, task)
		runTCP(task, listeners[i])
	}
	db.StoreTasksToJsonFile()
	return nil
}

func ReplaceTCPTask(old, updated *file.Tunnel) error {
	service, running := RunList.Load(old.Id)
	if running {
		if service != nil {
			if err := service.(proxy.Service).Close(); err != nil {
				return err
			}
		}
		RunList.Delete(old.Id)
	}
	if err := AddTask(updated); err != nil {
		if running {
			if restartErr := AddTask(old); restartErr != nil {
				return fmt.Errorf("%v; could not restart the original tunnel: %w", err, restartErr)
			}
		}
		return err
	}
	return file.GetDb().UpdateTask(updated)
}

type tcpRangeKey struct {
	id       int
	clientID int
	ip       string
	ports    string
	name     string
}

func rangeKey(t *file.Tunnel) tcpRangeKey {
	return tcpRangeKey{t.PortRangeID, t.Client.Id, t.ServerIp, t.PortRange, t.PortRangeName}
}

type tcpTunnelRow struct {
	Id, Port, RangeCount int
	Mode, Remark, Ports  string
	SourceIP             string
	SourceAt             *time.Time
	Status, RunStatus    bool
	Target               struct{ TargetStr string }
	Client               struct {
		Id        int
		IsConnect bool
	}
	Flow struct{ InletFlow, ExportFlow int64 }
}

func tcpRow(t *file.Tunnel) *tcpTunnelRow {
	row := &tcpTunnelRow{Id: t.Id, Port: t.Port, Mode: t.Mode, Remark: t.Remark}
	row.Client.Id = t.Client.Id
	if Bridge != nil {
		_, row.Client.IsConnect = Bridge.Client.Load(t.Client.Id)
	}
	_, row.RunStatus = RunList.Load(t.Id)
	row.Target.TargetStr = t.Target.TargetStr
	t.RLock()
	row.Status = t.Status
	t.RUnlock()
	row.Flow.InletFlow, row.Flow.ExportFlow = t.Flow.Totals()
	ip, seen := t.LastSource()
	if ip != "" {
		row.SourceIP, row.SourceAt = ip, &seen
	}
	return row
}

// GetTCPRows groups ranges before pagination. A range ID opens its member ports.
func GetTCPRows(start, length, clientID, rangeID int, search string) ([]*tcpTunnelRow, int) {
	rows := make([]*tcpTunnelRow, 0)
	db := file.GetDb()
	var selected tcpRangeKey
	if rangeID != 0 {
		t, err := db.GetTask(rangeID)
		if err != nil || t.Mode != "tcp" || t.PortRange == "" || (clientID != 0 && t.Client.Id != clientID) {
			return rows, 0
		}
		selected = rangeKey(t)
	}

	type rangeRow struct {
		row     *tcpTunnelRow
		matches bool
	}
	groups := make(map[tcpRangeKey]*rangeRow)
	search = strings.TrimSpace(search)
	db.JsonDb.Tasks.Range(func(_, value interface{}) bool {
		t := value.(*file.Tunnel)
		if t.Mode != "tcp" || (clientID != 0 && t.Client.Id != clientID) {
			return true
		}
		key := rangeKey(t)
		if rangeID != 0 && key != selected {
			return true
		}
		matches := search == "" || search == strconv.Itoa(t.Id) || search == strconv.Itoa(t.Port) ||
			strings.Contains(t.Remark, search) || strings.Contains(t.Password, search)
		row := tcpRow(t)
		if rangeID != 0 || t.PortRange == "" {
			if matches {
				rows = append(rows, row)
			}
			return true
		}
		group := groups[key]
		if group == nil {
			group = &rangeRow{row: &tcpTunnelRow{Id: row.Id, Mode: row.Mode, Ports: t.PortRange, Remark: t.PortRangeName}}
			group.row.Client = row.Client
			groups[key] = group
		}
		group.matches = group.matches || matches || strings.Contains(t.PortRange, search)
		if row.Id < group.row.Id {
			group.row.Id = row.Id
		}
		if row.SourceAt != nil && (group.row.SourceAt == nil || row.SourceAt.After(*group.row.SourceAt)) {
			group.row.SourceIP, group.row.SourceAt = row.SourceIP, row.SourceAt
		}
		group.row.RangeCount++
		group.row.Flow.InletFlow += row.Flow.InletFlow
		group.row.Flow.ExportFlow += row.Flow.ExportFlow
		return true
	})
	for _, group := range groups {
		if group.matches {
			rows = append(rows, group.row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rangeID != 0 && rows[i].Port != rows[j].Port {
			return rows[i].Port < rows[j].Port
		}
		return rows[i].Id < rows[j].Id
	})
	total := len(rows)
	if start < 0 || start >= total || length <= 0 {
		return rows[:0], total
	}
	if length > total-start {
		length = total - start
	}
	return rows[start : start+length], total
}

func NewTCPRangeTasks(base *file.Tunnel, first, last int, target string) ([]*file.Tunnel, error) {
	host, ports := "", target
	// Split at the last colon so bracketed IPv6 targets are accepted too.
	if colon := strings.LastIndex(target, ":"); colon >= 0 {
		host, ports = target[:colon+1], target[colon+1:]
	}
	targetFirst, targetLast, ok := common.ParsePortRange(ports)
	if !ok || targetLast-targetFirst != last-first {
		return nil, fmt.Errorf("the target must contain a matching range, for example 127.0.0.1:%d-%d", first, last)
	}

	db := file.GetDb().JsonDb
	tasks := make([]*file.Tunnel, 0, last-first+1)
	portRange := fmt.Sprintf("%d-%d", first, last)
	for port := first; port <= last; port++ {
		id := base.Id
		if port != first {
			id = int(db.GetTaskId())
		}
		tasks = append(tasks, &file.Tunnel{
			Id: id, Mode: "tcp", Port: port, ServerIp: base.ServerIp,
			Client: base.Client, Status: true, Flow: new(file.Flow),
			Remark: base.Remark + "_" + strconv.Itoa(port), PortRange: portRange, PortRangeName: base.Remark, PortRangeID: base.Id,
			Target: &file.Target{TargetStr: host + strconv.Itoa(targetFirst+port-first), LocalProxy: base.Target.LocalProxy},
		})
	}
	return tasks, nil
}
