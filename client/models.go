// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// DataType is the backend field data type discriminator.
type DataType string

const (
	// DataTypeText is the TEXT data type constant.
	DataTypeText DataType = "TEXT"
	// DataTypeInteger is the INTEGER data type constant.
	DataTypeInteger DataType = "INTEGER"
	// DataTypeDecimal is the DECIMAL data type constant.
	DataTypeDecimal DataType = "DECIMAL"
	// DataTypeBoolean is the BOOLEAN data type constant.
	DataTypeBoolean DataType = "BOOLEAN"
	// DataTypeDate is the DATE data type constant.
	DataTypeDate DataType = "DATE"
	// DataTypeTime is the TIME data type constant.
	DataTypeTime DataType = "TIME"
	// DataTypeTimestamp is the TIMESTAMP data type constant.
	DataTypeTimestamp DataType = "TIMESTAMP"
	// DataTypeUUID is the UUID data type constant.
	DataTypeUUID DataType = "UUID"
)

// FieldConfig mirrors the pydantic discriminated union {"data_type": ...}.
type FieldConfig struct {
	DataType DataType `json:"data_type"`
}

// ConnectionType is the backend connection discriminator.
type ConnectionType string

const (
	// ConnectionKey is the KEY connection type constant.
	ConnectionKey ConnectionType = "KEY"
	// ConnectionFragment is the FRAGMENT connection type constant.
	ConnectionFragment ConnectionType = "FRAGMENT"
)

// Connection mirrors {"connection_type":"KEY"} /
// {"connection_type":"FRAGMENT","fragment_name":"x"}.
type Connection struct {
	Type         ConnectionType
	FragmentName string
}

type connectionWire struct {
	ConnectionType ConnectionType `json:"connection_type"`
	FragmentName   *string        `json:"fragment_name,omitempty"`
}

// MarshalJSON marshals Connection to JSON.
func (c Connection) MarshalJSON() ([]byte, error) {
	w := connectionWire{ConnectionType: c.Type}
	if c.Type == ConnectionFragment {
		if c.FragmentName == "" {
			return nil, fmt.Errorf("connection FRAGMENT requires FragmentName")
		}
		w.FragmentName = &c.FragmentName
	}
	return json.Marshal(w)
}

// UnmarshalJSON unmarshals Connection from JSON.
func (c *Connection) UnmarshalJSON(b []byte) error {
	var w connectionWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	c.Type = w.ConnectionType
	c.FragmentName = ""
	if w.FragmentName != nil {
		c.FragmentName = *w.FragmentName
	}
	return nil
}

// MetadataProject mirrors the backend MetadataProject model.
type MetadataProject struct {
	ObjectID  *uuid.UUID
	Name      string
	UniqueKey string // server-computed, read-only
}

type projectWire struct {
	MetadataType string     `json:"metadata_type"`
	ObjectID     *uuid.UUID `json:"object_id,omitempty"`
	Name         string     `json:"name"`
	UniqueKey    string     `json:"unique_key,omitempty"`
}

// MarshalJSON marshals MetadataProject to JSON.
func (p MetadataProject) MarshalJSON() ([]byte, error) {
	return json.Marshal(projectWire{"metadata_project", p.ObjectID, p.Name, p.UniqueKey})
}

// UnmarshalJSON unmarshals MetadataProject from JSON.
func (p *MetadataProject) UnmarshalJSON(b []byte) error {
	var w projectWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*p = MetadataProject{ObjectID: w.ObjectID, Name: w.Name, UniqueKey: w.UniqueKey}
	return nil
}

// MetadataEntity mirrors the backend MetadataEntity model.
type MetadataEntity struct {
	ObjectID  *uuid.UUID
	ProjectID uuid.UUID
	Name      string
	UniqueKey string
}

type entityWire struct {
	MetadataType string     `json:"metadata_type"`
	ObjectID     *uuid.UUID `json:"object_id,omitempty"`
	ProjectID    uuid.UUID  `json:"project_id"`
	Name         string     `json:"name"`
	UniqueKey    string     `json:"unique_key,omitempty"`
}

// MarshalJSON marshals MetadataEntity to JSON.
func (e MetadataEntity) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityWire{"metadata_entity", e.ObjectID, e.ProjectID, e.Name, e.UniqueKey})
}

// UnmarshalJSON unmarshals MetadataEntity from JSON.
func (e *MetadataEntity) UnmarshalJSON(b []byte) error {
	var w entityWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*e = MetadataEntity{ObjectID: w.ObjectID, ProjectID: w.ProjectID, Name: w.Name, UniqueKey: w.UniqueKey}
	return nil
}

// MetadataField mirrors the backend MetadataField model.
type MetadataField struct {
	ObjectID   *uuid.UUID
	ProjectID  uuid.UUID
	EntityID   uuid.UUID
	Name       string
	Config     FieldConfig
	Connection Connection
	UniqueKey  string
}

type fieldWire struct {
	MetadataType string      `json:"metadata_type"`
	ObjectID     *uuid.UUID  `json:"object_id,omitempty"`
	ProjectID    uuid.UUID   `json:"project_id"`
	EntityID     uuid.UUID   `json:"entity_id"`
	Name         string      `json:"name"`
	Config       FieldConfig `json:"config"`
	Connection   Connection  `json:"connection_type"`
	UniqueKey    string      `json:"unique_key,omitempty"`
}

// MarshalJSON marshals MetadataField to JSON.
func (f MetadataField) MarshalJSON() ([]byte, error) {
	return json.Marshal(fieldWire{"metadata_field", f.ObjectID, f.ProjectID, f.EntityID, f.Name, f.Config, f.Connection, f.UniqueKey})
}

// UnmarshalJSON unmarshals MetadataField from JSON.
func (f *MetadataField) UnmarshalJSON(b []byte) error {
	var w fieldWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*f = MetadataField{ObjectID: w.ObjectID, ProjectID: w.ProjectID, EntityID: w.EntityID,
		Name: w.Name, Config: w.Config, Connection: w.Connection, UniqueKey: w.UniqueKey}
	return nil
}

func (p MetadataProject) hasObjectID() bool { return p.ObjectID != nil }
func (e MetadataEntity) hasObjectID() bool  { return e.ObjectID != nil }
func (f MetadataField) hasObjectID() bool   { return f.ObjectID != nil }
