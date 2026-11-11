package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
)

// FieldTypes are the custom field types.
var FieldTypes = []string{"text", "number", "date", "select", "checkbox", "url"}

// FieldOption is one choice of a select field.
type FieldOption struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
}

// Field is a custom field of a space.
type Field struct {
	ID       uuid.UUID     `json:"id"`
	SpaceID  uuid.UUID     `json:"space_id"`
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	Options  []FieldOption `json:"options"`
	Position int           `json:"position"`
}

// FieldInput creates or updates a field. Type cannot change after
// creation (existing values would no longer fit).
type FieldInput struct {
	Name     *string        `json:"name"`
	Type     string         `json:"type"`
	Options  *[]FieldOption `json:"options"`
	Position *int           `json:"position"`
}

const (
	maxFieldsPerSpace = 50
	maxFieldOptions   = 100
	maxFieldText      = 5000
)

func scanField(row pgx.Row) (Field, error) {
	var f Field
	var opts []byte
	if err := row.Scan(&f.ID, &f.SpaceID, &f.Name, &f.Type, &opts, &f.Position); err != nil {
		return Field{}, err
	}
	f.Options = []FieldOption{}
	_ = json.Unmarshal(opts, &f.Options)
	return f, nil
}

const fieldCols = `id, space_id, name, type, options, position`

