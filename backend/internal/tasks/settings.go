package tasks

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/AGlElectronics/tasks/backend/internal/httperror"
	"github.com/AGlElectronics/tasks/backend/internal/mail"
)

// Module settings: retention and reminders. Each value comes from, weakest
// first: the built-in default, Tasks' own setting (module_settings, edited
// on Settings), and Den's module settings when the module is paired and
// Den sends the key (client.ModuleSettings). Den's value wins and cannot
// be changed in Tasks. The contract name is under tasks.* (tasks.trash_days);
// the name Tasks stores (retention.trash_days) is still accepted.
//
// The retention defaults are placeholders pending legal review; set them
// to what your organisation's data-protection rules say.

// SettingDef describes one setting.
type SettingDef struct {
	Key         string `json:"key"` // section.field, e.g. retention.trash_days
	Default     int    `json:"default"`
	Min         int    `json:"min"`
	Max         int    `json:"max"`
	Description string `json:"description"`
}

// SettingDefs is every module setting. Retention values are days; 0 keeps
// the data forever.
var SettingDefs = []SettingDef{
	{"retention.trash_days", 30, 0, 3650, "Days a deleted task stays in the trash before it is purged for good. 0 = never purge."},
	{"retention.notification_days", 90, 0, 3650, "Days a read Inbox notification is kept. 0 = keep forever."},
	{"retention.audit_days", 730, 0, 3650, "Days an audit log entry is kept. 0 = keep forever."},
	{"retention.automation_run_days", 90, 0, 3650, "Days an automation run stays in its history. 0 = keep forever."},
	{"reminders.due_soon_days", 1, 0, 30, "Assignees get a due-soon reminder this many days before the due date (and on the day). 0 = no reminders."},
}

func settingDef(key string) (SettingDef, bool) {
	for _, d := range SettingDefs {
		if d.Key == key {
			return d, true
		}
	}
	return SettingDef{}, false
}

// SettingsView is GET /api/v1/settings.
type SettingsView struct {
	// Settings is the effective value of every setting, nested by section.
	Settings map[string]map[string]int `json:"settings"`
	// Sources says where each value comes from: default, tasks or den.
	Sources map[string]string `json:"sources"`
	Defs    []SettingDef      `json:"definitions"`
	Email   EmailStatus       `json:"email"`
}

// EmailStatus says whether and through what email goes out. It never
// includes the password.
type EmailStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"` // den, tasks or none
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	From       string `json:"from,omitempty"`
	Security   string `json:"security,omitempty"`
}

// denSettingKey is the contract name of a Tasks setting: under the module
// slug (tasks.trash_days). denValues also accepts the older name (the key
// Tasks stores itself, such as retention.trash_days) so a bundle written
// before the names settled still applies.
func denSettingKey(key string) string {
	switch key {
	case "retention.trash_days":
		return "tasks.trash_days"
	case "retention.notification_days":
		return "tasks.notification_days"
	case "retention.audit_days":
		return "tasks.audit_days"
	case "retention.automation_run_days":
		return "tasks.automation_run_days"
	case "reminders.due_soon_days":
		return "tasks.due_soon_days"
	default:
		return "tasks." + key
	}
}

// denValues reads Den's module settings from the paired denlink client
// (the bundle it already stored). values is keyed by Tasks' own setting
// names. smtp is set only when Den sets smtp.host and a sender. passwordSet
// means the password is not in the bundle: the caller fetches it when mail
// is actually sent. All three are empty when the module is standalone.
func (s *Service) denValues() (values map[string]int, smtp *mail.Config, passwordSet bool) {
	if s == nil || s.den == nil {
		return nil, nil, false
	}
	ms := s.den.ModuleSettings()
	if ms == nil {
		return nil, nil, false
	}
	values = map[string]int{}
	for _, d := range SettingDefs {
		n, ok := ms.Days(denSettingKey(d.Key))
		if !ok {
			n, ok = ms.Days(d.Key)
		}
		if !ok || n < d.Min || n > d.Max {
			continue
		}
		values[d.Key] = n
	}
	sm, ok := ms.SMTP()
	if !ok {
		return values, nil, false
	}
	cfg := (mail.Config{
		Host: sm.Host, Port: sm.Port, Username: sm.Username,
		From: sm.From, FromName: sm.FromName, Security: sm.Security,
	}).Normalized()
	if !cfg.Enabled() {
		return values, nil, false
	}
	return values, &cfg, sm.PasswordSet
}

