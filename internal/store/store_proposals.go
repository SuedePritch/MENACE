package store

import (
	"database/sql"
	"fmt"
	"log/slog"

	mlog "menace/internal/log"
)

// ─── Proposal methods ───────────────────────────────────────────────────

func (s *Store) LoadProposals(projectID string) ([]Proposal, error) {
	rows, err := s.db.Query(
		`SELECT id, description, instruction FROM proposals WHERE project_id = ? ORDER BY rowid`, projectID,
	)
	if err != nil {
		mlog.Error("LoadProposals", slog.String("err", err.Error()))
		return nil, fmt.Errorf("LoadProposals: %w", err)
	}
	defer rows.Close()
	var proposals []Proposal
	for rows.Next() {
		var p Proposal
		if err := rows.Scan(&p.ID, &p.Description, &p.Instruction); err != nil {
			mlog.Error("LoadProposals scan", slog.String("err", err.Error()))
			continue
		}
		subs, err := s.loadProposalSubtasks(p.ID)
		if err != nil {
			return proposals, fmt.Errorf("LoadProposals: subtasks for %s: %w", p.ID, err)
		}
		p.Subtasks = subs
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("LoadProposals rows iteration", slog.String("err", err.Error()))
		return proposals, fmt.Errorf("LoadProposals: rows: %w", err)
	}
	return proposals, nil
}

func (s *Store) loadProposalSubtasks(proposalID string) ([]ProposalSubtask, error) {
	rows, err := s.db.Query(
		`SELECT id, seq, description, instruction FROM proposal_subtasks WHERE proposal_id = ? ORDER BY seq`, proposalID,
	)
	if err != nil {
		mlog.Error("loadProposalSubtasks", slog.String("err", err.Error()))
		return nil, fmt.Errorf("loadProposalSubtasks: %w", err)
	}
	defer rows.Close()
	var subs []ProposalSubtask
	for rows.Next() {
		var sub ProposalSubtask
		if err := rows.Scan(&sub.ID, &sub.Seq, &sub.Description, &sub.Instruction); err != nil {
			mlog.Error("loadProposalSubtasks scan", slog.String("err", err.Error()))
			continue
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		mlog.Error("loadProposalSubtasks rows iteration", slog.String("err", err.Error()))
		return subs, fmt.Errorf("loadProposalSubtasks: rows: %w", err)
	}
	return subs, nil
}

func (s *Store) SaveProposal(projectID, sessionID string, p Proposal) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO proposals (id, session_id, project_id, description, instruction) VALUES (?, ?, ?, ?, ?)`,
			p.ID, sessionID, projectID, p.Description, p.Instruction,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM proposal_subtasks WHERE proposal_id = ?`, p.ID); err != nil {
			return err
		}
		for _, sub := range p.Subtasks {
			if _, err := tx.Exec(
				`INSERT INTO proposal_subtasks (id, proposal_id, seq, description, instruction) VALUES (?, ?, ?, ?, ?)`,
				sub.ID, p.ID, sub.Seq, sub.Description, sub.Instruction,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DeleteProposal(proposalID string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM proposal_subtasks WHERE proposal_id = ?`, proposalID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM proposals WHERE id = ?`, proposalID)
		return err
	})
}

func (s *Store) ClearProposals(projectID string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM proposal_subtasks WHERE proposal_id IN (SELECT id FROM proposals WHERE project_id = ?)`, projectID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM proposals WHERE project_id = ?`, projectID)
		return err
	})
}
