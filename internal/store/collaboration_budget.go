package store

import (
	"database/sql"
	"errors"
	"time"
)

const DefaultCollaborationMinutes = 20
const CollaborationTimeReason = "已达到本次协作的时长上限，已暂停并保留已有内容；发送“继续”可接着推进"
const CollaborationTokenReason = "已达到本次协作的 Token 上限，已暂停并保留已有内容；发送“继续”可接着推进"

var ErrCollaborationTime = errors.New(CollaborationTimeReason)
var ErrCollaborationTokens = errors.New(CollaborationTokenReason)

type budgetReader interface {
	QueryRow(string, ...any) *sql.Row
}

// Waiting before the first execution does not spend time. Retries and restarts
// share the original deadline; each chain snapshots its conversation's budget.
func collaborationDeadline(q budgetReader, c Chain) (time.Time, error) {
	if (c.Action != "lead" && c.Action != "discussion") || c.TimeBudgetMinutes == 0 {
		return time.Time{}, nil
	}
	var started string
	if err := q.QueryRow(`SELECT started_at FROM runs WHERE chain_id=? AND started_at<>'' ORDER BY rowid LIMIT 1`, c.ID).Scan(&started); err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, started)
	return t.Add(time.Duration(c.TimeBudgetMinutes) * time.Minute), err
}

func collaborationBudgetError(q budgetReader, c Chain) error {
	if c.Action != "lead" && c.Action != "discussion" {
		return nil
	}
	deadline, err := collaborationDeadline(q, c)
	if err != nil {
		return err
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return ErrCollaborationTime
	}
	if c.TokenBudget > 0 {
		var used int
		if err = q.QueryRow(`SELECT COALESCE(SUM(input_tokens+output_tokens),0) FROM runs WHERE chain_id=?`, c.ID).Scan(&used); err != nil {
			return err
		}
		if used >= c.TokenBudget {
			return ErrCollaborationTokens
		}
	}
	return nil
}

func (s *Store) CollaborationDeadline(runID string) (time.Time, error) {
	c, err := s.RunChain(runID)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return collaborationDeadline(s.db, c)
}

func (s *Store) CheckCollaborationBudget(runID string) error {
	c, err := s.RunChain(runID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return collaborationBudgetError(s.db, c)
}

// Usage is saved after every model response, including summaries and failed or
// interrupted runs. Finishing a run never overwrites these counters.
func (s *Store) RecordTokenUsage(runID string, input, output, cached int, estimated bool, revision int) error {
	_, err := s.db.Exec(`UPDATE runs SET input_tokens=input_tokens+?,output_tokens=output_tokens+?,cached_tokens=cached_tokens+?,usage_estimated=MAX(usage_estimated,?),revision=MAX(revision,?) WHERE id=?`, input, output, cached, estimated, revision, runID)
	return err
}
