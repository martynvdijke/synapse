package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// NPMInstance holds the connection details for a single Nginx Proxy Manager
// instance. Multiple instances may be configured; syncs fan out to all
// enabled instances.
type NPMInstance struct {
	ent.Schema
}

func (NPMInstance) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("url"),
		field.String("username"),
		field.String("password").Sensitive(),
		field.Bool("enabled").Default(true),
		field.Time("created_at"),
	}
}

func (NPMInstance) Edges() []ent.Edge {
	return nil
}
