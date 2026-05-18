package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type SyncRun struct {
	ent.Schema
}

func (SyncRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("source").Default("docker"),
		field.String("status").Default("pending"),
		field.Time("started_at"),
		field.Time("finished_at").Optional().Nillable(),
		field.Int("total_services").Default(0),
		field.Int("added").Default(0),
		field.Int("skipped").Default(0),
		field.Int("failed").Default(0),
		field.String("error_message").Optional().Default(""),
	}
}

func (SyncRun) Edges() []ent.Edge {
	return nil
}
