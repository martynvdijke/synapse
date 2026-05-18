package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Settings struct {
	ent.Schema
}

func (Settings) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique(),
		field.String("value"),
	}
}

func (Settings) Edges() []ent.Edge {
	return nil
}

func (Settings) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("key").Unique(),
	}
}
