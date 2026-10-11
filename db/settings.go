package db

import "database/sql"

// Settings is a tiny key-value store for global app config the UI toggles at
// runtime (e.g. traffic_capture). Missing keys fall back to caller defaults.

// GetSetting returns the stored value and ok=false when the key is unset.
func (d *DB) GetSetting(key string) (value string, ok bool, err error) {
	err = d.QueryRow(`SELECT value FROM settings WHERE key=$1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSetting upserts a setting value.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(`
INSERT INTO settings(key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, value)
	return err
}

// GetBool returns the boolean setting, or def when unset/unparseable.
func (d *DB) GetBool(key string, def bool) bool {
	v, ok, err := d.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	return v == "true" || v == "1"
}

// SetBool stores a boolean setting as "true"/"false".
func (d *DB) SetBool(key string, val bool) error {
	if val {
		return d.SetSetting(key, "true")
	}
	return d.SetSetting(key, "false")
}

// defaultConversationModeKey is the settings-table key holding the app-wide
// default conversation mode ("security" | "general") used when a new
// conversation's create request omits mode.
const defaultConversationModeKey = "default_conversation_mode"

// GetDefaultConversationMode returns the app-wide default mode for new
// conversations, falling back to "security" when unset/unrecognized.
func (d *DB) GetDefaultConversationMode() string {
	v, ok, err := d.GetSetting(defaultConversationModeKey)
	if err != nil || !ok || v != "general" {
		return "security"
	}
	return "general"
}

// SetDefaultConversationMode stores the app-wide default mode for new
// conversations. mode must be "security" or "general".
func (d *DB) SetDefaultConversationMode(mode string) error {
	if mode != "general" {
		mode = "security"
	}
	return d.SetSetting(defaultConversationModeKey, mode)
}
