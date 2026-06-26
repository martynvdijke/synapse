package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AutheliaAlert holds alerts about NPM CNAMEs missing from Authelia config.
type AutheliaAlert struct {
	ent.Schema
}

func (AutheliaAlert) Fields() []ent.Field {
	return []ent.Field{
		field.String("cname"),
		field.String("message"),
		field.String("severity").Default("warning"),
		field.String("status").Default("open"),
		field.Int("authelia_instance_id").Default(0),
		field.Time("created_at"),
	}
}

func (AutheliaAlert) Edges() []ent.Edge {
	return nil
}
