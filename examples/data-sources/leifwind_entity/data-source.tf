# Look up by id.
data "leifwind_entity" "by_id" {
  project_id = "00000000-0000-0000-0000-000000000000"
  id         = "00000000-0000-0000-0000-000000000001"
}

# Look up by exact name.
data "leifwind_entity" "by_name" {
  project_id = "00000000-0000-0000-0000-000000000000"
  name       = "book"
}
