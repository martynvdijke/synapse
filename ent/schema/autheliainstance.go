package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AutheliaInstance holds the connection details for a single Authelia
// instance. Multiple instances may be configured; syncs can fan out to all
// enabled instances, or target a specific instance by ID.
type AutheliaInstance struct {
	ent.Schema
}

func (AutheliaInstance) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("config_path"),
		field.String("db_path").Optional(),
		field.String("default_policy").Default("one_factor"),
		field.String("overrides").Optional(),
		field.Bool("auto_sync").Default(false),
		field.String("npm_instance_ids").Optional(), // JSON array of ints, empty = all NPMs
		field.Bool("enabled").Default(true),
		field.Time("created_at"),
	}
}

func (AutheliaInstance) Edges() []ent.Edge {
	return nil
}
