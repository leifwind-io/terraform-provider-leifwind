// SPDX-License-Identifier: MPL-2.0

package client

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// golden strings mirror the backend's pydantic serialization exactly
const goldenProject = `{"metadata_type":"metadata_project","object_id":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f","name":"proj_a","unique_key":"proj_a"}`

const goldenFieldFragment = `{"metadata_type":"metadata_field","object_id":null,` +
	`"project_id":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f",` +
	`"entity_id":"7e57d004-2b97-44e7-8f00-63d2c6b0a50e","name":"body",` +
	`"config":{"data_type":"TEXT"},` +
	`"connection_type":{"connection_type":"FRAGMENT","fragment_name":"content"},` +
	`"unique_key":"a2ff0efa-64ac-4499-b2a4-99b598ee1c9f:7e57d004-2b97-44e7-8f00-63d2c6b0a50e:body"}`

func TestProjectUnmarshalGolden(t *testing.T) {
	var p MetadataProject
	if err := json.Unmarshal([]byte(goldenProject), &p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "proj_a" || p.UniqueKey != "proj_a" || p.ObjectID == nil {
		t.Fatalf("bad decode: %+v", p)
	}
}

func TestProjectMarshalEmitsTypeAndOmitsNilID(t *testing.T) {
	b, err := json.Marshal(MetadataProject{Name: "proj_a"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["metadata_type"] != "metadata_project" {
		t.Fatalf("metadata_type missing: %s", b)
	}
	if _, present := m["object_id"]; present {
		t.Fatalf("nil object_id must be omitted on input: %s", b)
	}
}

func TestFieldFragmentRoundTrip(t *testing.T) {
	var f MetadataField
	if err := json.Unmarshal([]byte(goldenFieldFragment), &f); err != nil {
		t.Fatal(err)
	}
	if f.Connection.Type != ConnectionFragment || f.Connection.FragmentName != "content" {
		t.Fatalf("bad connection: %+v", f.Connection)
	}
	if f.Config.DataType != DataTypeText {
		t.Fatalf("bad config: %+v", f.Config)
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	conn := m["connection_type"].(map[string]any)
	if conn["fragment_name"] != "content" {
		t.Fatalf("fragment_name lost: %s", b)
	}
}

func TestConnectionKeyOmitsFragmentName(t *testing.T) {
	b, err := json.Marshal(Connection{Type: ConnectionKey})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"connection_type":"KEY"}` {
		t.Fatalf("got %s", b)
	}
}

func TestEntityRoundTrip(t *testing.T) {
	pid := uuid.New()
	e := MetadataEntity{ProjectID: pid, Name: "book"}
	b, _ := json.Marshal(e)
	var back MetadataEntity
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ProjectID != pid || back.Name != "book" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}
