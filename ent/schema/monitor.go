package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Monitor struct {
	ent.Schema
}

func (Monitor) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("service_name"),
		field.String("monitor_type"),
		field.String("url").Optional().Default(""),
		field.String("docker_container").Optional().Default(""),
		field.Int("kuma_id").Default(0),
		// kuma_instance_id references the KumaInstance this monitor belongs
		// to. Managed at the application level (no ent edge) to keep the
		// generated code simple. The same service can exist in multiple
		// instances, each with its own kuma_id within that instance.
		field.Int("kuma_instance_id").Default(0),
		field.Time("created_at"),
	}
}

func (Monitor) Edges() []ent.Edge {
	return nil
}

func (Monitor) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name", "monitor_type", "kuma_instance_id").Unique(),
	}
}
