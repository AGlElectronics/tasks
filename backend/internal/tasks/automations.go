package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// Automations (TASK-22). A rule is data: one trigger, conditions that must
// all hold, and actions executed in order. Rules run inside the same
// transaction as the triggering change — if the change rolls back, nothing
// the rule did survives either. Each rule's own actions run under a
// savepoint, so a misconfigured rule fails alone without breaking the
// user's edit; the failure is visible in the run history.
//
// Loop guard: a chain of rules (rule A changes status, which matches rule
// B, …) shares a visited set, so each rule fires at most once per original
// change, and chains stop at depth 5 regardless.

// Trigger starts a rule.
type Trigger struct {
	// Type: task_created, status_changed, assignee_added, comment_added,
	// checklist_completed, due_approaching.
	Type string `json:"type"`
	// status_changed only: fire only when the task lands on this status…
	StatusID *uuid.UUID `json:"status_id,omitempty"`
	// …or on any status of this category (not_started, active, done).
	Category string `json:"category,omitempty"`
	// due_approaching only: days before the due date (0 = on the day).
	DaysBefore *int `json:"days_before,omitempty"`
}

// Condition narrows a rule. All conditions must hold.
type Condition struct {
	Field string `json:"field"` // list, status, priority, tag, assignee
	Value string `json:"value"` // an id, or a priority name
}

// Action is one thing a rule does.
type Action struct {
	Type     string     `json:"type"`
	StatusID *uuid.UUID `json:"status_id,omitempty"` // set_status
	UserID   *uuid.UUID `json:"user_id,omitempty"`   // assign
	Priority string     `json:"priority,omitempty"`  // set_priority
	Days     *int       `json:"days,omitempty"`      // set_due: today + days
	TagID    *uuid.UUID `json:"tag_id,omitempty"`    // add_tag
	ListID   *uuid.UUID `json:"list_id,omitempty"`   // move_list (same space)
	Title    string     `json:"title,omitempty"`     // create_subtask
	Body     string     `json:"body,omitempty"`      // add_comment, add_checklist_item
	Message  string     `json:"message,omitempty"`   // notify (followers)
}

