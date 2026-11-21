package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type SyncRun struct {
	ent.Schema
}

// SyncInstanceError is one enabled integration instance that could not be
// read or written during a sync/reconcile run. It is persisted as JSON on the
// run so a partial failure can never be reported as a clean success.
type SyncInstanceError struct {
	InstanceID int    `json:"instance_id"`
	Name       string `json:"name"`
	Error      string `json:"error"`
}

func (SyncRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("source").Default("docker"),
		field.String("status").Default("pending"),
		field.Time("started_at"),
		field.Time("finished_at").Optional().Nillable(),
		field.Int("total_services").Default(0),
		field.Int("added").Default(0),
		field.Int("updated").Default(0),
		field.Int("skipped").Default(0),
		field.Int("failed").Default(0),
		field.String("error_message").Optional().Default(""),
		field.JSON("instance_errors", []SyncInstanceError{}).Optional(),
		field.Bool("dry_run").Optional().Default(false),
	}
}

func (SyncRun) Edges() []ent.Edge {
	return nil
}