// SpaceFields lists a space's custom fields in order.
func (s *Service) SpaceFields(ctx context.Context, spaceID uuid.UUID) ([]Field, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+fieldCols+` FROM custom_fields WHERE space_id = $1 ORDER BY position, created_at`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Field{}
	for rows.Next() {
		f, err := scanField(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetField loads one field or a typed 404.
func (s *Service) GetField(ctx context.Context, id uuid.UUID) (Field, error) {
	f, err := scanField(s.pool.QueryRow(ctx, `SELECT `+fieldCols+` FROM custom_fields WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Field{}, httperror.NotFound("field")
	}
	return f, err
}

func cleanOptions(in []FieldOption) ([]FieldOption, map[string]string) {
	issues := map[string]string{}
	if len(in) > maxFieldOptions {
		issues["options"] = "at most 100 options"
	}
	out := make([]FieldOption, 0, len(in))
	seen := map[string]bool{}
	for _, o := range in {
		o.Name = strings.TrimSpace(o.Name)
		if o.Name == "" {
			issues["options"] = "every option needs a name"
			continue
		}
		if seen[strings.ToLower(o.Name)] {
			issues["options"] = "option names must be unique"
			continue
		}
		seen[strings.ToLower(o.Name)] = true
		if o.ID == uuid.Nil {
			o.ID = uuid.New()
		}
		if o.Color == "" {
			o.Color = "#9ca3af"
		}
		if !colorPattern.MatchString(o.Color) {
			issues["options"] = "colours must look like #1a2b3c"
		}
		out = append(out, o)
	}
	return out, issues
}

// CreateField adds a field at the end of the space's fields.
func (s *Service) CreateField(ctx context.Context, spaceID uuid.UUID, in FieldInput) (Field, error) {
	issues := map[string]string{}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" || len(name) > 100 {
		issues["name"] = "1 to 100 characters"
	}
	known := false
	for _, t := range FieldTypes {
		known = known || t == in.Type
	}
	if !known {
		issues["type"] = "must be " + strings.Join(FieldTypes, ", ")
	}
	opts := []FieldOption{}
	if in.Options != nil {
		var oi map[string]string
		opts, oi = cleanOptions(*in.Options)
		for k, v := range oi {
			issues[k] = v
		}
	}
	if in.Type == "select" && len(opts) == 0 {
		issues["options"] = "a select field needs at least one option"
	}
	if in.Type != "select" && len(opts) > 0 {
		issues["options"] = "only select fields have options"
	}
	if len(issues) > 0 {
		return Field{}, httperror.Validation("invalid field", issues)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM custom_fields WHERE space_id = $1`, spaceID).Scan(&count); err != nil {
		return Field{}, err
	}
	if count >= maxFieldsPerSpace {
		return Field{}, httperror.Validation("a space can have at most 50 fields", nil)
	}
	raw, _ := json.Marshal(opts)
	f, err := scanField(s.pool.QueryRow(ctx, `
		INSERT INTO custom_fields (space_id, name, type, options, position)
		VALUES ($1, $2, $3, $4, (SELECT COALESCE(max(position), 0) + 1 FROM custom_fields WHERE space_id = $1))
		RETURNING `+fieldCols, spaceID, name, in.Type, raw))
	if isUniqueViolation(err) {
		return Field{}, httperror.Conflict("this space already has a field with that name")
	}
	return f, err
}

// UpdateField renames, reorders or changes the options of a field.
// Removing a select option clears it from every task that had it.
func (s *Service) UpdateField(ctx context.Context, id uuid.UUID, in FieldInput) (Field, error) {
	cur, err := s.GetField(ctx, id)
	if err != nil {
		return Field{}, err
	}
	if in.Type != "" && in.Type != cur.Type {
		return Field{}, httperror.Validation("a field's type cannot change; create a new field", nil)
	}
	issues := map[string]string{}
	name := cur.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" || len(name) > 100 {
			issues["name"] = "1 to 100 characters"
		}
	}
	opts := cur.Options
	if in.Options != nil {
		if cur.Type != "select" {
			issues["options"] = "only select fields have options"
		} else {
			var oi map[string]string
			opts, oi = cleanOptions(*in.Options)
			for k, v := range oi {
				issues[k] = v
			}
			if len(opts) == 0 {
				issues["options"] = "a select field needs at least one option"
			}
		}
	}
	if len(issues) > 0 {
		return Field{}, httperror.Validation("invalid field", issues)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Field{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	raw, _ := json.Marshal(opts)
	f, err := scanField(tx.QueryRow(ctx, `
		UPDATE custom_fields SET name = $2, options = $3, position = COALESCE($4, position)
		 WHERE id = $1 RETURNING `+fieldCols, id, name, raw, in.Position))
	if isUniqueViolation(err) {
		return Field{}, httperror.Conflict("this space already has a field with that name")
	}
	if err != nil {
		return Field{}, err
	}
	if cur.Type == "select" {
		keep := make([]string, 0, len(opts))
		for _, o := range opts {
			keep = append(keep, o.ID.String())
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_field_values WHERE field_id = $1 AND NOT (value #>> '{}' = ANY($2))`, id, keep); err != nil {
			return Field{}, err
		}
	}
	return f, tx.Commit(ctx)
}

// DeleteField removes a field and every value in it, with an audit row.
func (s *Service) DeleteField(ctx context.Context, actor, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var name, typ string
	var spaceID uuid.UUID
	var values int
	err = tx.QueryRow(ctx, `
		SELECT name, type, space_id, (SELECT count(*) FROM task_field_values WHERE field_id = f.id)
		  FROM custom_fields f WHERE id = $1`, id).Scan(&name, &typ, &spaceID, &values)
	if errors.Is(err, pgx.ErrNoRows) {
		return httperror.NotFound("field")
	}
	if err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, &actor, "field", id, "deleted", map[string]any{
		"name": name, "type": typ, "space_id": spaceID, "values": values,
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM custom_fields WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// normalizeFieldValue checks a value against its field and returns it in
// stored form. A JSON null means "clear".
func normalizeFieldValue(f Field, raw json.RawMessage) (json.RawMessage, error) {
	bad := func(msg string) error {
		return httperror.Validation("invalid field value", map[string]string{"fields." + f.ID.String(): msg})
	}
	switch f.Type {
	case "text", "url", "date", "select":
		var v string
		if json.Unmarshal(raw, &v) != nil {
			return nil, bad(f.Name + " must be a string")
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		switch f.Type {
		case "text":
			if len(v) > maxFieldText {
				return nil, bad(f.Name + " is too long")
			}
		case "url":
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(v) > 2000 {
				return nil, bad(f.Name + " must be an http(s) address")
			}
		case "date":
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return nil, bad(f.Name + " must be YYYY-MM-DD")
			}
		case "select":
			ok := false
			for _, o := range f.Options {
				ok = ok || o.ID.String() == v
			}
			if !ok {
				return nil, bad(f.Name + " must be one of its options")
			}
		}
		out, _ := json.Marshal(v)
		return out, nil
	case "number":
		var v float64
		if json.Unmarshal(raw, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, bad(f.Name + " must be a number")
		}
		out, _ := json.Marshal(v)
		return out, nil
	case "checkbox":
		var v bool
		if json.Unmarshal(raw, &v) != nil {
			return nil, bad(f.Name + " must be true or false")
		}
		out, _ := json.Marshal(v)
		return out, nil
	}
	return nil, bad("unknown field type")
}

// fieldDisplay renders a stored value for the activity log.
func fieldDisplay(f Field, raw []byte) string {
	if raw == nil {
		return ""
	}
	if f.Type == "select" {
		var id string
		_ = json.Unmarshal(raw, &id)
		for _, o := range f.Options {
			if o.ID.String() == id {
				return o.Name
			}
		}
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return "no"
	}
	return strings.TrimSpace(string(raw))
}

// setFieldValues applies a fields patch inside UpdateTask's transaction.
func (s *Service) setFieldValues(ctx context.Context, tx pgx.Tx, actor, taskID, spaceID uuid.UUID, patch map[uuid.UUID]json.RawMessage) error {
	if len(patch) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT `+fieldCols+` FROM custom_fields WHERE space_id = $1`, spaceID)
	if err != nil {
		return err
	}
	fields := map[uuid.UUID]Field{}
	for rows.Next() {
		f, err := scanField(rows)
		if err != nil {
			rows.Close()
			return err
		}
		fields[f.ID] = f
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, raw := range patch {
		f, ok := fields[id]
		if !ok {
			return httperror.Validation("unknown field", map[string]string{"fields." + id.String(): "not a field of this space"})
		}
		var value json.RawMessage
		if len(raw) > 0 && string(raw) != "null" {
			if value, err = normalizeFieldValue(f, raw); err != nil {
				return err
			}
		}
		var before []byte
		err := tx.QueryRow(ctx, `SELECT value FROM task_field_values WHERE task_id = $1 AND field_id = $2`, taskID, id).Scan(&before)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if value == nil {
			if before == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM task_field_values WHERE task_id = $1 AND field_id = $2`, taskID, id); err != nil {
				return err
			}
		} else {
			if before != nil && fieldDisplay(f, before) == fieldDisplay(f, value) {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO task_field_values (task_id, field_id, value) VALUES ($1, $2, $3)
				ON CONFLICT (task_id, field_id) DO UPDATE SET value = EXCLUDED.value`, taskID, id, []byte(value)); err != nil {
				return err
			}
		}
		if err := addEvent(ctx, tx, taskID, actor, "field", map[string]any{
			"field": f.Name, "from": fieldDisplay(f, before), "to": fieldDisplay(f, value),
		}); err != nil {
			return err
		}
	}
	return nil
}