// effectiveSettings resolves every setting and where it came from.
func (s *Service) effectiveSettings(ctx context.Context) (map[string]int, map[string]string, error) {
	values, sources := map[string]int{}, map[string]string{}
	for _, d := range SettingDefs {
		values[d.Key], sources[d.Key] = d.Default, "default"
	}
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM module_settings`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var v int
		if _, ok := settingDef(key); ok && json.Unmarshal(raw, &v) == nil {
			values[key], sources[key] = v, "tasks"
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	den, _, _ := s.denValues()
	for key, v := range den {
		values[key], sources[key] = v, "den"
	}
	return values, sources, nil
}

// Setting returns one effective setting value.
func (s *Service) Setting(ctx context.Context, key string) (int, error) {
	values, _, err := s.effectiveSettings(ctx)
	if err != nil {
		return 0, err
	}
	return values[key], nil
}

// SetEnvSMTP sets Tasks' own SMTP settings (SMTP_* environment), used when
// Den does not send any.
func (s *Service) SetEnvSMTP(c mail.Config) { s.envSMTP = c.Normalized() }

// MailConfig is the SMTP server email goes through: Den's when it sends
// one, else Tasks' own. ok is false when neither is configured. When Den
// holds the password (smtp.password_set) it is fetched now and kept only
// in the returned config.
func (s *Service) MailConfig(ctx context.Context) (cfg mail.Config, source string, ok bool, err error) {
	return s.mailConfig(ctx, true)
}

func (s *Service) mailConfig(ctx context.Context, withPassword bool) (cfg mail.Config, source string, ok bool, err error) {
	_, den, passwordSet := s.denValues()
	if den != nil {
		cfg = *den
		if passwordSet && withPassword {
			pw, err := s.den.SMTPPassword(ctx)
			if err != nil {
				return mail.Config{}, "", false, err
			}
			cfg.Password = pw
		}
		return cfg, "den", true, nil
	}
	if s.envSMTP.Enabled() {
		return s.envSMTP, "tasks", true, nil
	}
	return mail.Config{}, "none", false, nil
}

// Settings returns the settings page view.
func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	values, sources, err := s.effectiveSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	v := SettingsView{Settings: map[string]map[string]int{}, Sources: sources, Defs: SettingDefs}
	for key, val := range values {
		section, field, _ := strings.Cut(key, ".")
		if v.Settings[section] == nil {
			v.Settings[section] = map[string]int{}
		}
		v.Settings[section][field] = val
	}
	cfg, source, ok, err := s.mailConfig(ctx, false)
	if err != nil {
		return SettingsView{}, err
	}
	v.Email = EmailStatus{Configured: ok, Source: source}
	if ok {
		v.Email.Host, v.Email.Port, v.Email.From, v.Email.Security = cfg.Host, cfg.Port, cfg.From, cfg.Security
	}
	return v, nil
}

// SettingsPatch is PUT /api/v1/settings: {"retention": {"trash_days": 14}}.
// null resets a setting to its default.
type SettingsPatch map[string]map[string]*int

// UpdateSettings stores Tasks' own values. Settings Den manages are
// refused: change them in Den.
func (s *Service) UpdateSettings(ctx context.Context, actor uuid.UUID, p SettingsPatch) (SettingsView, error) {
	_, sources, err := s.effectiveSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	issues := map[string]string{}
	changes := map[string]*int{}
	for section, fields := range p {
		for field, v := range fields {
			key := section + "." + field
			d, ok := settingDef(key)
			switch {
			case !ok:
				issues[key] = "unknown setting"
			case sources[key] == "den":
				issues[key] = "managed by Den; change it in Den's module settings"
			case v != nil && (*v < d.Min || *v > d.Max):
				issues[key] = "out of range"
			default:
				changes[key] = v
			}
		}
	}
	if len(issues) > 0 {
		return SettingsView{}, httperror.Validation("invalid settings", issues)
	}
	if len(changes) == 0 {
		return s.Settings(ctx)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	audit := map[string]any{}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := changes[key]
		if v == nil {
			if _, err := tx.Exec(ctx, `DELETE FROM module_settings WHERE key = $1`, key); err != nil {
				return SettingsView{}, err
			}
			audit[key] = nil
			continue
		}
		raw, _ := json.Marshal(*v)
		if _, err := tx.Exec(ctx, `
			INSERT INTO module_settings (key, value, updated_by, updated_at) VALUES ($1, $2, $3, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			key, raw, actor); err != nil {
			return SettingsView{}, err
		}
		audit[key] = *v
	}
	if err := writeAudit(ctx, tx, &actor, "settings", uuid.Nil, "updated", audit); err != nil {
		return SettingsView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SettingsView{}, err
	}
	return s.Settings(ctx)
}