// Automation is one rule.
type Automation struct {
	ID         uuid.UUID   `json:"id"`
	SpaceID    uuid.UUID   `json:"space_id"`
	ListID     *uuid.UUID  `json:"list_id"`
	Name       string      `json:"name"`
	Enabled    bool        `json:"enabled"`
	Trigger    Trigger     `json:"trigger"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
	CreatedBy  *uuid.UUID  `json:"created_by"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// AutomationRun is one entry in a rule's run history.
type AutomationRun struct {
	ID        uuid.UUID  `json:"id"`
	TaskID    *uuid.UUID `json:"task_id"`
	TaskKey   string     `json:"task_key"`
	Status    string     `json:"status"`
	Detail    string     `json:"detail"`
	CreatedAt time.Time  `json:"created_at"`
}

const (
	maxAutomationName  = 200
	maxAutomationParts = 10 // conditions and actions each
	maxChainDepth      = 5
)

var triggerTypes = map[string]bool{
	"task_created": true, "status_changed": true, "assignee_added": true,
	"comment_added": true, "checklist_completed": true, "due_approaching": true,
}

var conditionFields = map[string]bool{
	"list": true, "status": true, "priority": true, "tag": true, "assignee": true,
}

var actionTypes = map[string]bool{
	"set_status": true, "assign": true, "set_priority": true, "set_due": true,
	"add_tag": true, "move_list": true, "create_subtask": true,
	"add_checklist_item": true, "add_comment": true, "notify": true,
}

// AutomationInput creates or patches a rule. Pointer fields are optional on
// patch; Create requires Name, Trigger and Actions.
type AutomationInput struct {
	Name       *string      `json:"name"`
	Enabled    *bool        `json:"enabled"`
	ListID     NullableUUID `json:"list_id"`
	Trigger    *Trigger     `json:"trigger"`
	Conditions *[]Condition `json:"conditions"`
	Actions    *[]Action    `json:"actions"`
}

func (s *Service) validateAutomation(ctx context.Context, spaceID uuid.UUID, trig Trigger, conds []Condition, acts []Action, listID *uuid.UUID) error {
	issues := map[string]string{}
	if !triggerTypes[trig.Type] {
		issues["trigger"] = "unknown trigger type"
	}
	if trig.Type == "due_approaching" && (trig.DaysBefore == nil || *trig.DaysBefore < 0 || *trig.DaysBefore > 365) {
		issues["trigger"] = "due_approaching needs days_before between 0 and 365"
	}
	if trig.Category != "" && !validCategory(trig.Category) {
		issues["trigger"] = "category must be not_started, active or done"
	}
	if len(conds) > maxAutomationParts {
		issues["conditions"] = "too many"
	}
	for _, c := range conds {
		if !conditionFields[c.Field] || strings.TrimSpace(c.Value) == "" {
			issues["conditions"] = "unknown field or empty value"
		}
		if c.Field == "priority" && !validPriority(&c.Value) {
			issues["conditions"] = "priority must be urgent, high, normal or low"
		}
	}
	if len(acts) == 0 {
		issues["actions"] = "at least one action"
	}
	if len(acts) > maxAutomationParts {
		issues["actions"] = "too many"
	}
	for _, a := range acts {
		if !actionTypes[a.Type] {
			issues["actions"] = "unknown action type: " + a.Type
			continue
		}
		switch a.Type {
		case "set_status":
			if a.StatusID == nil {
				issues["actions"] = "set_status needs status_id"
			}
		case "assign":
			if a.UserID == nil {
				issues["actions"] = "assign needs user_id"
			}
		case "set_priority":
			if !validPriority(&a.Priority) {
				issues["actions"] = "set_priority needs urgent, high, normal or low"
			}
		case "set_due":
			if a.Days == nil || *a.Days < 0 || *a.Days > 3650 {
				issues["actions"] = "set_due needs days between 0 and 3650"
			}
		case "add_tag":
			if a.TagID == nil {
				issues["actions"] = "add_tag needs tag_id"
			}
		case "move_list":
			if a.ListID == nil {
				issues["actions"] = "move_list needs list_id"
			}
		case "create_subtask":
			if strings.TrimSpace(a.Title) == "" || len(a.Title) > maxTitleLen {
				issues["actions"] = "create_subtask needs a title"
			}
		case "add_checklist_item", "add_comment":
			if strings.TrimSpace(a.Body) == "" || len(a.Body) > maxCommentLen {
				issues["actions"] = a.Type + " needs a body"
			}
		case "notify":
			if strings.TrimSpace(a.Message) == "" || len(a.Message) > maxCommentLen {
				issues["actions"] = "notify needs a message"
			}
		}
	}
	if len(issues) > 0 {
		return httperror.Validation("invalid automation", issues)
	}

	// Everything the rule references must live in this space, so a rule can
	// never touch things its creator could not.
	check := func(query string, id uuid.UUID, what string) error {
		var ok bool
		if err := s.pool.QueryRow(ctx, query, id, spaceID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httperror.Validation("invalid automation", map[string]string{"actions": what + " is not in this space"})
		}
		return nil
	}
	if listID != nil {
		if err := check(`SELECT EXISTS (SELECT 1 FROM lists WHERE id = $1 AND space_id = $2)`, *listID, "list"); err != nil {
			return err
		}
	}
	if trig.StatusID != nil {
		if err := check(`SELECT EXISTS (SELECT 1 FROM statuses WHERE id = $1 AND space_id = $2)`, *trig.StatusID, "trigger status"); err != nil {
			return err
		}
	}
	for _, a := range acts {
		var err error
		switch {
		case a.StatusID != nil:
			err = check(`SELECT EXISTS (SELECT 1 FROM statuses WHERE id = $1 AND space_id = $2)`, *a.StatusID, "status")
		case a.TagID != nil:
			err = check(`SELECT EXISTS (SELECT 1 FROM tags WHERE id = $1 AND space_id = $2)`, *a.TagID, "tag")
		case a.ListID != nil:
			err = check(`SELECT EXISTS (SELECT 1 FROM lists WHERE id = $1 AND space_id = $2 AND archived_at IS NULL)`, *a.ListID, "list")
		case a.UserID != nil:
			var ok bool
			if err = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, *a.UserID).Scan(&ok); err == nil && !ok {
				err = httperror.Validation("invalid automation", map[string]string{"actions": "unknown user"})
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func validCategory(c string) bool {
	return c == "not_started" || c == "active" || c == "done"
}

func scanAutomation(row pgx.Row) (Automation, error) {
	var a Automation
	var trig, conds, acts []byte
	err := row.Scan(&a.ID, &a.SpaceID, &a.ListID, &a.Name, &a.Enabled, &trig, &conds, &acts, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(trig, &a.Trigger); err != nil {
		return a, err
	}
	if err := json.Unmarshal(conds, &a.Conditions); err != nil {
		return a, err
	}
	if err := json.Unmarshal(acts, &a.Actions); err != nil {
		return a, err
	}
	if a.Conditions == nil {
		a.Conditions = []Condition{}
	}
	if a.Actions == nil {
		a.Actions = []Action{}
	}
	return a, nil
}

const automationCols = `id, space_id, list_id, name, enabled, trigger, conditions, actions, created_by, created_at, updated_at`

// SpaceAutomations lists a space's rules.
func (s *Service) SpaceAutomations(ctx context.Context, spaceID uuid.UUID) ([]Automation, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+automationCols+` FROM automations WHERE space_id = $1 ORDER BY created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Automation{}
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateAutomation adds a rule to a space.
func (s *Service) CreateAutomation(ctx context.Context, actor, spaceID uuid.UUID, in AutomationInput) (Automation, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" || len(name) > maxAutomationName {
		return Automation{}, httperror.Validation("invalid automation", map[string]string{"name": "required"})
	}
	if in.Trigger == nil {
		return Automation{}, httperror.Validation("invalid automation", map[string]string{"trigger": "required"})
	}
	conds := []Condition{}
	if in.Conditions != nil {
		conds = *in.Conditions
	}
	acts := []Action{}
	if in.Actions != nil {
		acts = *in.Actions
	}
	if err := s.validateAutomation(ctx, spaceID, *in.Trigger, conds, acts, in.ListID.Value); err != nil {
		return Automation{}, err
	}
	trig, _ := json.Marshal(in.Trigger)
	condsRaw, _ := json.Marshal(conds)
	actsRaw, _ := json.Marshal(acts)
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO automations (space_id, list_id, name, enabled, trigger, conditions, actions, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+automationCols, spaceID, in.ListID.Value, name, enabled, trig, condsRaw, actsRaw, actor)
	return scanAutomation(row)
}

// GetAutomation fetches one rule (used for authorization and patches).
func (s *Service) GetAutomation(ctx context.Context, id uuid.UUID) (Automation, error) {
	a, err := scanAutomation(s.pool.QueryRow(ctx, `SELECT `+automationCols+` FROM automations WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, httperror.NotFound("automation")
	}
	return a, err
}

// UpdateAutomation patches a rule.
func (s *Service) UpdateAutomation(ctx context.Context, id uuid.UUID, in AutomationInput) (Automation, error) {
	cur, err := s.GetAutomation(ctx, id)
	if err != nil {
		return Automation{}, err
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > maxAutomationName {
			return Automation{}, httperror.Validation("invalid automation", map[string]string{"name": "required"})
		}
		cur.Name = n
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	if in.ListID.Set {
		cur.ListID = in.ListID.Value
	}
	if in.Trigger != nil {
		cur.Trigger = *in.Trigger
	}
	if in.Conditions != nil {
		cur.Conditions = *in.Conditions
	}
	if in.Actions != nil {
		cur.Actions = *in.Actions
	}
	if err := s.validateAutomation(ctx, cur.SpaceID, cur.Trigger, cur.Conditions, cur.Actions, cur.ListID); err != nil {
		return Automation{}, err
	}
	trig, _ := json.Marshal(cur.Trigger)
	conds, _ := json.Marshal(cur.Conditions)
	acts, _ := json.Marshal(cur.Actions)
	row := s.pool.QueryRow(ctx, `
		UPDATE automations SET list_id = $2, name = $3, enabled = $4, trigger = $5, conditions = $6, actions = $7, updated_at = now()
		 WHERE id = $1 RETURNING `+automationCols, id, cur.ListID, cur.Name, cur.Enabled, trig, conds, acts)
	return scanAutomation(row)
}

// DeleteAutomation removes a rule and its run history.
func (s *Service) DeleteAutomation(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("automation")
	}
	return nil
}

// AutomationRuns returns a rule's newest runs.
func (s *Service) AutomationRuns(ctx context.Context, id uuid.UUID, limit int) ([]AutomationRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, task_key, status, detail, created_at
		  FROM automation_runs WHERE automation_id = $1 ORDER BY created_at DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRun{}
	for rows.Next() {
		var r AutomationRun
		if err := rows.Scan(&r.ID, &r.TaskID, &r.TaskKey, &r.Status, &r.Detail, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Execution
// ----------------------------------------------------------------------------

// autoTrigger is what just happened, matched against rule triggers.
type autoTrigger struct {
	kind     string
	statusID uuid.UUID // status_changed
	category string    // status_changed
}

// taskFacts is the task state conditions are checked against, read inside
// the transaction so rules see the change that triggered them.
type taskFacts struct {
	spaceID   uuid.UUID
	listID    uuid.UUID
	statusID  uuid.UUID
	priority  string
	parentID  *uuid.UUID
	tags      map[uuid.UUID]bool
	assignees map[uuid.UUID]bool
}

func loadFacts(ctx context.Context, tx pgx.Tx, taskID uuid.UUID) (taskFacts, bool, error) {
	f := taskFacts{tags: map[uuid.UUID]bool{}, assignees: map[uuid.UUID]bool{}}
	err := tx.QueryRow(ctx, `
		SELECT space_id, list_id, status_id, COALESCE(priority, ''), parent_id
		  FROM tasks WHERE id = $1 AND deleted_at IS NULL`, taskID,
	).Scan(&f.spaceID, &f.listID, &f.statusID, &f.priority, &f.parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, err
	}
	rows, err := tx.Query(ctx, `
		SELECT tag_id, NULL::uuid FROM task_tags WHERE task_id = $1
		UNION ALL SELECT NULL, user_id FROM task_assignees WHERE task_id = $1`, taskID)
	if err != nil {
		return f, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag, user *uuid.UUID
		if err := rows.Scan(&tag, &user); err != nil {
			return f, false, err
		}
		if tag != nil {
			f.tags[*tag] = true
		}
		if user != nil {
			f.assignees[*user] = true
		}
	}
	return f, true, rows.Err()
}

func (f taskFacts) matches(conds []Condition) bool {
	for _, c := range conds {
		id, _ := uuid.Parse(c.Value)
		switch c.Field {
		case "list":
			if f.listID != id {
				return false
			}
		case "status":
			if f.statusID != id {
				return false
			}
		case "priority":
			if f.priority != c.Value {
				return false
			}
		case "tag":
			if !f.tags[id] {
				return false
			}
		case "assignee":
			if !f.assignees[id] {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// autoEvent is addEvent with a nullable actor (the due-date sweep has none).
func autoEvent(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, actor *uuid.UUID, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_events (task_id, actor_id, kind, data, task_key, task_title)
		SELECT t.id, $2, $3, $4, sp.key || '-' || t.number, t.title
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $1`, taskID, actor, kind, raw)
	return err
}

// autoNotify is notify with a nullable actor.
func autoNotify(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, actor *uuid.UUID, kind, message string, userIDs []uuid.UUID) error {
	seen := map[uuid.UUID]bool{}
	if actor != nil {
		seen[*actor] = true
	}
	for _, uid := range userIDs {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (user_id, task_id, task_key, task_title, actor_id, kind, message)
			SELECT $1, t.id, sp.key || '-' || t.number, t.title, $3, $4, $5
			  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $2`,
			uid, taskID, actor, kind, message); err != nil {
			return err
		}
	}
	return nil
}

func recordRun(ctx context.Context, tx pgx.Tx, automationID, taskID uuid.UUID, status, detail string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO automation_runs (automation_id, task_id, task_key, status, detail)
		SELECT $1, t.id, sp.key || '-' || t.number, $3, $4
		  FROM tasks t JOIN spaces sp ON sp.id = t.space_id WHERE t.id = $2`,
		automationID, taskID, status, detail)
	return err
}

// fireAutomations runs every enabled rule matching the trigger against the
// task, inside the caller's transaction. visited carries the rules already
// fired in this chain (loop guard); pass a fresh map at the first call.
func (s *Service) fireAutomations(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, actor *uuid.UUID, trig autoTrigger, visited map[uuid.UUID]bool, depth int) error {
	if depth >= maxChainDepth {
		return nil
	}
	facts, ok, err := loadFacts(ctx, tx, taskID)
	if err != nil || !ok {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT `+automationCols+` FROM automations
		 WHERE space_id = $1 AND enabled AND trigger->>'type' = $2
		   AND (list_id IS NULL OR list_id = $3)
		 ORDER BY created_at`, facts.spaceID, trig.kind, facts.listID)
	if err != nil {
		return err
	}
	var rules []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			rows.Close()
			return err
		}
		rules = append(rules, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, a := range rules {
		if visited[a.ID] {
			continue
		}
		if trig.kind == "status_changed" {
			if a.Trigger.StatusID != nil && *a.Trigger.StatusID != trig.statusID {
				continue
			}
			if a.Trigger.Category != "" && a.Trigger.Category != trig.category {
				continue
			}
		}
		if !facts.matches(a.Conditions) {
			continue
		}
		visited[a.ID] = true
		if err := s.runAutomation(ctx, tx, a, taskID, actor, visited, depth); err != nil {
			return err
		}
	}
	return nil
}

// runAutomation executes one rule's actions under a savepoint and records
// the run. An action error rolls back only this rule's work.
func (s *Service) runAutomation(ctx context.Context, tx pgx.Tx, a Automation, taskID uuid.UUID, actor *uuid.UUID, visited map[uuid.UUID]bool, depth int) error {
	inner, err := tx.Begin(ctx) // SAVEPOINT
	if err != nil {
		return err
	}
	chain, detail, actErr := s.applyActions(ctx, inner, a, taskID, actor)
	if actErr != nil {
		_ = inner.Rollback(ctx)
		// Expected misconfigurations (deleted status, archived list, …) are
		// recorded and skipped; infrastructure errors abort the change.
		var httpErr *httperror.Error
		if !errors.As(actErr, &httpErr) {
			return actErr
		}
		return recordRun(ctx, tx, a.ID, taskID, "error", actErr.Error())
	}
	if err := inner.Commit(ctx); err != nil {
		return err
	}
	if err := recordRun(ctx, tx, a.ID, taskID, "ok", detail); err != nil {
		return err
	}
	// Chain: what this rule changed may trigger other rules.
	for _, next := range chain {
		if err := s.fireAutomations(ctx, tx, taskID, actor, next, visited, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// applyActions performs the rule's actions. It returns the follow-up
// triggers to chain and a human-readable summary for the run history.
func (s *Service) applyActions(ctx context.Context, tx pgx.Tx, a Automation, taskID uuid.UUID, actor *uuid.UUID) ([]autoTrigger, string, error) {
	by := map[string]any{"automation": a.Name}
	var chain []autoTrigger
	var done []string

	for _, act := range a.Actions {
		switch act.Type {
		case "set_status":
			var name, category, curName string
			var same bool
			err := tx.QueryRow(ctx, `
				SELECT st.name, st.category, cur.name, t.status_id = st.id
				  FROM tasks t
				  JOIN statuses cur ON cur.id = t.status_id
				  JOIN statuses st ON st.id = $2 AND st.space_id = t.space_id
				 WHERE t.id = $1`, taskID, *act.StatusID).Scan(&name, &category, &curName, &same)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, "", httperror.Validation("status no longer exists in this space", nil)
			}
			if err != nil {
				return nil, "", err
			}
			if same {
				continue
			}
			if _, err := tx.Exec(ctx, `
				UPDATE tasks SET status_id = $2,
				       completed_at = CASE WHEN $3 = 'done' THEN COALESCE(completed_at, now()) ELSE NULL END
				 WHERE id = $1`, taskID, *act.StatusID, category); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, taskID, actor, "status", map[string]any{"from": curName, "to": name, "automation": a.Name}); err != nil {
				return nil, "", err
			}
			followers, err := s.followers(ctx, tx, taskID)
			if err != nil {
				return nil, "", err
			}
			if err := autoNotify(ctx, tx, taskID, actor, "status", fmt.Sprintf("moved to %s by automation %q", name, a.Name), followers); err != nil {
				return nil, "", err
			}
			chain = append(chain, autoTrigger{kind: "status_changed", statusID: *act.StatusID, category: category})
			done = append(done, "status → "+name)

		case "assign":
			tag, err := tx.Exec(ctx, `INSERT INTO task_assignees (task_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, taskID, *act.UserID)
			if err != nil {
				return nil, "", err
			}
			if tag.RowsAffected() == 0 {
				continue // already assigned
			}
			var name string
			if err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, *act.UserID).Scan(&name); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, taskID, actor, "assigned", map[string]any{"user": name, "automation": a.Name}); err != nil {
				return nil, "", err
			}
			if err := autoNotify(ctx, tx, taskID, actor, "assigned",
				fmt.Sprintf("assigned this task to you (automation %q)", a.Name), []uuid.UUID{*act.UserID}); err != nil {
				return nil, "", err
			}
			chain = append(chain, autoTrigger{kind: "assignee_added"})
			done = append(done, "assigned "+name)

		case "set_priority":
			if _, err := tx.Exec(ctx, `UPDATE tasks SET priority = $2 WHERE id = $1 AND priority IS DISTINCT FROM $2`, taskID, act.Priority); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, taskID, actor, "priority", map[string]any{"to": act.Priority, "automation": a.Name}); err != nil {
				return nil, "", err
			}
			done = append(done, "priority → "+act.Priority)

		case "set_due":
			due := time.Now().AddDate(0, 0, *act.Days).Format("2006-01-02")
			if _, err := tx.Exec(ctx, `UPDATE tasks SET due_at = $2::date WHERE id = $1`, taskID, due); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, taskID, actor, "due_at", map[string]any{"to": due, "automation": a.Name}); err != nil {
				return nil, "", err
			}
			done = append(done, "due → "+due)

		case "add_tag":
			tag, err := tx.Exec(ctx, `
				INSERT INTO task_tags (task_id, tag_id)
				SELECT t.id, tg.id FROM tasks t JOIN tags tg ON tg.id = $2 AND tg.space_id = t.space_id
				 WHERE t.id = $1
				ON CONFLICT DO NOTHING`, taskID, *act.TagID)
			if err != nil {
				return nil, "", err
			}
			if tag.RowsAffected() > 0 {
				if err := autoEvent(ctx, tx, taskID, actor, "tags", by); err != nil {
					return nil, "", err
				}
				done = append(done, "tagged")
			}

		case "move_list":
			var fromName, toName string
			var already bool
			err := tx.QueryRow(ctx, `
				SELECT cur.name, dst.name, t.list_id = dst.id
				  FROM tasks t
				  JOIN lists cur ON cur.id = t.list_id
				  JOIN lists dst ON dst.id = $2 AND dst.space_id = t.space_id AND dst.archived_at IS NULL
				 WHERE t.id = $1 AND t.parent_id IS NULL`, taskID, *act.ListID).Scan(&fromName, &toName, &already)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, "", httperror.Validation("target list no longer exists in this space (or the task is a subtask)", nil)
			}
			if err != nil {
				return nil, "", err
			}
			if already {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE tasks SET list_id = $2 WHERE id = $1 OR parent_id = $1`, taskID, *act.ListID); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, taskID, actor, "list", map[string]any{"from": fromName, "to": toName, "automation": a.Name}); err != nil {
				return nil, "", err
			}
			done = append(done, "moved to "+toName)

		case "create_subtask":
			var spaceID uuid.UUID
			var parentID *uuid.UUID
			var listID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT space_id, list_id, parent_id FROM tasks WHERE id = $1`, taskID).Scan(&spaceID, &listID, &parentID); err != nil {
				return nil, "", err
			}
			if parentID != nil {
				continue // subtasks cannot have subtasks
			}
			var number int
			if err := tx.QueryRow(ctx, `UPDATE spaces SET next_number = next_number + 1 WHERE id = $1 RETURNING next_number - 1`, spaceID).Scan(&number); err != nil {
				return nil, "", err
			}
			var subID uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO tasks (space_id, list_id, parent_id, number, title, status_id, position)
				SELECT $1, $2, $3, $4, $5,
				       (SELECT id FROM statuses WHERE space_id = $1 ORDER BY category <> 'not_started', position LIMIT 1),
				       (SELECT COALESCE(max(position), 0) + 1024 FROM tasks WHERE list_id = $2)
				RETURNING id`, spaceID, listID, taskID, number, strings.TrimSpace(act.Title)).Scan(&subID); err != nil {
				return nil, "", err
			}
			if err := autoEvent(ctx, tx, subID, actor, "created", by); err != nil {
				return nil, "", err
			}
			done = append(done, "subtask "+strings.TrimSpace(act.Title))

		case "add_checklist_item":
			if _, err := tx.Exec(ctx, `
				INSERT INTO checklist_items (task_id, body, position)
				SELECT $1, $2, COALESCE(max(position), 0) + 1 FROM checklist_items WHERE task_id = $1`,
				taskID, strings.TrimSpace(act.Body)); err != nil {
				return nil, "", err
			}
			done = append(done, "checklist item")

		case "add_comment":
			if _, err := tx.Exec(ctx, `
				INSERT INTO comments (task_id, author_id, body, via_automation) VALUES ($1, NULL, $2, $3)`,
				taskID, strings.TrimSpace(act.Body), a.Name); err != nil {
				return nil, "", err
			}
			done = append(done, "comment")

		case "notify":
			followers, err := s.followers(ctx, tx, taskID)
			if err != nil {
				return nil, "", err
			}
			if err := autoNotify(ctx, tx, taskID, nil, "automation",
				fmt.Sprintf("%s (automation %q)", strings.TrimSpace(act.Message), a.Name), followers); err != nil {
				return nil, "", err
			}
			done = append(done, "notified followers")
		}
	}
	return chain, strings.Join(done, "; "), nil
}

// ----------------------------------------------------------------------------
// Due-date sweep
// ----------------------------------------------------------------------------

// RunDueSweep fires due_approaching rules for tasks whose due date is within
// reach. Each (rule, task) pair runs at most once ever — the run history is
// the dedup — in its own transaction. Called every minute by the server.
func (s *Service) RunDueSweep(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, t.id
		  FROM automations a
		  JOIN tasks t ON t.space_id = a.space_id AND (a.list_id IS NULL OR t.list_id = a.list_id)
		  JOIN statuses st ON st.id = t.status_id
		 WHERE a.enabled AND a.trigger->>'type' = 'due_approaching'
		   AND t.deleted_at IS NULL AND t.due_at IS NOT NULL AND st.category <> 'done'
		   AND t.due_at <= (CURRENT_DATE + COALESCE((a.trigger->>'days_before')::int, 1))
		   AND NOT EXISTS (SELECT 1 FROM automation_runs r WHERE r.automation_id = a.id AND r.task_id = t.id)
		 LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type pair struct{ auto, task uuid.UUID }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.auto, &p.task); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	ran := 0
	for _, p := range pairs {
		if err := s.sweepOne(ctx, p.auto, p.task); err != nil {
			return ran, err
		}
		ran++
	}
	return ran, nil
}

func (s *Service) sweepOne(ctx context.Context, automationID, taskID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	a, err := scanAutomation(tx.QueryRow(ctx, `SELECT `+automationCols+` FROM automations WHERE id = $1 AND enabled FOR UPDATE`, automationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	facts, ok, err := loadFacts(ctx, tx, taskID)
	if err != nil {
		return err
	}
	if !ok || !facts.matches(a.Conditions) {
		// Record a run anyway so the pair is not retried every minute.
		if err := recordRun(ctx, tx, a.ID, taskID, "ok", "skipped: conditions did not match"); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	visited := map[uuid.UUID]bool{a.ID: true}
	if err := s.runAutomation(ctx, tx, a, taskID, nil, visited, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
