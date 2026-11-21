package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// KumaInstance holds the connection details for a single Uptime Kuma
// instance. Multiple instances may be configured; syncs fan out to all
// enabled instances.
type KumaInstance struct {
	ent.Schema
}

func (KumaInstance) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("url"),
		field.String("username"),
		field.String("password").Sensitive(),
		// session_token persists the JWT returned by Kuma's login so
		// later connections can use loginByToken (no rate limit) instead
		// of a fresh password login on every dial. Cleared whenever the
		// credential identity (url/username/password) changes.
		field.String("session_token").Optional().Sensitive().Default(""),
		field.Bool("enabled").Default(true),
		field.Time("created_at"),
	}
}

func (KumaInstance) Edges() []ent.Edge {
	return nil
}
