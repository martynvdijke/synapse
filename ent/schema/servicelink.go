package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ServiceLink persists the relationship between a docker-compose service and
// its NPM proxy host and Kuma monitor. npm_instance_id / kuma_instance_id are
// managed at the application level (no ent edges) to match the existing
// pattern in Monitor. Cached details are JSON snapshots refreshed on demand.
type ServiceLink struct {
	ent.Schema
}

func (ServiceLink) Fields() []ent.Field {
	return []ent.Field{
		field.String("service_name").Unique(),
		field.Int("npm_instance_id").Default(0),
		field.String("npm_host_name").Optional().Default(""),
		field.String("npm_details").Optional().Default(""),
		field.Int("kuma_instance_id").Default(0),
		field.Int("kuma_monitor_id").Default(0),
		field.String("kuma_monitor_name").Optional().Default(""),
		field.String("kuma_details").Optional().Default(""),
		field.Time("created_at"),
		field.Time("updated_at").Optional().Nillable(),
	}
}

func (ServiceLink) Edges() []ent.Edge {
	return nil
}

func (ServiceLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("service_name").Unique(),
	}
}
