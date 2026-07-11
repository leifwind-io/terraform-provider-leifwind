# Look up by id.
data "leifwind_field" "by_id" {
  project_id = "00000000-0000-0000-0000-000000000000"
  entity_id  = "00000000-0000-0000-0000-000000000001"
  id         = "00000000-0000-0000-0000-000000000002"
}

# Look up by exact name.
data "leifwind_field" "by_name" {
  project_id = "00000000-0000-0000-0000-000000000000"
  entity_id  = "00000000-0000-0000-0000-000000000001"
  name       = "title"
}
