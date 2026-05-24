package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// TempAccess holds temporary IP access rules managed by Synapse.
type TempAccess struct {
	ent.Schema
}

func (TempAccess) Fields() []ent.Field {
	return []ent.Field{
		field.String("ip"),
		field.String("reason").Default(""),
		field.Time("expires_at"),
		field.Time("created_at"),
		field.String("status").Default("active"),
	}
}

func (TempAccess) Edges() []ent.Edge {
	return nil
}
