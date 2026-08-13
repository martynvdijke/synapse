package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DockerEvent is a single event observed from the Docker Engine events
// stream (container lifecycle, image events, health status). image_old and
// image_new are populated for synthesized "image updated" events.
type DockerEvent struct {
	ent.Schema
}

func (DockerEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("event_type").Default(""),
		field.String("action").Default(""),
		field.String("actor_id").Optional().Default(""),
		field.String("actor_name").Optional().Default(""),
		field.String("image").Optional().Default(""),
		field.String("status").Optional().Default(""),
		field.String("image_old").Optional().Default(""),
		field.String("image_new").Optional().Default(""),
		field.String("payload").Optional().Default(""),
		field.Time("created_at"),
	}
}

func (DockerEvent) Edges() []ent.Edge {
	return nil
}

func (DockerEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
	}
}
