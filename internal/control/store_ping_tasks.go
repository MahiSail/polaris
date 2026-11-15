package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type PingTask struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Target    string   `json:"target"`
	Interval  int      `json:"interval"`
	AutoJoin  bool     `json:"auto_join"`
	NodeIDs   []string `json:"node_ids"`
	Sort      int      `json:"sort"`
	CreatedAt int64    `json:"created_at"`
}

func normalizePingNodeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (s *Store) initPingTasksTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS ping_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  target TEXT NOT NULL,
  interval INTEGER NOT NULL DEFAULT 60,
  auto_join INTEGER NOT NULL DEFAULT 1,
  node_ids TEXT NOT NULL DEFAULT '[]',
  sort INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
`)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) ListPingTasks(ctx context.Context) ([]PingTask, error) {
	if err := s.initPingTasksTable(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, target, interval, auto_join, node_ids, sort, created_at FROM ping_tasks ORDER BY sort ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]PingTask, 0)
	for rows.Next() {
		var t PingTask
		var autoJoinInt int
		var nodeIDsJSON string
		if err := rows.Scan(&t.ID, &t.Name, &t.Target, &t.Interval, &autoJoinInt, &nodeIDsJSON, &t.Sort, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.AutoJoin = autoJoinInt == 1
		_ = json.Unmarshal([]byte(nodeIDsJSON), &t.NodeIDs)
		if t.NodeIDs == nil {
			t.NodeIDs = []string{}
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (s *Store) SavePingTask(ctx context.Context, task *PingTask) error {
	if err := s.initPingTasksTable(ctx); err != nil {
		return err
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Target = strings.TrimSpace(task.Target)
	if task.Name == "" {
		return errors.New("探测名称不能为空")
	}
	if task.Target == "" {
		return errors.New("目标地址不能为空")
	}
	if task.Interval < 5 {
		task.Interval = 60
	}
	if task.AutoJoin {
		task.NodeIDs = []string{}
	} else {
		task.NodeIDs = normalizePingNodeIDs(task.NodeIDs)
		if len(task.NodeIDs) == 0 {
			return errors.New("独立探针请至少选择一台适用服务器")
		}
	}
	nodeIDsBytes, _ := json.Marshal(task.NodeIDs)
	autoJoinInt := 0
	if task.AutoJoin {
		autoJoinInt = 1
	}
	now := time.Now().Unix()

	if task.ID <= 0 {
		var maxSort sql.NullInt64
		_ = s.db.QueryRowContext(ctx, `SELECT MAX(sort) FROM ping_tasks`).Scan(&maxSort)
		nextSort := 0
		if maxSort.Valid {
			nextSort = int(maxSort.Int64) + 1
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO ping_tasks (name, target, interval, auto_join, node_ids, sort, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			task.Name, task.Target, task.Interval, autoJoinInt, string(nodeIDsBytes), nextSort, now)
		if err != nil {
			return err
		}
		task.ID, _ = res.LastInsertId()
		task.Sort = nextSort
		task.CreatedAt = now
	} else {
		_, err := s.db.ExecContext(ctx, `UPDATE ping_tasks SET name = ?, target = ?, interval = ?, auto_join = ?, node_ids = ? WHERE id = ?`,
			task.Name, task.Target, task.Interval, autoJoinInt, string(nodeIDsBytes), task.ID)
		if err != nil {
			return err
		}
		if !task.AutoJoin && len(task.NodeIDs) > 0 {
			placeholders := make([]string, len(task.NodeIDs))
			args := make([]any, 0, len(task.NodeIDs)+1)
			args = append(args, strconv.FormatInt(task.ID, 10))
			for i, nid := range task.NodeIDs {
				placeholders[i] = "?"
				args = append(args, nid)
			}
			_, _ = s.db.ExecContext(ctx, `DELETE FROM node_latency_points WHERE probe_id = ? AND node_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
			clearNodeLatencyCache("")
		}
	}
	return nil
}

func (s *Store) DeletePingTask(ctx context.Context, id int64) error {
	if err := s.initPingTasksTable(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ping_tasks WHERE id = ?`, id); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM node_latency_points WHERE probe_id = ?`, strconv.FormatInt(id, 10))
	clearNodeLatencyCache("")
	return nil
}

// SyncNodePingTasks synchronizes dedicated (non-public) probes bound to nodeID.
func (s *Store) SyncNodePingTasks(ctx context.Context, nodeID string, customProbes []PingTask) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return errors.New("节点 ID 不能为空")
	}
	normalized := make([]PingTask, 0, len(customProbes))
	for _, p := range customProbes {
		p.Name = strings.TrimSpace(p.Name)
		p.Target = strings.TrimSpace(p.Target)
		if p.Name == "" && p.Target == "" {
			continue
		}
		if p.Name == "" {
			return errors.New("探测名称不能为空")
		}
		if p.Target == "" {
			return errors.New("目标地址不能为空")
		}
		if p.Interval < 5 {
			p.Interval = 60
		}
		normalized = append(normalized, p)
	}

	existing, err := s.ListPingTasks(ctx)
	if err != nil {
		return err
	}
	existingByID := make(map[int64]PingTask)
	for _, t := range existing {
		if !t.AutoJoin && containsString(t.NodeIDs, nodeID) {
			existingByID[t.ID] = t
		}
	}

	keptIDs := make(map[int64]bool)
	for _, item := range normalized {
		if item.ID > 0 {
			if old, ok := existingByID[item.ID]; ok {
				keptIDs[item.ID] = true
				old.Name = item.Name
				old.Target = item.Target
				old.Interval = item.Interval
				old.AutoJoin = false
				if err := s.SavePingTask(ctx, &old); err != nil {
					return err
				}
				continue
			}
		}
		newTask := PingTask{
			Name:     item.Name,
			Target:   item.Target,
			Interval: item.Interval,
			AutoJoin: false,
			NodeIDs:  []string{nodeID},
		}
		if err := s.SavePingTask(ctx, &newTask); err != nil {
			return err
		}
	}

	for id, old := range existingByID {
		if keptIDs[id] {
			continue
		}
		remaining := make([]string, 0, len(old.NodeIDs))
		for _, nid := range old.NodeIDs {
			if strings.TrimSpace(nid) != nodeID {
				remaining = append(remaining, strings.TrimSpace(nid))
			}
		}
		if len(remaining) == 0 {
			if err := s.DeletePingTask(ctx, id); err != nil {
				return err
			}
		} else {
			old.NodeIDs = remaining
			if err := s.SavePingTask(ctx, &old); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) ReorderPingTasks(ctx context.Context, ids []int64) error {
	if err := s.initPingTasksTable(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE ping_tasks SET sort = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
